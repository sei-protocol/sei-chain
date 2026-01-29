package commands_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tendermint/tendermint/cmd/tendermint/commands"
	"github.com/tendermint/tendermint/libs/log"
	"github.com/tendermint/tendermint/rpc/client/local"
	rpctest "github.com/tendermint/tendermint/rpc/test"
	e2e "github.com/tendermint/tendermint/test/e2e/app"
)

func TestRollbackIntegration(t *testing.T) {
	var height int64
	dir := t.TempDir()
	cfg, err := rpctest.CreateConfig(t, t.Name())
	require.NoError(t, err)
	cfg.BaseConfig.DBBackend = "goleveldb"

	app, err := e2e.NewApplication(e2e.DefaultConfig(dir))
	require.NoError(t, err)

	t.Run("First run", func(t *testing.T) {
		ctx := t.Context()
		require.NoError(t, err)
		node, _, err := rpctest.StartTendermint(ctx, cfg, app, rpctest.SuppressStdout)
		require.NoError(t, err)
		require.True(t, node.IsRunning())

		// Wait for prev_app_state.json to exist (requires 2 block commits)
		// This is more reliable than a fixed sleep on slow CI runners
		prevStateFile := filepath.Join(dir, "prev_app_state.json")
		deadline := time.Now().Add(30 * time.Second)
		for time.Now().Before(deadline) {
			if _, err := os.Stat(prevStateFile); err == nil {
				break
			}
			time.Sleep(100 * time.Millisecond)
		}
		require.FileExists(t, prevStateFile, "prev_app_state.json should exist after 2 block commits")

		t.Cleanup(func() {
			node.Wait()
			require.False(t, node.IsRunning())
		})
	})
	t.Run("Rollback", func(t *testing.T) {
		time.Sleep(time.Second)
		require.NoError(t, app.Rollback())
		height, _, err = commands.RollbackState(cfg, false)
		require.NoError(t, err, "%d", height)
	})
	t.Run("Restart", func(t *testing.T) {
		require.True(t, height > 0, "%d", height)

		ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
		defer cancel()
		node2, _, err2 := rpctest.StartTendermint(ctx, cfg, app, rpctest.SuppressStdout)
		require.NoError(t, err2)
		t.Cleanup(node2.Wait)

		logger := log.NewNopLogger()

		client, err := local.New(logger, node2.(local.NodeService))
		require.NoError(t, err)

		ticker := time.NewTicker(200 * time.Millisecond)
		for {
			select {
			case <-ctx.Done():
				t.Fatalf("failed to make progress after 20 seconds. Min height: %d", height)
			case <-ticker.C:
				status, err := client.Status(ctx)
				require.NoError(t, err)

				if status.SyncInfo.LatestBlockHeight > height {
					return
				}
			}
		}
	})

}
