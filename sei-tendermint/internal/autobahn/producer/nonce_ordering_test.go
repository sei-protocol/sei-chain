package producer

import (
	"context"
	"fmt"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/consensus"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/proxy"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/scope"
)

type nonceReadGate struct {
	enabled  bool
	entered  bool
	released bool
}

type nonceOrderingApp struct {
	*testApp
	ctx  context.Context
	gate utils.Watch[*nonceReadGate]
}

func (a *nonceOrderingApp) EvmNonce(addr common.Address) uint64 {
	nonce := a.testApp.EvmNonce(addr)
	for gate, ctrl := range a.gate.Lock() {
		if gate.enabled {
			gate.entered = true
			ctrl.Updated()
			// Return the snapshot even if execution advances while this read is blocked.
			_ = ctrl.WaitUntil(a.ctx, func() bool { return gate.released })
		}
	}
	return nonce
}

func TestInsertTx_PendingNoncesReadBeforeAppNonce(t *testing.T) {
	rng := utils.TestRng()
	app := &nonceOrderingApp{
		testApp: newTestApp(),
		ctx:     t.Context(),
		gate:    utils.NewWatch(&nonceReadGate{}),
	}
	cfg := app.Cfg()
	cfg.BlockInterval = 0
	cfg.MaxTxsPerBlock = 1
	env := newTestEnv(rng, cfg, proxy.New(app))
	addr, nonce := app.NewAccount(rng)
	tx1 := env.genTx(rng, addr, nonce)
	tx2 := env.genTx(rng, addr, nonce+1)
	tx3 := env.genTx(rng, addr, nonce+2)

	require.NoError(t, scope.Run(t.Context(), func(ctx context.Context, s scope.Scope) error {
		s.SpawnBg(func() error { return utils.IgnoreCancel(env.data.Run(ctx)) })
		s.SpawnBg(func() error { return utils.IgnoreCancel(env.consensus.Run(ctx)) })
		s.SpawnBg(func() error { return utils.IgnoreCancel(env.state.Run(ctx)) })
		s.SpawnBg(func() error {
			return utils.IgnoreCancel(consensus.RunTestNetwork(ctx, utils.Slice(env.consensus)))
		})
		mp, err := env.waitForMempool(ctx)
		if err != nil {
			return err
		}
		if _, err := env.state.InsertTx(ctx, tx1.encode()); err != nil {
			return err
		}
		index := env.data.Registry().MustEpoch(0).RoadRange().First
		block, err := env.data.SuperBlock(ctx, index)
		if err != nil {
			return err
		}
		if err := utils.TestDiff([][]byte{tx1.encode()}, block.Txs); err != nil {
			return err
		}
		if _, err := env.state.InsertTx(ctx, tx2.encode()); err != nil {
			return err
		}
		for gate := range app.gate.Lock() {
			gate.enabled = true
		}
		return scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
			app.ctx = ctx
			s.Spawn(func() error {
				resp, err := env.state.InsertTx(ctx, tx3.encode())
				if err != nil {
					return fmt.Errorf("InsertTx(tx3): %w", err)
				}
				return utils.TestDiff(tx3.asResponse().ResponseCheckTx, resp)
			})
			for gate, ctrl := range app.gate.Lock() {
				if err := ctrl.WaitUntil(ctx, func() bool { return gate.entered }); err != nil {
					return err
				}
			}
			resp, err := app.FinalizeBlock(ctx, &abci.RequestFinalizeBlock{Txs: block.Txs, Hash: block.Hash[:]})
			if err != nil {
				return err
			}
			// PushAppHash records the AppProposal and lets the producer prune tx1.
			if err := env.data.PushAppHash(ctx, index, resp.AppHash, nil); err != nil {
				return err
			}
			for inner, ctrl := range mp.inner.Lock() {
				if err := ctrl.WaitUntil(ctx, func() bool {
					r, ok := inner.evmNonces[addr]
					return ok && r.first == tx2.Nonce && r.next == tx3.Nonce
				}); err != nil {
					return err
				}
			}
			for gate, ctrl := range app.gate.Lock() {
				gate.released = true
				ctrl.Updated()
			}
			return nil
		})
	}))
}
