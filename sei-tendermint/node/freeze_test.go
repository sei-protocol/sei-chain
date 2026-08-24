package node

import (
	"math"
	"testing"

	"github.com/sei-protocol/sei-chain/sei-tendermint/config"
	mempoolreactor "github.com/sei-protocol/sei-chain/sei-tendermint/internal/mempool/reactor"
)

func TestValidateFreezeHeight(t *testing.T) {
	for _, tc := range []struct {
		name          string
		freezeHeight  uint64
		initialHeight int64
		stateHeight   int64
		blockHeight   int64
		appHeight     int64
		wantErr       bool
	}{
		{name: "disabled"},
		{name: "below target", freezeHeight: 10, initialHeight: 1, stateHeight: 8, blockHeight: 9, appHeight: 8},
		{name: "immediately before target", freezeHeight: 10, initialHeight: 1, stateHeight: 9, blockHeight: 9, appHeight: 9},
		{name: "target below initial height", freezeHeight: 9, initialHeight: 10, wantErr: true},
		{name: "state at target", freezeHeight: 10, initialHeight: 1, stateHeight: 10, wantErr: true},
		{name: "block store at target", freezeHeight: 10, initialHeight: 1, blockHeight: 10, wantErr: true},
		{name: "application at target", freezeHeight: 10, initialHeight: 1, appHeight: 10, wantErr: true},
		{name: "target above max height", freezeHeight: uint64(math.MaxInt64) + 1, initialHeight: 1, wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := validateFreezeHeight(tc.freezeHeight, tc.initialHeight, tc.stateHeight, tc.blockHeight, tc.appHeight)
			if (err != nil) != tc.wantErr {
				t.Fatalf("validateFreezeHeight() error = %v, wantErr %t", err, tc.wantErr)
			}
		})
	}
}

func TestWithFreezeHeight(t *testing.T) {
	const height = uint64(123)
	if got := resolveOptions(WithFreezeHeight(height)).freezeHeight; got != height {
		t.Fatalf("freeze height = %d, want %d", got, height)
	}
}

func TestFreezeModeDisablesMempoolTraffic(t *testing.T) {
	cfg, err := config.ResetTestRoot(t.TempDir(), "freeze_mempool_test")
	if err != nil {
		t.Fatal(err)
	}
	nodeService, err := newLocalNodeService(t.Context(), cfg, WithFreezeHeight(2))
	if err != nil {
		t.Fatal(err)
	}
	node := nodeService.(*nodeImpl)
	t.Cleanup(func() {
		_ = node.shutdownOps()
		_ = node.blockStore.Close()
		_ = node.stateStore.Close()
	})

	if !node.mempool.IsPresent() {
		t.Fatal("internal mempool is unavailable")
	}
	if node.rpcEnv.Mempool.IsPresent() {
		t.Fatal("RPC mempool is available in freeze mode")
	}
	for _, nodeService := range node.services {
		if _, ok := nodeService.(*mempoolreactor.Reactor); ok {
			t.Fatal("mempool reactor is enabled in freeze mode")
		}
	}
}
