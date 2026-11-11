package node

import (
	"os"
	"path/filepath"
	"testing"

	gigaconfig "github.com/sei-protocol/sei-chain/giga/config"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv"
	ablockstore "github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/blockstore"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/data"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/p2p"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
)

func TestConfigureStateSyncProofExportDisabled(t *testing.T) {
	cfg := &p2p.GigaRouterCommonConfig{}
	anchor := utils.NewAtomicSend(data.Anchor{})

	configureStateSyncProofExport(
		cfg,
		gigaconfig.StateSyncConfig{},
		t.TempDir(),
		ablockstore.NewTestStore(t),
		anchor.Subscribe(),
	)

	require.False(t, cfg.StateSyncProofExport.IsPresent())
}

func TestConfigureStateSyncProofExportEnabled(t *testing.T) {
	cfg := &p2p.GigaRouterCommonConfig{}
	psd := t.TempDir()
	anchor := utils.NewAtomicSend(data.Anchor{})
	checkpoint := filepath.Join(psd, "data", "state_commit", "flatkv", flatkv.SnapshotDirName(7))
	require.NoError(t, os.MkdirAll(checkpoint, 0o750))

	configureStateSyncProofExport(
		cfg,
		gigaconfig.StateSyncConfig{Enabled: true},
		psd,
		ablockstore.NewTestStore(t),
		anchor.Subscribe(),
	)

	export, ok := cfg.StateSyncProofExport.Get()
	require.True(t, ok)
	require.Equal(t, filepath.Join(psd, "data", "state_commit", "state_sync_proofs"), export.Dir)
	heights, err := export.CheckpointHeights()
	require.NoError(t, err)
	require.Equal(t, []int64{7}, heights)
}
