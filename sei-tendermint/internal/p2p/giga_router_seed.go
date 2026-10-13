package p2p

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	atypes "github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/data"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/epoch"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/p2p/giga"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/p2p/rpc"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
)

// StateSyncRestoredMarker is the file under the persistent state directory that a
// state sync restore writes last. Its presence allows one BlockStore seed.
const StateSyncRestoredMarker = "state-sync-restored"

const seedPeerTimeout = time.Minute

var errSeedQCReceived = errors.New("state sync seed QC received")

// BuildFullnodeDataState is BuildDataState for a fullnode. When a state sync
// restore left StateSyncRestoredMarker and the BlockStore is empty, it first
// writes the verified CommitQC that covers the app height into the BlockStore.
func BuildFullnodeDataState(ctx context.Context, cfg *GigaRouterCommonConfig, key NodeSecretKey, blockStore atypes.BlockStore) (*data.State, error) {
	registry, err := buildRegistry(cfg)
	if err != nil {
		return nil, err
	}
	if err := seedFromStateSync(ctx, cfg, key, registry, blockStore); err != nil {
		return nil, err
	}
	return newDataState(registry, blockStore)
}

// seedFromStateSync applies the StateSyncRestoredMarker rules: no marker does
// nothing; a marker with a non-empty BlockStore is removed; a marker with an
// empty BlockStore seeds the store and is removed only after the seed is durable.
func seedFromStateSync(ctx context.Context, cfg *GigaRouterCommonConfig, key NodeSecretKey, registry *epoch.Registry, blockStore atypes.BlockStore) error {
	marker := filepath.Join(cfg.PersistentStateDir, StateSyncRestoredMarker)
	if _, err := os.Stat(marker); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("stat %s: %w", marker, err)
	}
	if blockStore.Status().IsPresent() {
		logger.Info("state sync marker found but BlockStore is not empty; removing the marker", "marker", marker)
		return os.Remove(marker)
	}
	height := cfg.App.LastBlockHeight()
	if height < cfg.GenDoc.InitialHeight {
		return fmt.Errorf("state sync marker %s is present but app height %d is below initial height %d", marker, height, cfg.GenDoc.InitialHeight)
	}
	h := atypes.GlobalBlockNumber(height) // nolint:gosec // checked against InitialHeight >= 1.
	qc, err := fetchCoveringQC(ctx, cfg, key, registry, h)
	if err != nil {
		return fmt.Errorf("seed BlockStore for state sync height %d: %w", h, err)
	}
	if err := blockStore.WriteQC(qc); err != nil {
		return fmt.Errorf("seed BlockStore: WriteQC: %w", err)
	}
	if err := blockStore.Flush(); err != nil {
		return fmt.Errorf("seed BlockStore: Flush: %w", err)
	}
	if err := os.Remove(marker); err != nil {
		return fmt.Errorf("remove %s: %w", marker, err)
	}
	gr := qc.QC().GlobalRange()
	logger.Warn("BlockStore seeded from state sync; blocks below the seed are not available on this node",
		"height", h, "first", gr.First, "next", gr.Next, "epoch", qc.QC().Proposal().EpochIndex())
	return nil
}

// fetchCoveringQC asks each configured validator in turn for the CommitQC that
// covers h, and returns the first one that covers h and verifies against registry.
func fetchCoveringQC(ctx context.Context, cfg *GigaRouterCommonConfig, key NodeSecretKey, registry *epoch.Registry, h atypes.GlobalBlockNumber) (*atypes.FullCommitQC, error) {
	r := &gigaRouterCommon{
		cfg:     cfg,
		key:     key,
		poolOut: giga.NewPool[atypes.PublicKey, rpc.Client[giga.API]](),
	}
	var errs []error
	for validator, addr := range cfg.ValidatorAddrs {
		var qc *atypes.FullCommitQC
		err := utils.WithTimeout(ctx, seedPeerTimeout, func(ctx context.Context) error {
			return r.dialAndRunConn(ctx, validator, addr.Key, addr.HostPort, func(ctx context.Context, client rpc.Client[giga.API]) error {
				got, err := requestQC(ctx, client, h)
				if err != nil {
					return err
				}
				qc = got
				// The connection runs until its scope is cancelled, and an error is what cancels it.
				return errSeedQCReceived
			})
		})
		if errors.Is(err, errSeedQCReceived) {
			err = verifyCoveringQC(qc, registry, h)
		}
		if err == nil {
			return qc, nil
		}
		logger.Info("state sync seed: validator did not give a usable QC", "validator", validator, "addr", addr.HostPort, "err", err)
		errs = append(errs, fmt.Errorf("validator %v: %w", validator, err))
		if ctx.Err() != nil {
			break
		}
	}
	return nil, fmt.Errorf("no validator gave a QC covering %d: %w", h, errors.Join(errs...))
}

func requestQC(ctx context.Context, client rpc.Client[giga.API], h atypes.GlobalBlockNumber) (*atypes.FullCommitQC, error) {
	stream, err := giga.StreamFullCommitQCs.Call(ctx, client)
	if err != nil {
		return nil, fmt.Errorf("StreamFullCommitQCs.Call(): %w", err)
	}
	defer stream.Close()
	if err := stream.Send(ctx, giga.StreamFullCommitQCsReqConv.Encode(&giga.StreamFullCommitQCsReq{NextBlock: h})); err != nil {
		return nil, fmt.Errorf("stream.Send(): %w", err)
	}
	raw, err := stream.Recv(ctx)
	if err != nil {
		return nil, fmt.Errorf("stream.Recv(): %w", err)
	}
	qc, err := atypes.FullCommitQCConv.Decode(raw)
	if err != nil {
		return nil, fmt.Errorf("decode FullCommitQC: %w", err)
	}
	return qc, nil
}

func verifyCoveringQC(qc *atypes.FullCommitQC, registry *epoch.Registry, h atypes.GlobalBlockNumber) error {
	gr := qc.QC().GlobalRange()
	if h < gr.First || h >= gr.Next {
		return fmt.Errorf("QC covers [%d,%d), not %d; the peer may have pruned %d", gr.First, gr.Next, h, h)
	}
	ep, err := registry.EpochByIndex(qc.QC().Proposal().EpochIndex())
	if err != nil {
		return fmt.Errorf("registry.EpochByIndex(%d): %w", qc.QC().Proposal().EpochIndex(), err)
	}
	if err := qc.Verify(ep); err != nil {
		return fmt.Errorf("qc.Verify(): %w", err)
	}
	return nil
}
