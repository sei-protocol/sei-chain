package producer

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/producer/metrics"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/proxy"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/scope"
)

// txPriority is the priority a priorityApp gives a tx before and after CheckTx.
type txPriority struct {
	hint    int64
	checked int64
}

// priorityApp is a testApp that ranks txs by a per-tx priority and counts its CheckTx and
// priority hint calls.
type priorityApp struct {
	*testApp
	checkTxCalls atomic.Int64
	hintCalls    atomic.Int64
	priorities   utils.Mutex[map[common.Hash]txPriority]
}

func newPriorityApp() *priorityApp {
	return &priorityApp{
		testApp:    newTestApp(),
		priorities: utils.NewMutex(map[common.Hash]txPriority{}),
	}
}

func (a *priorityApp) Proxy() *proxy.Proxy {
	return proxy.New(a)
}

func (a *priorityApp) setPriority(tx *txSpec, p txPriority) {
	for priorities := range a.priorities.Lock() {
		priorities[tx.EVMHash] = p
	}
}

func (a *priorityApp) priority(raw []byte) txPriority {
	tx, err := decodeTxSpec(raw)
	if err != nil {
		return txPriority{}
	}
	for priorities := range a.priorities.Lock() {
		return priorities[tx.EVMHash]
	}
	panic("unreachable")
}

func (a *priorityApp) CheckTx(ctx context.Context, req *abci.RequestCheckTxV2) *abci.ResponseCheckTxV2 {
	a.checkTxCalls.Add(1)
	resp := a.testApp.CheckTx(ctx, req)
	resp.Priority = a.priority(req.Tx).checked
	return resp
}

func (a *priorityApp) GetTxPriorityHint(_ context.Context, req *abci.RequestGetTxPriorityHintV2) (*abci.ResponseGetTxPriorityHint, error) {
	a.hintCalls.Add(1)
	return &abci.ResponseGetTxPriorityHint{Priority: a.priority(req.Tx).hint}, nil
}

// fullTxOf returns a tx of addr at nonce that alone fills a block.
func (env *testEnv) fullTxOf(rng utils.Rng, addr common.Address, nonce uint64) *txSpec {
	tx := env.genTx(rng, addr, nonce)
	tx.GasWanted = env.state.cfg.MaxGasWantedPerBlock
	tx.GasEstimated = tx.GasWanted
	return tx
}

// rankedTx returns a block-filling tx of a new account with the given CheckTx and hint priority,
// whose sender's shard is owned by the local validator iff owned.
func (env *testEnv) rankedTx(rng utils.Rng, app *priorityApp, owned bool, priority int64) *txSpec {
	local := env.consensus.Avail().LocalLane().OrPanic("test local lane").Validator
	committee := env.data.NextCommitEpoch().Load().Committee()
	for {
		addr, nonce := app.NewAccount(rng)
		if (committee.EvmShard(addr) == local) == owned {
			tx := env.fullTxOf(rng, addr, nonce)
			app.setPriority(tx, txPriority{hint: priority, checked: priority})
			return tx
		}
	}
}

// enqueueTxs spawns one blocked InsertTx call per tx, one at a time, so the arrival order is known.
func (env *testEnv) enqueueTxs(ctx context.Context, s scope.Scope, mp *mempool, txs []*txSpec, want error) error {
	pending := mp.pendingInserts.Load()
	for _, tx := range txs {
		env.spawnInserter(ctx, s, tx, want)
		pending += 1
		if _, err := mp.pendingInserts.Wait(ctx, func(got uint64) bool { return got == pending }); err != nil {
			return err
		}
	}
	return nil
}

// insertAsync spawns an InsertTx call and returns a watch that holds its error once it returns.
func (env *testEnv) insertAsync(ctx context.Context, s scope.Scope, tx *txSpec) utils.AtomicRecv[utils.Option[error]] {
	res := utils.NewAtomicSend(utils.None[error]())
	s.Spawn(func() error {
		_, err := env.state.InsertTx(ctx, tx.encode())
		res.Store(utils.Some(err))
		return nil
	})
	return res.Subscribe()
}

func waitResult(ctx context.Context, res utils.AtomicRecv[utils.Option[error]]) (error, error) {
	got, err := res.Wait(ctx, func(o utils.Option[error]) bool { return o.IsPresent() })
	if err != nil {
		return nil, err
	}
	insertErr, _ := got.Get()
	return insertErr, nil
}

// expectAdmissionOrder frees one block per tx and checks that the queued txs are admitted in order.
func (env *testEnv) expectAdmissionOrder(ctx context.Context, mp *mempool, order []*txSpec) error {
	pending := mp.pendingInserts.Load()
	for i, tx := range order {
		env.freeOneBlock(mp)
		pending -= 1
		if _, err := waitPending(ctx, mp, pending); err != nil {
			return err
		}
		if want := [][]byte{tx.encode()}; !slices.EqualFunc(env.state.UnconfirmedTxs(), want, slices.Equal) {
			return fmt.Errorf("admitted tx %d out of order", i)
		}
	}
	return nil
}

// Freed capacity goes to the highest-priority blocked call; equal priorities keep arrival order.
func TestInsertTx_AdmitsHighestPriorityFirst(t *testing.T) {
	ctx := t.Context()
	rng := utils.TestRng()
	app := newPriorityApp()
	env := newTestEnv(rng, app.Cfg(), app.Proxy())
	mp, err := env.fillMempool(ctx, rng, app.testApp)
	require.NoError(t, err)

	low := env.rankedTx(rng, app, true, 1)
	firstHigh := env.rankedTx(rng, app, true, 3)
	mid := env.rankedTx(rng, app, true, 2)
	secondHigh := env.rankedTx(rng, app, true, 3)
	require.NoError(t, scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		if err := env.enqueueTxs(ctx, s, mp, utils.Slice(low, firstHigh, mid, secondHigh), nil); err != nil {
			return err
		}
		return env.expectAdmissionOrder(ctx, mp, utils.Slice(firstHigh, secondHigh, mid, low))
	}))
}

// A sender of an owned shard is admitted before a fallback sender at equal and at higher priority.
func TestInsertTx_AdmitsOwnedShardBeforeFallback(t *testing.T) {
	ctx := t.Context()
	rng := utils.TestRng()
	app := newPriorityApp()
	cfg := app.Cfg()
	cfg.RankByShardOwnership = true
	env, _, _ := newTestEnvN(rng, 2, cfg, app.Proxy())
	mp, err := env.fillMempool(ctx, rng, app.testApp)
	require.NoError(t, err)

	fallbackHigh := env.rankedTx(rng, app, false, 5)
	fallbackLow := env.rankedTx(rng, app, false, 1)
	ownedLow := env.rankedTx(rng, app, true, 1)
	ownedZero := env.rankedTx(rng, app, true, 0)
	require.NoError(t, scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		if err := env.enqueueTxs(ctx, s, mp, utils.Slice(fallbackHigh, fallbackLow, ownedLow, ownedZero), nil); err != nil {
			return err
		}
		return env.expectAdmissionOrder(ctx, mp, utils.Slice(ownedLow, ownedZero, fallbackHigh, fallbackLow))
	}))
}

// Without RankByShardOwnership, a fallback sender ranks as an owned one: priority, then arrival.
func TestInsertTx_IgnoresShardOwnershipWhenDisabled(t *testing.T) {
	ctx := t.Context()
	rng := utils.TestRng()
	app := newPriorityApp()
	env, _, _ := newTestEnvN(rng, 2, app.Cfg(), app.Proxy())
	mp, err := env.fillMempool(ctx, rng, app.testApp)
	require.NoError(t, err)

	fallbackHigh := env.rankedTx(rng, app, false, 5)
	fallbackLow := env.rankedTx(rng, app, false, 1)
	ownedLow := env.rankedTx(rng, app, true, 1)
	ownedZero := env.rankedTx(rng, app, true, 0)
	require.NoError(t, scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		if err := env.enqueueTxs(ctx, s, mp, utils.Slice(fallbackHigh, fallbackLow, ownedLow, ownedZero), nil); err != nil {
			return err
		}
		return env.expectAdmissionOrder(ctx, mp, utils.Slice(fallbackHigh, fallbackLow, ownedLow, ownedZero))
	}))
}

// On a full queue, the priority hint waits for a CheckTx permit, so hints are bounded like CheckTx.
func TestInsertTx_HintHoldsCheckTxPermit(t *testing.T) {
	ctx := t.Context()
	rng := utils.TestRng()
	app := newPriorityApp()
	cfg := app.Cfg()
	cfg.MaxPendingInserts = 1
	cfg.MaxConcurrentCheckTx = utils.Some[uint64](1)
	env := newTestEnv(rng, cfg, app.Proxy())
	mp, err := env.fillMempool(ctx, rng, app.testApp)
	require.NoError(t, err)

	require.NoError(t, utils.IgnoreCancel(scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		if err := env.enqueueTxs(ctx, s, mp, utils.Slice(env.rankedTx(rng, app, true, 1)), context.Canceled); err != nil {
			return err
		}
		release, err := env.state.acquireCheckTxPermit(ctx)
		if err != nil {
			return err
		}
		defer release()
		hints := app.hintCalls.Load()
		callCtx, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
		defer cancel()
		if _, err := env.state.InsertTx(callCtx, env.rankedTx(rng, app, true, 9).encode()); !errors.Is(err, context.DeadlineExceeded) {
			return fmt.Errorf("InsertTx with no permit free: got %v, want context.DeadlineExceeded", err)
		}
		if got := app.hintCalls.Load(); got != hints {
			return fmt.Errorf("hint calls with no permit free: got %d, want %d", got, hints)
		}
		s.Cancel(context.Canceled)
		return nil
	})))
}

// On a full queue, a call that outranks the lowest-ranked blocked call evicts it with
// errPendingFull; a call that does not is rejected with errPendingFull before CheckTx.
func TestInsertTx_EvictsLowestRankedOnFullQueue(t *testing.T) {
	ctx := t.Context()
	rng := utils.TestRng()
	app := newPriorityApp()
	cfg := app.Cfg()
	cfg.MaxPendingInserts = 2
	env := newTestEnv(rng, cfg, app.Proxy())
	mp, err := env.fillMempool(ctx, rng, app.testApp)
	require.NoError(t, err)

	low := env.rankedTx(rng, app, true, 1)
	mid := env.rankedTx(rng, app, true, 2)
	high := env.rankedTx(rng, app, true, 3)
	require.NoError(t, scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		lowRes := env.insertAsync(ctx, s, low)
		if _, err := mp.pendingInserts.Wait(ctx, func(got uint64) bool { return got == 1 }); err != nil {
			return err
		}
		if err := env.enqueueTxs(ctx, s, mp, utils.Slice(mid), nil); err != nil {
			return err
		}
		env.spawnInserter(ctx, s, high, nil)
		lowErr, err := waitResult(ctx, lowRes)
		if err != nil {
			return err
		}
		if got := insertResult(nil, lowErr); got != metrics.ResultPendingFull {
			return fmt.Errorf("evicted InsertTx: got %v, want errPendingFull", lowErr)
		}
		if got := mp.pendingInserts.Load(); got != 2 {
			return fmt.Errorf("pending after eviction: got %d, want 2", got)
		}
		// Neither a lower priority nor a tie with the lowest-ranked queued call outranks it.
		for _, priority := range utils.Slice[int64](1, 2) {
			calls := app.checkTxCalls.Load()
			_, err := env.state.InsertTx(ctx, env.rankedTx(rng, app, true, priority).encode())
			if insertResult(nil, err) != metrics.ResultPendingFull {
				return fmt.Errorf("InsertTx at priority %d: got %v, want errPendingFull", priority, err)
			}
			if got := app.checkTxCalls.Load(); got != calls {
				return fmt.Errorf("CheckTx calls at priority %d: got %d, want %d", priority, got, calls)
			}
		}
		return env.expectAdmissionOrder(ctx, mp, utils.Slice(high, mid))
	}))
}

// When the priority hint and the CheckTx priority disagree, the CheckTx priority decides.
func TestInsertTx_CheckTxPriorityOverridesHint(t *testing.T) {
	ctx := t.Context()
	rng := utils.TestRng()
	app := newPriorityApp()
	cfg := app.Cfg()
	cfg.MaxPendingInserts = 1
	env := newTestEnv(rng, cfg, app.Proxy())
	mp, err := env.fillMempool(ctx, rng, app.testApp)
	require.NoError(t, err)

	queued := env.rankedTx(rng, app, true, 1)
	overHinted := env.rankedTx(rng, app, true, 0)
	app.setPriority(overHinted, txPriority{hint: 5, checked: 0})
	require.NoError(t, scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		if err := env.enqueueTxs(ctx, s, mp, utils.Slice(queued), nil); err != nil {
			return err
		}
		calls := app.checkTxCalls.Load()
		_, err := env.state.InsertTx(ctx, overHinted.encode())
		if insertResult(nil, err) != metrics.ResultPendingFull {
			return fmt.Errorf("InsertTx: got %v, want errPendingFull", err)
		}
		if got := app.checkTxCalls.Load(); got != calls+1 {
			return fmt.Errorf("CheckTx calls: got %d, want %d", got, calls+1)
		}
		return env.expectAdmissionOrder(ctx, mp, utils.Slice(queued))
	}))
}

// A sender's later nonce waits behind its earlier one, whatever their priorities and arrival
// order, while other senders compete freely; every call is admitted without errBadNonce.
func TestInsertTx_KeepsSenderNonceOrder(t *testing.T) {
	ctx := t.Context()
	rng := utils.TestRng()
	app := newPriorityApp()
	env := newTestEnv(rng, app.Cfg(), app.Proxy())
	mp, err := env.fillMempool(ctx, rng, app.testApp)
	require.NoError(t, err)

	addr, nonce := app.NewAccount(rng)
	first := env.fullTxOf(rng, addr, nonce)
	app.setPriority(first, txPriority{hint: 1, checked: 1})
	second := env.fullTxOf(rng, addr, nonce+1)
	app.setPriority(second, txPriority{hint: 10, checked: 10})
	third := env.fullTxOf(rng, addr, nonce+2)
	app.setPriority(third, txPriority{hint: 9, checked: 9})
	other := env.rankedTx(rng, app, true, 5)
	require.NoError(t, scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		// first arrives after its successors and takes their place at the front of the sender.
		if err := env.enqueueTxs(ctx, s, mp, utils.Slice(second, other, third, first), nil); err != nil {
			return err
		}
		return env.expectAdmissionOrder(ctx, mp, utils.Slice(other, first, second, third))
	}))
}

// Closing the mempool releases calls queued behind their sender's earlier nonce too.
func TestInsertTx_ClosedReleasesSenderQueue(t *testing.T) {
	ctx := t.Context()
	rng := utils.TestRng()
	app := newPriorityApp()
	env := newTestEnv(rng, app.Cfg(), app.Proxy())
	mp, err := env.fillMempool(ctx, rng, app.testApp)
	require.NoError(t, err)

	addr, nonce := app.NewAccount(rng)
	txs := utils.Slice(env.fullTxOf(rng, addr, nonce), env.fullTxOf(rng, addr, nonce+1), env.fullTxOf(rng, addr, nonce+2))
	require.NoError(t, scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		if err := env.enqueueTxs(ctx, s, mp, txs, ErrNotProducing); err != nil {
			return err
		}
		env.state.clearMempool()
		return nil
	}))
}

// Many inserters racing block frees, evictions and cancellations all finish, every sender's
// txs are sequenced in nonce order, and the admission queue drains. Burst senders submit their
// nonces concurrently, so a later nonce may arrive first and fail errBadNonce.
func TestInsertTx_PriorityAdmissionStress(t *testing.T) {
	ctx := t.Context()
	rng := utils.TestRng()
	app := newPriorityApp()
	cfg := app.Cfg()
	cfg.MaxPendingInserts = 8
	env, _, _ := newTestEnvN(rng, 2, cfg, app.Proxy())
	mp, err := env.fillMempool(ctx, rng, app.testApp)
	require.NoError(t, err)

	const senders, txsPerSender, cancellers, bursts, txsPerBurst = 24, 6, 8, 4, 4
	type account struct {
		addr  common.Address
		start uint64
		txs   []*txSpec
	}
	accounts := make([]account, senders)
	for i := range accounts {
		addr, start := app.NewAccount(rng)
		accounts[i] = account{addr: addr, start: start}
		for k := range uint64(txsPerSender) {
			tx := env.fullTxOf(rng, addr, start+k)
			p := rng.Int63n(10)
			app.setPriority(tx, txPriority{hint: p, checked: p})
			accounts[i].txs = append(accounts[i].txs, tx)
		}
	}
	var burstTxs []*txSpec
	for range bursts {
		addr, start := app.NewAccount(rng)
		for k := range uint64(txsPerBurst) {
			tx := env.fullTxOf(rng, addr, start+k)
			p := rng.Int63n(10)
			app.setPriority(tx, txPriority{hint: p, checked: p})
			burstTxs = append(burstTxs, tx)
		}
	}
	cancelled := make([]*txSpec, cancellers)
	for i := range cancelled {
		cancelled[i] = env.rankedTx(rng, app, i%2 == 0, rng.Int63n(10))
	}

	require.NoError(t, scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		freeCtx, stopFreeing := context.WithCancel(ctx)
		s.Spawn(func() error { return utils.IgnoreCancel(freeWhenFull(freeCtx, env, mp)) })
		defer stopFreeing()
		return scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
			for _, acc := range accounts {
				s.Spawn(func() error {
					for _, tx := range acc.txs {
						if err := insertRetryingPendingFull(ctx, env, mp, tx); err != nil {
							return err
						}
					}
					return nil
				})
			}
			for _, tx := range burstTxs {
				s.Spawn(func() error {
					_, err := env.state.InsertTx(ctx, tx.encode())
					if err != nil && !errors.Is(err, errPendingFull) && !errors.Is(err, errBadNonce) {
						return fmt.Errorf("burst InsertTx: %w", err)
					}
					return nil
				})
			}
			for _, tx := range cancelled {
				s.Spawn(func() error {
					insertCtx, cancel := context.WithCancel(ctx)
					res := env.insertAsync(insertCtx, s, tx)
					cancel()
					insertErr, err := waitResult(ctx, res)
					if err != nil {
						return err
					}
					if insertErr != nil && !errors.Is(insertErr, context.Canceled) && !errors.Is(insertErr, errPendingFull) {
						return fmt.Errorf("cancelled InsertTx: %w", insertErr)
					}
					return nil
				})
			}
			return nil
		})
	}))

	for _, acc := range accounts {
		require.Equal(t, acc.start+txsPerSender, env.state.EvmNextPendingNonce(acc.addr))
	}
	require.Equal(t, uint64(0), mp.pendingInserts.Load())
	for m := range mp.inner.Lock() {
		require.Equal(t, uint64(0), m.waiters.Len())
		require.Equal(t, 0, m.waiters.best.Len())
		require.Equal(t, 0, m.waiters.worst.Len())
		require.Equal(t, 0, len(m.waiters.senders))
	}
}

// freeWhenFull executes and prunes the oldest lane block each time the mempool fills up, until
// ctx is done. Executing first keeps the app nonces of senders with later queued txs current.
func freeWhenFull(ctx context.Context, env *testEnv, mp *mempool) error {
	for {
		var first types.BlockNumber
		var txs [][]byte
		for m, ctrl := range mp.inner.Lock() {
			if err := ctrl.WaitUntil(ctx, func() bool { return m.IsFull() }); err != nil {
				return err
			}
			first = m.first
			txs = m.blocks[first].txs
		}
		if _, err := env.app.FinalizeBlock(ctx, &abci.RequestFinalizeBlock{Txs: txs}); err != nil {
			return err
		}
		env.state.pruneMempool(mp, first+1)
	}
}

// insertRetryingPendingFull inserts tx, retrying once the queue has room after errPendingFull.
func insertRetryingPendingFull(ctx context.Context, env *testEnv, mp *mempool, tx *txSpec) error {
	limit := env.state.cfg.maxPendingInserts()
	for {
		_, err := env.state.InsertTx(ctx, tx.encode())
		if !errors.Is(err, errPendingFull) {
			return err
		}
		if _, err := mp.pendingInserts.Wait(ctx, func(got uint64) bool { return got < limit }); err != nil {
			return err
		}
	}
}

// admissionQueue keeps Best and Worst equal to the best and worst eligible calls of a model
// under random pushes and removals, and VictimFor names the last call of the worst one's sender.
func TestAdmissionQueue_MatchesModel(t *testing.T) {
	rng := utils.TestRng()
	q := newAdmissionQueue()
	var queued []*insertTicket
	addrs := utils.Slice(common.Address{1}, common.Address{2}, common.Address{3})
	eligible := func() []*insertTicket {
		var out []*insertTicket
		for _, t := range queued {
			addr, ok := t.sender.Get()
			if !ok {
				out = append(out, t)
				continue
			}
			first := true
			for _, o := range queued {
				if o != t && o.sender == utils.Some(addr) && nonceOrder(o, t) < 0 {
					first = false
				}
			}
			if first {
				out = append(out, t)
			}
		}
		return out
	}
	newTicket := func() *insertTicket {
		r := rank{owned: rng.Intn(2) == 0, priority: rng.Int63n(4), seq: unqueuedSeq}
		if rng.Intn(4) == 0 {
			return newInsertTicket(r, utils.None[common.Address](), 0)
		}
		return newInsertTicket(r, utils.Some(addrs[rng.Intn(len(addrs))]), uint64(rng.Intn(4))) //nolint:gosec // small test value
	}
	for range 1000 {
		if len(queued) == 0 || rng.Intn(2) == 0 {
			t := newTicket()
			q.Push(t)
			queued = append(queued, t)
		} else {
			i := rng.Intn(len(queued))
			require.True(t, q.Remove(queued[i]))
			require.False(t, q.Remove(queued[i]))
			queued = slices.Delete(queued, i, i+1)
		}
		require.Equal(t, uint64(len(queued)), q.Len())
		want := eligible()
		slices.SortFunc(want, func(a, b *insertTicket) int {
			if a.rank.outranks(b.rank) {
				return -1
			}
			return 1
		})
		require.Equal(t, len(want), q.best.Len())
		require.Equal(t, len(queued), len(slices.Collect(q.All())))
		if len(want) == 0 {
			continue
		}
		require.Equal(t, want[0], q.Best().OrPanic("non-empty"))
		worst := want[len(want)-1]
		require.Equal(t, worst, q.Worst().OrPanic("non-empty"))
		victim := q.VictimFor(newInsertTicket(rank{owned: true, priority: 100, seq: unqueuedSeq}, utils.None[common.Address](), 0)).OrPanic("outranks")
		if addr, ok := worst.sender.Get(); ok {
			calls := q.senders[addr]
			require.Equal(t, calls[len(calls)-1], victim)
		} else {
			require.Equal(t, worst, victim)
		}
	}
}
