package p2p

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/sei-protocol/sei-chain/giga/apphash/builder"
	ablockstore "github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/blockstore"
	atypes "github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/epoch"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
)

// StateSyncStart is the giga state sync state of a home at node start.
type StateSyncStart struct {
	// Enabled is the [giga.state_sync] enabled switch.
	Enabled bool
	// Restored is the road that the restore marker of the home names, or None when the home
	// holds no marker.
	Restored utils.Option[atypes.RoadIndex]
}

// seedBlockStore writes road block H, AppProposal(H) and AppQC(H) from the state sync proof file
// into blockStore when start names a restore of road H, and then deletes the state sync proof
// file and the restore marker. Without a restore it writes nothing.
func seedBlockStore(
	ctx context.Context,
	cfg *GigaRouterCommonConfig,
	registry *epoch.Registry,
	blockStore *ablockstore.Store,
	start StateSyncStart,
) error {
	road, ok := start.Restored.Get()
	if !ok {
		if start.Enabled {
			warnUnseededStart(cfg, blockStore)
		}
		return nil
	}
	if err := writeSeedRecords(ctx, cfg, registry, blockStore, road); err != nil {
		return fmt.Errorf("seed road block %d from the state sync proof file: %w", road, err)
	}
	if err := removeRestoreFiles(cfg.PersistentStateDir); err != nil {
		return err
	}
	logger.Info("Seeded the BlockDB from the state sync proof file", "road", road)
	return nil
}

// warnUnseededStart logs a warning when blockStore is empty while the app is past genesis.
func warnUnseededStart(cfg *GigaRouterCommonConfig, blockStore *ablockstore.Store) {
	if blockStore.Status().IsPresent() {
		return
	}
	appHeight := cfg.App.LastBlockHeight()
	if appHeight < cfg.GenDoc.InitialHeight {
		return
	}
	// Without a seed the node fetches blocks from genesis, and once every peer has pruned them
	// it waits with no error. This line is the only sign of that.
	logger.Warn("Giga state sync: the app is past genesis, the BlockDB is empty, and this home has no restore marker; the node fetches blocks from genesis",
		"app_height", appHeight, "marker", RestoreMarkerFile(cfg.PersistentStateDir))
}

// seedRecords names the records at the restored road that the seed writes.
type seedRecords struct {
	block       bool
	appProposal bool
	appQC       bool
}

// writeSeedRecords writes to blockStore each of road block road, AppProposal(road) and
// AppQC(road) that it does not hold, from the state sync proof file of the home.
func writeSeedRecords(
	ctx context.Context,
	cfg *GigaRouterCommonConfig,
	registry *epoch.Registry,
	blockStore *ablockstore.Store,
	road atypes.RoadIndex,
) error {
	missing, err := missingSeedRecords(blockStore, road)
	if err != nil {
		return err
	}
	if missing == (seedRecords{}) {
		return nil
	}
	path := RestoreStateSyncProofFile(cfg.PersistentStateDir)
	data, err := os.ReadFile(path) //nolint:gosec // path is the proof file under the node's own state dir.
	if err != nil {
		return fmt.Errorf("read the state sync proof file; run restore again: %w", err)
	}
	proof, appHash, err := VerifyStateSyncProof(registry, data, road)
	if err != nil {
		return err
	}
	if err := requireRestoreAnchor(ctx, cfg.AppHashBuilder, road, appHash); err != nil {
		return err
	}
	// A flush after each record keeps the durable records a prefix of the three, which the next
	// start completes. An AppQC without its AppProposal would fail the BlockDB on reopen.
	if missing.block {
		if err := blockStore.WriteBlock(proof.RoadBlock()); err != nil {
			return fmt.Errorf("write road block %d: %w", road, err)
		}
		if err := blockStore.Flush(); err != nil {
			return fmt.Errorf("flush road block %d: %w", road, err)
		}
	}
	if missing.appProposal {
		if err := blockStore.WriteAppProposal(proof.AppQC().Proposal()); err != nil {
			return fmt.Errorf("write AppProposal %d: %w", road, err)
		}
		if err := blockStore.Flush(); err != nil {
			return fmt.Errorf("flush AppProposal %d: %w", road, err)
		}
	}
	if missing.appQC {
		if err := blockStore.WriteAppQC(proof.AppQC()); err != nil {
			return fmt.Errorf("write AppQC %d: %w", road, err)
		}
		if err := blockStore.Flush(); err != nil {
			return fmt.Errorf("flush AppQC %d: %w", road, err)
		}
	}
	return nil
}

// missingSeedRecords returns the records at road that blockStore does not hold. It returns an
// error when blockStore holds a record at another road.
func missingSeedRecords(blockStore *ablockstore.Store, road atypes.RoadIndex) (seedRecords, error) {
	status, ok := blockStore.Status().Get()
	if !ok {
		return seedRecords{block: true, appProposal: true, appQC: true}, nil
	}
	if status.First != road || status.NextBlock > road+1 || status.NextAppProposal > road+1 || status.NextAppQC > road+1 {
		return seedRecords{}, fmt.Errorf("the BlockDB holds %+v, want at most the records of the restored road %d; this home was not restored by giga state sync alone", status, road)
	}
	return seedRecords{
		block:       status.NextBlock == road,
		appProposal: status.NextAppProposal == road,
		appQC:       status.NextAppQC == road,
	}, nil
}

// requireRestoreAnchor returns an error unless appHashBuilder holds appHash as the app hash of
// road.
func requireRestoreAnchor(ctx context.Context, appHashBuilder builder.AppHashBuilder, road atypes.RoadIndex, appHash [32]byte) error {
	entry, err := appHashBuilder.GetAppHash(ctx, uint64(road))
	if err != nil {
		return fmt.Errorf("read the restore anchor of road %d: %w", road, err)
	}
	if got := entry.AppHash(); got != appHash {
		return fmt.Errorf("the app-hash vault holds %x for road %d, but the AppQC of the state sync proof file signs %x; run restore again", got, road, appHash)
	}
	return nil
}

// removeRestoreFiles deletes the state sync proof file and then the restore marker of the
// persistent state dir psd.
func removeRestoreFiles(psd string) error {
	for _, path := range []string{RestoreStateSyncProofFile(psd), RestoreMarkerFile(psd)} {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("remove %s: %w", path, err)
		}
	}
	return nil
}
