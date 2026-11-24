package bootstrap

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv"
	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/blockstore"
	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/statesyncproof"
	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/stretchr/testify/require"
)

// newTestExporter returns an exporter over an empty BlockStore whose checkpoints come from
// checkpointRoads.
func newTestExporter(t *testing.T, checkpointRoads func() ([]types.RoadHeight, error)) *statesyncproof.Exporter {
	t.Helper()
	return statesyncproof.NewExporter(statesyncproof.ExportConfig{
		Dir:             t.TempDir(),
		CheckpointRoads: checkpointRoads,
		BlockStore:      blockstore.NewTestStore(t),
		LastAppQC:       func() utils.Option[types.RoadHeight] { return utils.None[types.RoadHeight]() },
	})
}

func noCheckpoints() ([]types.RoadHeight, error) { return nil, nil }

func TestStateSyncProofExportConfig(t *testing.T) {
	cfg := newHome(t, nil)
	checkpoint := filepath.Join(cfg.FlatKVConfig.DataDir, flatkv.SnapshotDirName(7))
	require.NoError(t, os.MkdirAll(checkpoint, 0o750))

	exportCfg := stateSyncProofExportConfig(cfg, blockstore.NewTestStore(t))

	require.Equal(t, filepath.Join(cfg.HomePath, "data", "state_commit", "state_sync_proofs"), exportCfg.Dir)
	roads, err := exportCfg.CheckpointRoads()
	require.NoError(t, err)
	require.Equal(t, []types.RoadHeight{7}, roads)
	require.False(t, exportCfg.LastAppQC().IsPresent())
}

func TestLastAppQC(t *testing.T) {
	require.False(t, lastAppQC(utils.None[blockstore.SuffixRange]()).IsPresent())

	noAppQC := blockstore.SuffixRange{First: 5, NextAppQC: 5, NextAppProposal: 7, NextBlock: 8}
	require.False(t, lastAppQC(utils.Some(noAppQC)).IsPresent())

	withAppQC := blockstore.SuffixRange{First: 5, NextAppQC: 6, NextAppProposal: 7, NextBlock: 8}
	got, ok := lastAppQC(utils.Some(withAppQC)).Get()
	require.True(t, ok)
	require.Equal(t, types.RoadHeight(5), got)
}

func TestStartStateSyncProofExportTwiceFails(t *testing.T) {
	manager, _ := openManager(t, nil)

	require.NoError(t, manager.StartStateSyncProofExport())
	require.Error(t, manager.StartStateSyncProofExport())
}

func TestStartStateSyncProofExportAfterCloseFails(t *testing.T) {
	manager, err := NewGigaStorageManager(context.Background(), newHome(t, nil))
	require.NoError(t, err)
	require.NoError(t, manager.Close())

	require.Error(t, manager.StartStateSyncProofExport())
}

func TestStateSyncProofExportStopIsIdempotent(t *testing.T) {
	var export stateSyncProofExport
	newLoop := func() *stateSyncProofExportLoop {
		return startStateSyncProofExportLoop(newTestExporter(t, noCheckpoints), time.Hour)
	}
	require.NoError(t, export.start(newLoop))

	export.stop()
	export.stop()

	require.Error(t, export.start(newLoop))
}

func TestStateSyncProofExportStopWaitsForActiveSweep(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	// The interval is long, so only the first sweep runs.
	loop := startStateSyncProofExportLoop(newTestExporter(t, func() ([]types.RoadHeight, error) {
		close(entered)
		<-release
		return nil, nil
	}), time.Hour)
	<-entered

	stopped := make(chan struct{})
	go func() {
		loop.stop()
		close(stopped)
	}()
	select {
	case <-stopped:
		t.Fatal("stop returned during a sweep")
	default:
	}

	close(release)
	<-stopped
}

func TestStateSyncProofExportKeepsSweepingAfterFailure(t *testing.T) {
	var sweeps atomic.Int32
	secondSweep := make(chan struct{})
	loop := startStateSyncProofExportLoop(newTestExporter(t, func() ([]types.RoadHeight, error) {
		switch sweeps.Add(1) {
		case 1:
			return nil, errors.New("list failed")
		case 2:
			close(secondSweep)
		}
		return nil, nil
	}), time.Millisecond)

	<-secondSweep
	loop.stop()
}
