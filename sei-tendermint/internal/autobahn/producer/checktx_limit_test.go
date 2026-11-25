package producer

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"testing"
	"testing/synctest"

	abevm "github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/evm"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/scope"
)

type gatedCheckTx struct {
	open     bool
	inflight uint64
	maxSeen  uint64
}

// gatedApp is a testApp whose CheckTx blocks until the gate is opened,
// tracking how many CheckTx calls are in flight at once.
type gatedApp struct {
	*testApp
	gate utils.Watch[*gatedCheckTx]
	ctx  context.Context
}

func newGatedApp(ctx context.Context) *gatedApp {
	return &gatedApp{
		testApp: newTestApp(),
		ctx:     ctx,
		gate:    utils.NewWatch(&gatedCheckTx{}),
	}
}

func (a *gatedApp) CheckTx(req abevm.EvmTx) (abevm.AdmittedTx, error) {
	for g, ctrl := range a.gate.Lock() {
		g.inflight += 1
		g.maxSeen = max(g.maxSeen, g.inflight)
		ctrl.Updated()
		err := ctrl.WaitUntil(a.ctx, func() bool { return g.open })
		g.inflight -= 1
		ctrl.Updated()
		if err != nil {
			return abevm.AdmittedTx{}, err
		}
	}
	return a.testApp.CheckTx(req)
}

// waitInflight blocks until exactly n CheckTx calls are in flight.
func (a *gatedApp) waitInflight(ctx context.Context, n uint64) error {
	for g, ctrl := range a.gate.Lock() {
		return ctrl.WaitUntil(ctx, func() bool { return g.inflight == n })
	}
	panic("unreachable")
}

func (a *gatedApp) openGate() {
	for g, ctrl := range a.gate.Lock() {
		g.open = true
		ctrl.Updated()
	}
}

func (a *gatedApp) maxInflight() uint64 {
	for g := range a.gate.Lock() {
		return g.maxSeen
	}
	panic("unreachable")
}

// With limit N and M>N concurrent inserts, at most N CheckTx calls run at once
// and every tx is still admitted.
func TestInsertTx_BoundsConcurrentCheckTx(t *testing.T) {
	limit := uint64(2)
	ctx := t.Context()
	rng := utils.TestRng()
	app := newGatedApp(ctx)
	cfg := app.Cfg()
	cfg.MaxConcurrentCheckTx = utils.Some(limit)
	require.NoError(t, scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		defer app.openGate()
		env := newTestEnv(t, rng, cfg, app)
		s.SpawnBg(func() error { return env.Run(ctx) })
		if _, err := env.waitForMempool(ctx); err != nil {
			return err
		}

		const m = 6
		txs := make([]abevm.EvmTxRaw, 0, m)
		for range m {
			addr, nonce := app.NewAccount(rng)
			txs = append(txs, env.genTx(rng, addr, nonce))
		}
		if err := scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
			for _, tx := range txs {
				s.Spawn(func() error {
					err := env.state.InsertTx(ctx, tx)
					return err
				})
			}
			if err := app.waitInflight(ctx, limit); err != nil {
				return err
			}
			app.openGate()
			return nil
		}); err != nil {
			return err
		}
		if err := utils.TestDiff(limit, app.maxInflight()); err != nil {
			return err
		}
		if err := utils.TestDiff(m, pendingTxCount(env.state)); err != nil {
			return err
		}
		return nil
	}))
}

// Waiters cancelled while queued for a CheckTx permit return ctx error and
// do not consume a permit, so later inserts still get through.
func TestInsertTx_CancelledCheckTxWaiterReleasesPermit(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx := t.Context()
		rng := utils.TestRng()
		app := newGatedApp(ctx)
		cfg := app.Cfg()
		cfg.MaxConcurrentCheckTx = utils.Some[uint64](1)
		env := newTestEnv(t, rng, cfg, app)
		require.NoError(t, scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
			s.SpawnBg(func() error { return env.Run(ctx) })
			if _, err := env.waitForMempool(ctx); err != nil {
				return err
			}

			newTx := func() abevm.EvmTxRaw {
				addr, nonce := app.NewAccount(rng)
				return env.genTx(rng, addr, nonce)
			}
			if err := scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
				defer app.openGate()
				holder := newTx()
				s.Spawn(func() error {
					err := env.state.InsertTx(ctx, holder)
					return err
				})
				// The single permit is now held by holder, blocked in CheckTx.
				if err := app.waitInflight(ctx, 1); err != nil {
					return err
				}
				waitCtx, cancel := context.WithCancel(ctx)
				if err := scope.Run(waitCtx, func(ctx context.Context, s scope.Scope) error {
					for range 4 {
						tx := newTx()
						s.Spawn(func() error {
							err := env.state.InsertTx(ctx, tx)
							if !errors.Is(err, context.Canceled) {
								return fmt.Errorf("InsertTx() = %v, want context.Canceled", err)
							}
							return nil
						})
					}
					synctest.Wait()
					cancel()
					return nil
				}); err != nil {
					return err
				}
				app.openGate()
				// Once holder finishes, a fresh insert must acquire the permit.
				err := env.state.InsertTx(ctx, newTx())
				return err
			}); err != nil {
				return err
			}
			if err := utils.TestDiff(1, app.maxInflight()); err != nil {
				return err
			}
			if err := utils.TestDiff(2, pendingTxCount(env.state)); err != nil {
				return err
			}
			return nil
		}))
	})
}

func TestConfig_MaxConcurrentCheckTxDefault(t *testing.T) {
	cfg := &Config{}
	require.True(t, cfg.maxConcurrentCheckTx() >= 1)
	require.Equal(t, max(1, runtime.GOMAXPROCS(0)/2), cfg.maxConcurrentCheckTx())
	cfg.MaxConcurrentCheckTx = utils.Some[uint64](0)
	require.Equal(t, max(1, runtime.GOMAXPROCS(0)/2), cfg.maxConcurrentCheckTx())
	cfg.MaxConcurrentCheckTx = utils.Some[uint64](7)
	require.Equal(t, 7, cfg.maxConcurrentCheckTx())
}
