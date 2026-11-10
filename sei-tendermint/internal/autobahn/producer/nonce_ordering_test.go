package producer

import (
	"context"
	"fmt"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/proxy"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/scope"
)

type nonceOrderingGate struct {
	pauseEVMNonce      bool
	pauseFinalizeBlock bool
}

type nonceOrderingApp struct {
	t *testing.T
	*testApp
	gate utils.Watch[*nonceOrderingGate]
}

func (a *nonceOrderingApp) EvmNonce(addr common.Address) uint64 {
	nonce := a.testApp.EvmNonce(addr)
	for gate, ctrl := range a.gate.Lock() {
		if gate.pauseEVMNonce {
			gate.pauseFinalizeBlock = false
			ctrl.Updated()
			// Return the snapshot even if execution advances while this read is blocked.
			_ = ctrl.WaitUntil(a.t.Context(), func() bool { return !gate.pauseEVMNonce })
		}
	}
	return nonce
}

func (a *nonceOrderingApp) FinalizeBlock(ctx context.Context, req *abci.RequestFinalizeBlock) (*abci.ResponseFinalizeBlock, error) {
	for gate, ctrl := range a.gate.Lock() {
		if err := ctrl.WaitUntil(ctx, func() bool { return !gate.pauseFinalizeBlock }); err != nil {
			return nil, err
		}
	}
	return a.testApp.FinalizeBlock(ctx, req)
}

// Test checking that evmNextPendingNonce accesses
// app.EVMNonce and mempool.evmNonces in the correct order.
// NOTE: this is a fragile test, remove if no longer relevant
// or too hard to maintain.
func TestEVMNextPendingNonce_race(t *testing.T) {
	rng := utils.TestRng()
	app := &nonceOrderingApp{
		t:       t,
		testApp: newTestApp(),
		gate:    utils.NewWatch(&nonceOrderingGate{pauseFinalizeBlock: true}),
	}
	cfg := app.Cfg()
	cfg.BlockInterval = 0
	cfg.MaxTxsPerBlock = 1
	env := newTestEnv(t, rng, cfg, proxy.NewTestEVMProxy(app))
	addr, nonce := app.NewAccount(rng)
	tx1 := env.genTx(rng, addr, nonce)
	tx2 := env.genTx(rng, addr, nonce+1)

	require.NoError(t, scope.Run(t.Context(), func(ctx context.Context, s scope.Scope) error {
		s.SpawnBg(func() error { return env.Run(ctx) })
		mp, err := env.waitForMempool(ctx)
		if err != nil {
			return err
		}
		t.Log("sequence tx1")
		if _, err := env.state.InsertTx(ctx, tx1.encode()); err != nil {
			return err
		}
		index := env.data.Registry().MustEpoch(0).RoadRange().First
		firstBlock, err := env.data.SuperBlock(ctx, index)
		if err != nil {
			return err
		}
		if err := utils.TestDiff([][]byte{tx1.encode()}, firstBlock.Txs); err != nil {
			return err
		}
		t.Log("insert tx2")
		if _, err := env.state.InsertTx(ctx, tx2.encode()); err != nil {
			return err
		}
		// We test evmNextPendingNonce directly instead of InsertTx or EVMNextPendingNonce,
		// because this test wouldn't make sense if evmNextPendingNonce() call was removed from either.
		t.Log("pause EVMNonce()")
		for gate, ctrl := range app.gate.Lock() {
			gate.pauseEVMNonce = true
			ctrl.Updated()
		}
		t.Log("start evmNextPendingNonce()")
		s.Spawn(func() error {
			// WARNING: this call is blocking because we made EVMNonce blocking
			// for test purposes.
			if got, want := env.state.evmNextPendingNonce(mp, addr), nonce+2; got != want {
				return fmt.Errorf("evmNextPendingNonce() = %v, want %v", got, want)
			}
			return nil
		})
		t.Log("wait for tx1 to be pruned from mempool")
		for inner, ctrl := range mp.inner.Lock() {
			if err := ctrl.WaitUntil(ctx, func() bool {
				r, ok := inner.evmNonces[addr]
				return ok && r.first == tx2.Nonce
			}); err != nil {
				return err
			}
		}
		t.Log("unpause EVMNonce()")
		for gate, ctrl := range app.gate.Lock() {
			gate.pauseEVMNonce = false
			ctrl.Updated()
		}
		return nil
	}))
}
