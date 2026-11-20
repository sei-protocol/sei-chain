package p2p

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/sei-protocol/sei-chain/giga/apphash/builder"
	ablockstore "github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/blockstore"
	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/statesyncproof"
	atypes "github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
)

// ReadRestoreMarker returns the road that the state sync restore marker in the persistent state
// dir psd names, or None when psd holds no marker.
func ReadRestoreMarker(psd string) (utils.Option[atypes.RoadHeight], error) {
	path := RestoreMarkerFile(psd)
	raw, err := os.ReadFile(path) //nolint:gosec // path is the marker under the node's own state dir.
	if errors.Is(err, os.ErrNotExist) {
		return utils.None[atypes.RoadHeight](), nil
	}
	if err != nil {
		return utils.None[atypes.RoadHeight](), fmt.Errorf("read the state sync restore marker %s: %w", path, err)
	}
	road, err := strconv.ParseUint(strings.TrimSuffix(string(raw), "\n"), 10, 64)
	if err != nil || road == 0 {
		return utils.None[atypes.RoadHeight](), fmt.Errorf("the state sync restore marker %s holds %q, want one positive decimal height", path, raw)
	}
	return utils.Some(atypes.RoadHeight(road)), nil
}

// SeedRestoredBlockStore writes road block H, AppProposal(H) and AppQC(H) from the state sync proof
// file of the persistent state dir psd into blockStore when psd holds a restore marker of road H,
// and then removes the proof file and the marker. The proof must verify against the epoch of H in
// registry and sign the app hash that appHashBuilder holds for H. Without a marker it writes
// nothing.
func SeedRestoredBlockStore(
	ctx context.Context,
	psd string,
	registry *atypes.EpochRegistry,
	appHashBuilder builder.AppHashBuilder,
	blockStore *ablockstore.Store,
) error {
	marked, err := ReadRestoreMarker(psd)
	if err != nil {
		return err
	}
	road, ok := marked.Get()
	if !ok {
		return nil
	}
	if err := writeSeedRecords(ctx, psd, registry, appHashBuilder, blockStore, road); err != nil {
		return fmt.Errorf("seed road block %d from the state sync proof file: %w", road, err)
	}
	if err := removeRestoreFiles(psd); err != nil {
		return err
	}
	logger.Info("Seeded the BlockDB from the state sync proof file", "road", road)
	return nil
}

// seedRecords names the records at the restored road that the seed writes.
type seedRecords struct {
	block       bool
	appProposal bool
	appQC       bool
}

// writeSeedRecords writes to blockStore each of road block road, AppProposal(road) and
// AppQC(road) that it does not hold, from the state sync proof file in psd.
func writeSeedRecords(
	ctx context.Context,
	psd string,
	registry *atypes.EpochRegistry,
	appHashBuilder builder.AppHashBuilder,
	blockStore *ablockstore.Store,
	road atypes.RoadHeight,
) error {
	missing, err := missingSeedRecords(blockStore, road)
	if err != nil {
		return err
	}
	if missing == (seedRecords{}) {
		return nil
	}
	data, err := os.ReadFile(RestoreProofFile(psd)) //nolint:gosec // the proof file is under the node's own state dir.
	if err != nil {
		return fmt.Errorf("read the state sync proof file; run seid statesync import again: %w", err)
	}
	proof, appHash, err := verifyRestoreProof(registry, data, road)
	if err != nil {
		return err
	}
	if err := requireRestoreAnchor(ctx, appHashBuilder, road, appHash); err != nil {
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

// verifyRestoreProof decodes the state sync proof file data of road and verifies its road block and
// AppQC against the epoch of road in registry. It returns the proof and the app hash that its
// AppQC signs.
func verifyRestoreProof(registry *atypes.EpochRegistry, data []byte, road atypes.RoadHeight) (*statesyncproof.Proof, [32]byte, error) {
	proof, err := statesyncproof.Decode(data, road)
	if err != nil {
		return nil, [32]byte{}, fmt.Errorf("decode the state sync proof of road %d: %w", road, err)
	}
	epochs, ok := registry.ForAnchor(road)
	if !ok {
		return nil, [32]byte{}, fmt.Errorf("the epoch registry of the restored state does not hold the epoch of road %d and the epoch after it", road)
	}
	ep := epochs[0]
	if err := proof.RoadBlock().Verify(ep); err != nil {
		return nil, [32]byte{}, fmt.Errorf("verify road block %d against epoch %d: %w", road, ep.EpochIndex(), err)
	}
	if err := proof.AppQC().Verify(ep.Committee()); err != nil {
		return nil, [32]byte{}, fmt.Errorf("verify AppQC %d against epoch %d: %w", road, ep.EpochIndex(), err)
	}
	appHash := proof.AppQC().Proposal().AppHash()
	if len(appHash) != 32 {
		return nil, [32]byte{}, fmt.Errorf("the AppQC of road %d signs an app hash of %d bytes, want 32", road, len(appHash))
	}
	return proof, [32]byte(appHash), nil
}

// missingSeedRecords returns the records at road that blockStore does not hold. It returns an
// error when blockStore holds a record at another road.
func missingSeedRecords(blockStore *ablockstore.Store, road atypes.RoadHeight) (seedRecords, error) {
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
func requireRestoreAnchor(ctx context.Context, appHashBuilder builder.AppHashBuilder, road atypes.RoadHeight, appHash [32]byte) error {
	entry, err := appHashBuilder.GetAppHash(ctx, uint64(road))
	if err != nil {
		return fmt.Errorf("read the restore anchor of road %d: %w", road, err)
	}
	if got := entry.AppHash(); got != appHash {
		return fmt.Errorf("the app-hash vault holds %x for road %d, but the AppQC of the state sync proof file signs %x; run seid statesync import again", got, road, appHash)
	}
	return nil
}

// removeRestoreFiles deletes the state sync proof file and then the restore marker of the
// persistent state dir psd.
func removeRestoreFiles(psd string) error {
	for _, path := range []string{RestoreProofFile(psd), RestoreMarkerFile(psd)} {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("remove %s: %w", path, err)
		}
	}
	return nil
}
