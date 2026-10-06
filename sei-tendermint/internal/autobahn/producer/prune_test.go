package producer

import (
	"context"
	"fmt"
	"slices"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/proxy"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/scope"
)

type nonceGate struct {
	armed   bool
	blocked bool
}

// nonceGateApp is a testApp whose next EvmNonce call, once armed, blocks until released.
type nonceGateApp struct {
	*testApp
	ctx  context.Context
	gate utils.Watch[*nonceGate]
}

func newNonceGateApp(ctx context.Context) *nonceGateApp {
	return &nonceGateApp{
		testApp: newTestApp(),
		ctx:     ctx,
		gate:    utils.NewWatch(&nonceGate{}),
	}
}

func (a *nonceGateApp) Proxy() *proxy.Proxy {
	return proxy.New(a)
}

func (a *nonceGateApp) EvmNonce(addr common.Address) uint64 {
	for g, ctrl := range a.gate.Lock() {
		if g.armed {
			g.armed = false
			g.blocked = true
			ctrl.Updated()
			// A cancelled wait means the test is over; the nonce read below still completes.
			_ = ctrl.WaitUntil(a.ctx, func() bool { return !g.blocked })
		}
	}
	return a.testApp.EvmNonce(addr)
}

func (a *nonceGateApp) arm() {
	for g := range a.gate.Lock() {
		g.armed = true
	}
}

// waitBlocked blocks until an armed EvmNonce call is blocked.
func (a *nonceGateApp) waitBlocked(ctx context.Context) error {
	for g, ctrl := range a.gate.Lock() {
		return ctrl.WaitUntil(ctx, func() bool { return g.blocked })
	}
	panic("unreachable")
}

func (a *nonceGateApp) release() {
	for g, ctrl := range a.gate.Lock() {
		g.armed = false
		g.blocked = false
		ctrl.Updated()
	}
}

// setNonce sets the executed nonce of addr, as if the app executed its txs.
func (a *testApp) setNonce(addr common.Address, nonce uint64) {
	for inner := range a.inner.Lock() {
		inner.nonces[addr] = nonce
	}
}

// insertFullTxs inserts one block-filling tx of addr per nonce in [from, to).
// Each tx seals the block before it.
func (env *testEnv) insertFullTxs(ctx context.Context, rng utils.Rng, addr common.Address, from, to uint64) error {
	for nonce := from; nonce < to; nonce++ {
		tx := env.genTx(rng, addr, nonce)
		tx.GasWanted = env.state.cfg.MaxGasWantedPerBlock
		tx.GasEstimated = tx.GasWanted
		if _, err := env.state.InsertTx(ctx, tx.encode()); err != nil {
			return fmt.Errorf("InsertTx(nonce %d): %w", nonce, err)
		}
	}
	return nil
}

// firstBlock returns the oldest lane block still in the mempool.
func firstBlock(mp *mempool) types.BlockNumber {
	for m := range mp.inner.Lock() {
		return m.first
	}
	panic("unreachable")
}

// trackingBlocks returns how many blocks still in the mempool, the open one included, track addr.
func trackingBlocks(mp *mempool, addr common.Address) int {
	for m := range mp.inner.Lock() {
		n := 0
		for _, b := range m.blocks {
			if _, ok := b.evmNonces[addr]; ok {
				n++
			}
		}
		if _, ok := m.nextBlock.evmNonces[addr]; ok {
			n++
		}
		return n
	}
	panic("unreachable")
}

// Pruning keeps a sender's tracked nonce while its pruned txs got executed, and resets
// it to the app nonce once one of them did not.
func TestPruneMempool_TracksExecutedNonces(t *testing.T) {
	const txs = 3
	for _, tc := range []struct {
		name     string
		pruned   uint64 // lane blocks pruned, one tx of the sender each
		executed uint64 // txs of the sender the app executed
		want     uint64 // pending nonce of the sender, relative to its first tx
		tracking int    // blocks still tracking the sender
	}{
		{name: "all executed", pruned: 3, executed: 3, want: 3, tracking: 0},
		{name: "pruned txs executed", pruned: 2, executed: 2, want: 3, tracking: 1},
		{name: "first pruned tx failed", pruned: 1, executed: 0, want: 0, tracking: 0},
		{name: "last pruned tx failed", pruned: 2, executed: 1, want: 1, tracking: 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := t.Context()
			rng := utils.TestRng()
			app := newTestApp()
			env := newTestEnv(rng, app.Cfg(), app.Proxy())
			env.alignLocalMempool()
			mp := env.state.mempool.Load().OrPanic("aligned")
			addr, nonce := app.NewAccount(rng)
			require.NoError(t, env.insertFullTxs(ctx, rng, addr, nonce, nonce+txs))
			// Seal the block of the last tx.
			_, err := env.state.InsertTx(ctx, env.fullTx(rng, app).encode())
			require.NoError(t, err)

			app.setNonce(addr, nonce+tc.executed)
			env.state.pruneMempool(mp, firstBlock(mp)+types.BlockNumber(tc.pruned))

			require.Equal(t, nonce+tc.want, env.state.EvmNextPendingNonce(addr))
			require.Equal(t, tc.tracking, trackingBlocks(mp, addr))
			_, err = env.state.InsertTx(ctx, env.genTx(rng, addr, nonce+tc.want).encode())
			require.NoError(t, err)
		})
	}
}

// Inserts complete while pruning reads app nonces.
func TestPruneMempool_InsertsProceedDuringNonceRead(t *testing.T) {
	ctx := t.Context()
	rng := utils.TestRng()
	app := newNonceGateApp(ctx)
	env := newTestEnv(rng, app.Cfg(), app.Proxy())
	env.alignLocalMempool()
	mp := env.state.mempool.Load().OrPanic("aligned")
	addr, nonce := app.NewAccount(rng)
	require.NoError(t, env.insertFullTxs(ctx, rng, addr, nonce, nonce+2))

	const inserts = 5
	want := make([]*txSpec, 0, inserts)
	for range inserts {
		sender, senderNonce := app.NewAccount(rng)
		want = append(want, env.genTx(rng, sender, senderNonce))
	}
	app.arm()
	require.NoError(t, scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		defer app.release()
		s.Spawn(func() error {
			env.state.pruneMempool(mp, firstBlock(mp)+1)
			return nil
		})
		if err := app.waitBlocked(ctx); err != nil {
			return err
		}
		return scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
			for _, tx := range want {
				s.Spawn(func() error {
					_, err := env.state.InsertTx(ctx, tx.encode())
					return err
				})
			}
			return nil
		})
	}))
	got := env.state.UnconfirmedTxs()
	for _, tx := range want {
		require.True(t, slices.ContainsFunc(got, func(raw []byte) bool { return slices.Equal(raw, tx.encode()) }))
	}
}

// An insert of a sender whose tracking the prune then resets stays sequenced, but its
// tracking is reset with the rest; other senders keep theirs.
func TestPruneMempool_InsertDuringNonceRead(t *testing.T) {
	ctx := t.Context()
	rng := utils.TestRng()
	app := newNonceGateApp(ctx)
	env := newTestEnv(rng, app.Cfg(), app.Proxy())
	env.alignLocalMempool()
	mp := env.state.mempool.Load().OrPanic("aligned")
	addr, nonce := app.NewAccount(rng)
	require.NoError(t, env.insertFullTxs(ctx, rng, addr, nonce, nonce+2))
	other, otherNonce := app.NewAccount(rng)

	// The app executed none of addr's txs.
	tx := env.genTx(rng, addr, nonce+2)
	app.arm()
	require.NoError(t, scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		defer app.release()
		s.Spawn(func() error {
			env.state.pruneMempool(mp, firstBlock(mp)+1)
			return nil
		})
		if err := app.waitBlocked(ctx); err != nil {
			return err
		}
		if _, err := env.state.InsertTx(ctx, tx.encode()); err != nil {
			return fmt.Errorf("InsertTx(addr): %w", err)
		}
		if _, err := env.state.InsertTx(ctx, env.genTx(rng, other, otherNonce).encode()); err != nil {
			return fmt.Errorf("InsertTx(other): %w", err)
		}
		return nil
	}))

	require.Equal(t, nonce, env.state.EvmNextPendingNonce(addr))
	require.Equal(t, 0, trackingBlocks(mp, addr))
	require.True(t, slices.ContainsFunc(env.state.UnconfirmedTxs(), func(raw []byte) bool { return slices.Equal(raw, tx.encode()) }))
	require.Equal(t, otherNonce+1, env.state.EvmNextPendingNonce(other))
	require.Equal(t, 1, trackingBlocks(mp, other))
}

// A mempool closed while pruning reads app nonces is not modified afterwards.
func TestPruneMempool_CloseDuringNonceRead(t *testing.T) {
	ctx := t.Context()
	rng := utils.TestRng()
	app := newNonceGateApp(ctx)
	env := newTestEnv(rng, app.Cfg(), app.Proxy())
	env.alignLocalMempool()
	mp := env.state.mempool.Load().OrPanic("aligned")
	addr, nonce := app.NewAccount(rng)
	require.NoError(t, env.insertFullTxs(ctx, rng, addr, nonce, nonce+2))

	// The app executed none of addr's txs, so the prune resets addr unless closed first.
	app.arm()
	require.NoError(t, scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		defer app.release()
		s.Spawn(func() error {
			env.state.pruneMempool(mp, firstBlock(mp)+1)
			return nil
		})
		if err := app.waitBlocked(ctx); err != nil {
			return err
		}
		env.state.clearMempool()
		return nil
	}))

	var closed bool
	var tracked uint64
	for m := range mp.inner.Lock() {
		closed, tracked = m.closed, m.evmNonces[addr]
	}
	require.True(t, closed)
	require.Equal(t, nonce+2, tracked)
	require.Equal(t, 1, trackingBlocks(mp, addr))
}
