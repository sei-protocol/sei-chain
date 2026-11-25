package p2p

import (
	"context"
	"errors"
	"fmt"
	"os"

	dbutils "github.com/sei-protocol/sei-chain/sei-db/common/utils"
	ablockstore "github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/blockstore"
	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/statesyncproof"
	atypes "github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
)

// StateSyncStart is the giga state sync state of a home at node start.
type StateSyncStart struct {
	// Enabled is the [giga.state_sync] enabled switch.
	Enabled bool
	// PendingRestore is the road that the state sync restore marker of the home names, or None
	// when the home holds no marker.
	PendingRestore utils.Option[atypes.RoadHeight]
}

// seedBlockStore writes road block H, AppProposal(H) and AppQC(H) from the state sync proof file
// into blockStore when start names a pending restore of road H. Without a pending restore it
// writes nothing.
func seedBlockStore(
	ctx context.Context,
	cfg *GigaRouterCommonConfig,
	blockStore *ablockstore.Store,
	start StateSyncStart,
) error {
	road, ok := start.PendingRestore.Get()
	if !ok {
		if start.Enabled {
			warnUnseededStart(cfg, blockStore)
		}
		return nil
	}
	if err := writeSeedRecords(ctx, cfg, blockStore, road); err != nil {
		return fmt.Errorf("seed road block %d from the state sync proof file: %w", road, err)
	}
	logger.Info("Seeded the BlockDB from the state sync proof file", "road", road)
	return nil
}

// warnUnseededStart logs a warning when blockStore is empty while cfg.App is past genesis.
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
	logger.Warn("Giga state sync: the app is past genesis, the BlockDB is empty, and this home has no state sync restore marker; the node fetches blocks from genesis",
		"app_height", appHeight, "marker", RestoreMarkerFile(cfg.PersistentStateDir))
}

// writeSeedRecords writes to blockStore each of road block road, AppProposal(road) and
// AppQC(road) that it does not hold, from the state sync proof file of the home. It writes nothing
// unless the epoch registry of cfg.App holds the epoch of road and the epoch after it, and the
// proof verifies against the epoch of road.
func writeSeedRecords(
	ctx context.Context,
	cfg *GigaRouterCommonConfig,
	blockStore *ablockstore.Store,
	road atypes.RoadHeight,
) error {
	registry, err := cfg.App.EpochRegistry()
	if err != nil {
		return fmt.Errorf("read the epoch registry of the app: %w", err)
	}
	// data.NewState anchors a BlockDB that starts at road on these two epochs.
	if _, ok := registry.ForAnchor(road); !ok {
		return fmt.Errorf("the epoch registry of the app holds no epoch after the epoch of road %d, so a BlockDB cannot start at it; import a checkpoint of another height", road)
	}
	status := blockStore.Status().Or(ablockstore.SuffixRange{First: road, NextBlock: road, NextAppProposal: road, NextAppQC: road})
	// NextBlock bounds NextAppProposal and NextAppQC.
	if status.First != road || status.NextBlock > road+1 {
		return fmt.Errorf("the BlockDB holds %+v, want at most the records of the restored road %d; this home was not restored by giga state sync alone", status, road)
	}
	if status.NextAppQC > road {
		return nil
	}
	path := RestoreProofFile(cfg.PersistentStateDir)
	data, err := os.ReadFile(path) //nolint:gosec // path is the proof file under the node's own state dir.
	if err != nil {
		return fmt.Errorf("read the state sync proof file; run seid statesync import again: %w", err)
	}
	proof, appHash, err := statesyncproof.DecodeAndVerify(registry, data, road)
	if err != nil {
		return err
	}
	entry, err := cfg.AppHashBuilder.GetAppHash(ctx, uint64(road))
	if err != nil {
		return fmt.Errorf("read the restore anchor of road %d: %w", road, err)
	}
	if got := entry.AppHash(); got != appHash {
		return fmt.Errorf("the app-hash vault holds %x for road %d, but the AppQC of the state sync proof file signs %x; run seid statesync import again", got, road, appHash)
	}
	// A flush after each record keeps the durable records a prefix of the three, which the next
	// start completes. An AppQC without its AppProposal would fail the BlockDB on reopen.
	if status.NextBlock == road {
		write := func() error { return blockStore.WriteBlock(proof.RoadBlock()) }
		if err := writeAndFlush(blockStore, fmt.Sprintf("road block %d", road), write); err != nil {
			return err
		}
	}
	if status.NextAppProposal == road {
		write := func() error { return blockStore.WriteAppProposal(proof.AppQC().Proposal()) }
		if err := writeAndFlush(blockStore, fmt.Sprintf("AppProposal %d", road), write); err != nil {
			return err
		}
	}
	write := func() error { return blockStore.WriteAppQC(proof.AppQC()) }
	if err := writeAndFlush(blockStore, fmt.Sprintf("AppQC %d", road), write); err != nil {
		return err
	}
	return nil
}

// writeAndFlush runs write, which writes record to blockStore, and makes record durable.
func writeAndFlush(blockStore *ablockstore.Store, record string, write func() error) error {
	if err := write(); err != nil {
		return fmt.Errorf("write %s: %w", record, err)
	}
	if err := blockStore.Flush(); err != nil {
		return fmt.Errorf("flush %s: %w", record, err)
	}
	return nil
}

// removeRestoreFiles deletes the state sync proof file and then the restore marker of the
// persistent state dir psd, and syncs psd.
func removeRestoreFiles(psd string) error {
	for _, path := range []string{RestoreProofFile(psd), RestoreMarkerFile(psd)} {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("remove %s: %w", path, err)
		}
	}
	return dbutils.SyncDir(psd)
}
