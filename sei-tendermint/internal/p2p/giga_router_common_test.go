package p2p

import (
	"context"
	"testing"
	"time"

	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/block/littblock"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/hashvault"
	atypes "github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
)

// newSeededVault returns a durable Pebble vault rooted in a temp dir with hash committed at height.
func newSeededVault(t *testing.T, height atypes.GlobalBlockNumber, hash []byte) hashvault.HashVault {
	t.Helper()
	cfg := hashvault.DefaultHashVaultConfig()
	cfg.DataDir = t.TempDir()
	v, err := hashvault.NewUnsafePebbleHashVault(context.Background(), cfg)
	require.NoError(t, err)
	t.Cleanup(func() { _ = v.Close(context.Background()) })
	require.NoError(t, v.CommitToHash(context.Background(), uint64(height), hash))
	return v
}

// TestCommitHashToVault covers the safety contract the restart path in runExecute relies on:
// an idempotent match returns nil, a divergent hash halts the node (panic), and a canceled
// context returns an error without halting.
func TestCommitHashToVault(t *testing.T) {
	const height atypes.GlobalBlockNumber = 42
	h1 := make([]byte, hashvault.BlockHashSize)
	for i := range h1 {
		h1[i] = 0xAA
	}
	h2 := make([]byte, hashvault.BlockHashSize)
	for i := range h2 {
		h2[i] = 0xBB
	}

	t.Run("matching hash is idempotent", func(t *testing.T) {
		vault := newSeededVault(t, height, h1)
		require.NoError(t, commitAppHashToVault(context.Background(), vault, height, h1))
	})

	t.Run("divergent hash halts the node", func(t *testing.T) {
		vault := newSeededVault(t, height, h1)
		require.Panics(t, func() {
			_ = commitAppHashToVault(context.Background(), vault, height, h2)
		})
	})

	t.Run("canceled context returns error without halting", func(t *testing.T) {
		vault := newSeededVault(t, height, h1)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		// Must not panic: a canceled context is a benign shutdown, not an equivocation.
		err := commitAppHashToVault(ctx, vault, height, h2)
		require.Error(t, err)
	})
}

func TestApplyBlockDBConfig(t *testing.T) {
	cfg, err := littblock.DefaultConfig(t.TempDir())
	require.NoError(t, err)
	require.Equal(t, 24*time.Hour, cfg.Retention)
	require.True(t, cfg.Litt.Fsync)

	applyBlockDBConfig(cfg, BlockDBConfig{
		Retention: utils.Some(30 * time.Second),
		GCPeriod:  utils.Some(5 * time.Second),
		Fsync:     utils.Some(false),
	})
	require.Equal(t, 30*time.Second, cfg.Retention)
	require.Equal(t, 5*time.Second, cfg.Litt.GCPeriod)
	require.False(t, cfg.Litt.Fsync)

	// Zero overrides leave an already-customized config unchanged.
	before := *cfg
	applyBlockDBConfig(cfg, BlockDBConfig{})
	require.Equal(t, before.Retention, cfg.Retention)
	require.Equal(t, before.Litt.GCPeriod, cfg.Litt.GCPeriod)
	require.Equal(t, before.Litt.Fsync, cfg.Litt.Fsync)
}
