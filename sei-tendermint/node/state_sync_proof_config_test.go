package node

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	gigaconfig "github.com/sei-protocol/sei-chain/giga/config"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv"
	ablockstore "github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/blockstore"
	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/statesyncproof"
	atypes "github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/data"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/scope"
)

func TestNewStateSyncProofExporter(t *testing.T) {
	for name, enabled := range map[string]bool{"disabled": false, "enabled": true} {
		t.Run(name, func(t *testing.T) {
			anchor := utils.NewAtomicSend(data.Anchor{})

			exporter := newStateSyncProofExporter(
				gigaconfig.StateSyncConfig{Enabled: enabled},
				t.TempDir(),
				ablockstore.NewTestStore(t),
				anchor.Subscribe(),
				noClosingEpoch,
			)

			require.Equal(t, enabled, exporter.IsPresent())
		})
	}
}

func TestStateSyncProofExportConfig(t *testing.T) {
	psd := t.TempDir()
	anchor := utils.NewAtomicSend(data.Anchor{})
	checkpoint := filepath.Join(psd, "data", "state_commit", "flatkv", flatkv.SnapshotDirName(7))
	require.NoError(t, os.MkdirAll(checkpoint, 0o750))
	closingRoad := atypes.RoadIndex(9)

	cfg := stateSyncProofExportConfig(
		psd,
		ablockstore.NewTestStore(t),
		anchor.Subscribe(),
		func(road atypes.RoadIndex) utils.Option[atypes.EpochIndex] {
			if road == closingRoad {
				return utils.Some(atypes.EpochIndex(0))
			}
			return utils.None[atypes.EpochIndex]()
		},
	)

	require.Equal(t, filepath.Join(psd, "data", "state_commit", "state_sync_proofs"), cfg.Dir)
	heights, err := cfg.CheckpointHeights()
	require.NoError(t, err)
	require.Equal(t, []int64{7}, heights)
	require.True(t, cfg.IsClosingRoad(closingRoad))
	require.False(t, cfg.IsClosingRoad(closingRoad-1))

	_, ok := cfg.DurableAppQC()
	require.False(t, ok)
	rng := utils.TestRng()
	committee, keys := atypes.GenCommittee(rng, 4)
	epoch := atypes.GenEpochWithCommittee(rng, committee)
	fullQC, _ := data.TestCommitQC(rng, epoch, keys, utils.None[*atypes.CommitQC]())
	qc := fullQC.QC()
	appQC := data.TestAppQC(keys, atypes.NewAppProposal(qc.View(), []byte{1}))
	durable := data.TestAnchor(epoch)
	durable.First = utils.Some(data.AnchorFirst{CommitQC: qc, AppQC: appQC})
	anchor.Store(durable)
	index, ok := cfg.DurableAppQC()
	require.True(t, ok)
	require.Equal(t, appQC.Proposal().Index(), index)
}

func TestRunStateSyncProofExportKeepsSweepingAfterFailure(t *testing.T) {
	// Nothing changes between sweeps, as on a halted chain, so only the timer can start a sweep.
	sweeps := utils.NewAtomicSend(0)
	exporter := statesyncproof.NewExporter(statesyncproof.ExportConfig{
		Dir: t.TempDir(),
		CheckpointHeights: func() ([]int64, error) {
			n := sweeps.Load() + 1
			sweeps.Store(n)
			if n == 1 {
				return nil, errors.New("list failed")
			}
			return nil, nil
		},
		BlockStore:    ablockstore.NewTestStore(t),
		DurableAppQC:  func() (atypes.RoadIndex, bool) { return 0, false },
		IsClosingRoad: func(atypes.RoadIndex) bool { return false },
	})

	err := scope.Run(t.Context(), func(ctx context.Context, s scope.Scope) error {
		s.SpawnBg(func() error {
			return utils.IgnoreCancel(runStateSyncProofExport(ctx, exporter, time.Millisecond))
		})
		_, err := sweeps.Subscribe().Wait(ctx, func(n int) bool { return n >= 2 })
		return err
	})

	require.NoError(t, err)
}

func noClosingEpoch(atypes.RoadIndex) utils.Option[atypes.EpochIndex] {
	return utils.None[atypes.EpochIndex]()
}
