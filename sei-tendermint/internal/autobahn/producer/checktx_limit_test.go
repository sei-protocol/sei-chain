package producer

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"testing"

	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/proxy"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/scope"
)

type gatedCheckTx struct {
	open     bool
	inflight int
	maxSeen  int
}

// gatedApp is a testApp whose CheckTx blocks until the gate is opened,
// tracking how many CheckTx calls are in flight at once.
type gatedApp struct {
	*testApp
	gate utils.Watch[*gatedCheckTx]
}

func newGatedApp() *gatedApp {
	return &gatedApp{
		testApp: newTestApp(),
		gate:    utils.NewWatch(&gatedCheckTx{}),
	}
}

func (a *gatedApp) Proxy() *proxy.Proxy {
	return proxy.New(a)
}

func (a *gatedApp) CheckTx(ctx context.Context, req *abci.RequestCheckTxV2) *abci.ResponseCheckTxV2 {
	for g, ctrl := range a.gate.Lock() {
		g.inflight += 1
		g.maxSeen = max(g.maxSeen, g.inflight)
		ctrl.Updated()
		err := ctrl.WaitUntil(ctx, func() bool { return g.open })
		g.inflight -= 1
		ctrl.Updated()
		if err != nil {
			return &abci.ResponseCheckTxV2{ResponseCheckTx: &abci.ResponseCheckTx{Code: 1, Log: err.Error()}}
		}
	}
	return a.testApp.CheckTx(ctx, req)
}

// waitInflight blocks until exactly n CheckTx calls are in flight.
func (a *gatedApp) waitInflight(ctx context.Context, n int) error {
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

func (a *gatedApp) maxInflight() int {
	for g := range a.gate.Lock() {
		return g.maxSeen
	}
	panic("unreachable")
}

// With limit N and M>N concurrent inserts, at most N CheckTx calls run at once
// and every tx is still admitted.
func TestInsertTx_BoundsConcurrentCheckTx(t *testing.T) {
	for _, limit := range utils.Slice(1, 2, 3) {
		t.Run(fmt.Sprint(limit), func(t *testing.T) {
			ctx := t.Context()
			rng := utils.TestRng()
			app := newGatedApp()
			cfg := app.Cfg()
			cfg.MaxConcurrentCheckTx = utils.Some(uint64(limit)) //nolint:gosec // small test constant
			env := newTestEnv(rng, cfg, app.Proxy())
			env.alignLocalMempool()

			const m = 6
			txs := make([][]byte, 0, m)
			for range m {
				addr, nonce := app.NewAccount(rng)
				txs = append(txs, env.genTx(rng, addr, nonce).encode())
			}
			require.NoError(t, scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
				for _, tx := range txs {
					s.Spawn(func() error {
						_, err := env.state.InsertTx(ctx, tx)
						return err
					})
				}
				if err := app.waitInflight(ctx, limit); err != nil {
					return err
				}
				app.openGate()
				return nil
			}))
			require.Equal(t, limit, app.maxInflight())
			require.Equal(t, m, len(env.state.UnconfirmedTxs()))
		})
	}
}

// Waiters cancelled while queued for a CheckTx permit return ctx error and
// do not consume a permit, so later inserts still get through.
func TestInsertTx_CancelledCheckTxWaiterReleasesPermit(t *testing.T) {
	ctx := t.Context()
	rng := utils.TestRng()
	app := newGatedApp()
	cfg := app.Cfg()
	cfg.MaxConcurrentCheckTx = utils.Some[uint64](1)
	env := newTestEnv(rng, cfg, app.Proxy())
	env.alignLocalMempool()

	newTx := func() []byte {
		addr, nonce := app.NewAccount(rng)
		return env.genTx(rng, addr, nonce).encode()
	}
	require.NoError(t, scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		holder := newTx()
		s.Spawn(func() error {
			_, err := env.state.InsertTx(ctx, holder)
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
					_, err := env.state.InsertTx(ctx, tx)
					if !errors.Is(err, context.Canceled) {
						return fmt.Errorf("InsertTx() = %v, want context.Canceled", err)
					}
					return nil
				})
			}
			cancel()
			return nil
		}); err != nil {
			return err
		}
		app.openGate()
		// Once holder finishes, a fresh insert must acquire the permit.
		_, err := env.state.InsertTx(ctx, newTx())
		return err
	}))
	require.Equal(t, 1, app.maxInflight())
	require.Equal(t, 2, len(env.state.UnconfirmedTxs()))
}

// An absent limit resolves to at least one permit so inserts proceed.
func TestConfig_MaxConcurrentCheckTxDefault(t *testing.T) {
	cfg := &Config{}
	require.True(t, cfg.maxConcurrentCheckTx() >= 1)
	require.Equal(t, max(1, runtime.GOMAXPROCS(0)/2), cfg.maxConcurrentCheckTx())
	cfg.MaxConcurrentCheckTx = utils.Some[uint64](7)
	require.Equal(t, 7, cfg.maxConcurrentCheckTx())
}
