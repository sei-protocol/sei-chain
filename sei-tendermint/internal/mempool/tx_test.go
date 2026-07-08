package mempool

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/holiman/uint256"
	"github.com/sei-protocol/sei-chain/sei-tendermint/abci/example/kvstore"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/proxy"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
	"github.com/sei-protocol/sei-chain/sei-tendermint/types"
)

func newTxStoreForTest() *txStore {
	return NewTxStore(TestConfig(), proxy.New(kvstore.NewApplication(), proxy.NewMetrics()), NewMetrics())
}

func txStoreCacheRemove(txStore *txStore, txHash types.TxHash) {
	for inner := range txStore.inner.Lock() {
		inner.cache.Remove(txHash)
	}
}

func txStoreStateForTest(ready, pending []*WrappedTx) txStoreState {
	state := txStoreState{}
	for _, wtx := range ready {
		state.ready.Inc(wtx.Size())
		state.total.Inc(wtx.Size())
	}
	for _, wtx := range pending {
		state.total.Inc(wtx.Size())
	}
	return state
}

type testAccount struct {
	address   common.Address
	baseNonce uint64
	lastNonce uint64
}

type testEnv struct {
	rng       utils.Rng
	txStore   *txStore
	app       *evmNonceApp
	accounts  []testAccount
	byHash    map[types.TxHash]*WrappedTx
	everReady map[types.TxHash]struct{}
}

func newTestEnv(
	rng utils.Rng,
	txStore *txStore,
	app *evmNonceApp,
	numAccounts int,
) *testEnv {
	env := &testEnv{
		rng:       rng,
		txStore:   txStore,
		app:       app,
		accounts:  make([]testAccount, numAccounts),
		byHash:    map[types.TxHash]*WrappedTx{},
		everReady: map[types.TxHash]struct{}{},
	}
	for i := range env.accounts {
		env.accounts[i].address = common.BytesToAddress(utils.GenBytes(rng, common.AddressLength))
		env.accounts[i].baseNonce = uint64(rng.Intn(20) + 1)
		rangeLen := rng.Intn(16) + 12
		env.accounts[i].lastNonce = env.accounts[i].baseNonce + uint64(rangeLen-1)
		env.app.setNonce(env.accounts[i].address, env.accounts[i].baseNonce)
	}
	return env
}

func (e *testEnv) insertTxs(
	t *testing.T,
	insertProbPercent int,
	makeTx func(account *testAccount, nonce uint64) *WrappedTx,
) {
	t.Helper()

	clear(e.byHash)
	for i := range e.accounts {
		account := &e.accounts[i]
		rangeLen := int(account.lastNonce-account.baseNonce) + 1
		for offset := range rangeLen {
			if e.rng.Intn(100) >= insertProbPercent {
				continue
			}
			wtx := makeTx(account, account.baseNonce+uint64(offset))
			e.byHash[wtx.Hash()] = wtx
			require.NoError(t, e.txStore.Insert(wtx))
		}
	}
}

func (e *testEnv) txs() []*WrappedTx {
	txs := make([]*WrappedTx, 0, len(e.byHash))
	for _, wtx := range e.byHash {
		txs = append(txs, wtx)
	}
	return txs
}

func (e *testEnv) byNonce(account testAccount) map[uint64]*WrappedTx {
	byNonce := map[uint64]*WrappedTx{}
	for _, wtx := range e.byHash {
		evm := wtx.evm.OrPanic("evm tx")
		if evm.address == account.address {
			byNonce[evm.nonce] = wtx
		}
	}
	return byNonce
}

func (e *testEnv) readyTxs() []*WrappedTx {
	var ready []*WrappedTx
	for _, account := range e.accounts {
		byNonce := e.byNonce(account)
		currentNonce := e.app.EvmNonce(account.address)
		balance := e.app.balanceOf(account.address)
		for nonce := currentNonce; ; nonce++ {
			wtx, ok := byNonce[nonce]
			if !ok {
				break
			}
			requiredBalance := wtx.evm.OrPanic("").requiredBalance
			if requiredBalance.CmpUint64(uint64(balance)) > 0 {
				break
			}
			ready = append(ready, wtx)
		}
	}
	return ready
}

func (e *testEnv) markReadyTxs() {
	for _, wtx := range e.readyTxs() {
		e.everReady[wtx.Hash()] = struct{}{}
	}
}

func (e *testEnv) stableReady() []*WrappedTx {
	var stable []*WrappedTx
	for _, wtx := range e.byHash {
		if _, ok := e.everReady[wtx.Hash()]; ok {
			stable = append(stable, wtx)
		}
	}
	return stable
}

func toTxs(wtxs []*WrappedTx) types.Txs {
	var txs types.Txs
	for _, wtx := range wtxs {
		txs = append(txs, wtx.Tx())
	}
	return txs
}

func genEvmHash(rng utils.Rng) common.Hash {
	return common.BytesToHash(utils.GenBytes(rng, common.HashLength))
}

func genEvmAddress(rng utils.Rng) common.Address {
	return common.BytesToAddress(utils.GenBytes(rng, common.AddressLength))
}

func makeEvmTxForTest(
	rng utils.Rng,
	address common.Address,
	nonce uint64,
	priority int64,
	requiredBalance int,
) *WrappedTx {
	return &WrappedTx{
		hashedTx:     newHashedTx(utils.GenBytes(rng, rng.Intn(48)+16)),
		timestamp:    time.Now(),
		priority:     priority,
		gasWanted:    1,
		estimatedGas: 1,
		evm: utils.Some(evmTx{
			address:         address,
			seiAddress:      address.Bytes(),
			hash:            genEvmHash(rng),
			nonce:           nonce,
			requiredBalance: *uint256.NewInt(uint64(requiredBalance)),
		}),
	}
}

func (e *testEnv) assertState(t *testing.T) {
	t.Helper()

	expectedReady := e.readyTxs()
	readySet := make(map[types.TxHash]struct{}, len(expectedReady))
	for _, wtx := range expectedReady {
		readySet[wtx.Hash()] = struct{}{}
	}
	var expectedPending []*WrappedTx
	for _, wtx := range e.txs() {
		if _, ok := readySet[wtx.Hash()]; ok {
			continue
		}
		expectedPending = append(expectedPending, wtx)
	}
	expectedStableReady := e.stableReady()

	require.Equal(t, txStoreStateForTest(expectedReady, expectedPending), e.txStore.State())

	readyTxs := e.txStore.ReadyTxs()
	require.ElementsMatch(t, toTxs(expectedReady), toTxs(readyTxs))

	reaped, _ := e.txStore.Reap(ReapLimits{MaxTxs: utils.Some(uint64(len(e.byHash)))}, false)
	require.ElementsMatch(t, toTxs(expectedReady), reaped)

	var listedTxs types.Txs
	for el := e.txStore.readyTxs.Front(); el != nil; el = el.Next() {
		listedTxs = append(listedTxs, el.Value())
	}
	require.ElementsMatch(t, toTxs(expectedStableReady), listedTxs)
}

func TestTxStore_GetTxByHash(t *testing.T) {
	txs := newTxStoreForTest()
	wtx := &WrappedTx{
		hashedTx:  newHashedTx(types.Tx("test_tx")),
		priority:  1,
		timestamp: time.Now(),
	}

	key := wtx.Hash()
	res, ok := txs.ByHash(key)
	require.False(t, ok)
	require.Nil(t, res)

	require.NoError(t, txs.Insert(wtx))

	res, ok = txs.ByHash(key)
	require.True(t, ok)
	require.Equal(t, wtx.Tx(), res)
}

func TestTxStore_GetTxByEvmHash(t *testing.T) {
	rng := utils.TestRng()
	txs := newTxStoreForTest()
	wtx := makeEvmTxForTest(rng, genEvmAddress(rng), 7, 1, 0)
	hash := wtx.evm.OrPanic("evm tx").hash

	res, ok := txs.ByEvmHash(hash)
	require.False(t, ok)

	require.NoError(t, txs.Insert(wtx))
	res, ok = txs.ByEvmHash(hash)
	require.True(t, ok)
	require.Equal(t, wtx.Tx(), res)
}

func TestTxStore_SetTx(t *testing.T) {
	txs := newTxStoreForTest()
	wtx := &WrappedTx{
		hashedTx:  newHashedTx(types.Tx("test_tx")),
		priority:  1,
		timestamp: time.Now(),
	}

	key := wtx.Hash()
	require.NoError(t, txs.Insert(wtx))

	res, ok := txs.ByHash(key)
	require.True(t, ok)
	require.Equal(t, wtx.Tx(), res)
}

func TestTxStore_Size(t *testing.T) {
	txStore := newTxStoreForTest()
	numTxs := 1000

	for i := range numTxs {
		require.NoError(t, txStore.Insert(&WrappedTx{
			hashedTx:  newHashedTx(fmt.Appendf(nil, "test_tx_%d", i)),
			priority:  int64(i),
			timestamp: time.Now(),
		}))
	}

	require.Equal(t, numTxs, txStore.State().total.count)
}

func TestTxStore_RejectsAndEvictsTransactionsBelowAccountNonce(t *testing.T) {
	rng := utils.TestRng()
	app := newEVMNonceApp()
	txStore := NewTxStore(TestConfig(), proxy.New(app, proxy.NewMetrics()), NewMetrics())

	makeTx := func(address common.Address, nonce uint64) *WrappedTx {
		requiredBalance := *uint256.NewInt(uint64(rng.Int63n(256)))
		return &WrappedTx{
			hashedTx:     newHashedTx(utils.GenBytes(rng, 32)),
			timestamp:    time.Now(),
			priority:     rng.Int63n(1_000_000) + 1,
			gasWanted:    1,
			estimatedGas: 1,
			evm: utils.Some(evmTx{
				address:         address,
				seiAddress:      address.Bytes(),
				hash:            genEvmHash(rng),
				nonce:           nonce,
				requiredBalance: requiredBalance,
			}),
		}
	}

	// Seed the store with sparse per-account nonce ranges so each account has a
	// mix of contiguous ready transactions and gaps that keep later transactions
	// pending.
	env := newTestEnv(rng, txStore, app, 8)
	for _, account := range env.accounts {
		app.setBalance(account.address, rng.Intn(256))
	}
	env.insertTxs(t, 80, func(account *testAccount, nonce uint64) *WrappedTx {
		return makeTx(account.address, nonce)
	})
	for _, account := range env.accounts {
		rejected := makeTx(account.address, account.baseNonce-1)
		require.ErrorIs(t, txStore.Insert(rejected), errOldNonce)
	}

	require.Equal(t, len(env.byHash), txStore.State().total.count)

	// Seed the stable-ready history with transactions that are already ready
	// after the initial inserts.
	env.markReadyTxs()

	// Advance the per-account nonce frontier in several randomized rounds and
	// verify that Update removes every transaction that fell below the account
	// nonce while preserving the rest.
	for height := range int64(5) {
		for _, account := range env.accounts {
			currentNonce := app.EvmNonce(account.address)
			if currentNonce > 0 {
				rejected := makeTx(account.address, currentNonce-1)
				require.ErrorIs(t, txStore.Insert(rejected), errOldNonce)
			}
			maxAdvance := max(0, int(account.lastNonce-currentNonce)+4)
			for range rng.Intn(maxAdvance + 1) {
				app.markMined(account.address)
			}
			app.setBalance(account.address, rng.Intn(256))
		}

		txStore.Update(updateSpec{
			Now:           time.Now(),
			Height:        height + 1,
			TxResults:     map[types.TxHash]bool{},
			Constraints:   NopTxConstraints(),
			NewPriorities: map[types.TxHash]int64{},
		})

		for txHash, wtx := range env.byHash {
			if wtx.EVMNonce() < app.EvmNonce(wtx.evm.OrPanic("").address) {
				delete(env.byHash, txHash)
			}
		}
		env.markReadyTxs()
		env.assertState(t)
	}
}

func testTxStoreUpdateExpiresTransactions(t *testing.T, removeExpiredTxsFromQueue bool) {
	rng := utils.TestRng()
	cfg := TestConfig()
	cfg.CacheSize = 1_000
	cfg.TTLNumBlocks = utils.Some(int64(10))
	cfg.TTLDuration = utils.Some(10 * time.Second)
	cfg.RemoveExpiredTxsFromQueue = removeExpiredTxsFromQueue

	app := newEVMNonceApp()
	txStore := NewTxStore(cfg, proxy.New(app, proxy.NewMetrics()), NewMetrics())
	baseTime := time.Unix(1_700_000_000, 0)

	makeTx := func(address common.Address, nonce uint64, height int64, timestamp time.Time) *WrappedTx {
		return &WrappedTx{
			hashedTx:     newHashedTx(utils.GenBytes(rng, 32)),
			height:       height,
			timestamp:    timestamp,
			priority:     rng.Int63n(1_000_000) + 1,
			gasWanted:    1,
			estimatedGas: 1,
			evm: utils.Some(evmTx{
				address:         address,
				seiAddress:      address.Bytes(),
				hash:            genEvmHash(rng),
				nonce:           nonce,
				requiredBalance: uint256.Int{},
			}),
		}
	}

	// Seed the store with randomized timestamps, heights, and sparse nonce
	// ranges across a bounded set of accounts.
	env := newTestEnv(rng, txStore, app, 5)
	for _, account := range env.accounts {
		app.setBalance(account.address, 1_000_000)
	}
	env.insertTxs(t, 100, func(account *testAccount, nonce uint64) *WrappedTx {
		return makeTx(
			account.address,
			nonce,
			int64(rng.Intn(28)+1),
			baseTime.Add(time.Duration(rng.Intn(31))*time.Second),
		)
	})

	// Record the transactions that are initially ready; the stable ready list
	// keeps these entries until the transactions are removed.
	env.markReadyTxs()

	updates := []updateSpec{
		{Now: baseTime.Add(16 * time.Second), Height: 14, TxResults: map[types.TxHash]bool{}, Constraints: NopTxConstraints(), NewPriorities: map[types.TxHash]int64{}},
		{Now: baseTime.Add(24 * time.Second), Height: 22, TxResults: map[types.TxHash]bool{}, Constraints: NopTxConstraints(), NewPriorities: map[types.TxHash]int64{}},
		{Now: baseTime.Add(36 * time.Second), Height: 34, TxResults: map[types.TxHash]bool{}, Constraints: NopTxConstraints(), NewPriorities: map[types.TxHash]int64{}},
	}

	for _, update := range updates {
		readyBeforeUpdate := env.readyTxs()
		readyBeforeUpdateSet := make(map[types.TxHash]struct{}, len(readyBeforeUpdate))
		for _, wtx := range readyBeforeUpdate {
			readyBeforeUpdateSet[wtx.Hash()] = struct{}{}
		}

		txStore.Update(update)
		minHeight := int64(-1)
		if ttl, ok := cfg.TTLNumBlocks.Get(); ok && update.Height > ttl {
			minHeight = update.Height - ttl
		}
		minTime := time.Time{}
		if ttl, ok := cfg.TTLDuration.Get(); ok {
			minTime = update.Now.Add(-ttl)
		}

		for txHash, wtx := range env.byHash {
			expiredByHeight := minHeight >= 0 && wtx.height < minHeight
			expiredByTime := !minTime.IsZero() && wtx.timestamp.Before(minTime)
			if !(expiredByHeight || expiredByTime) {
				continue
			}
			if !cfg.RemoveExpiredTxsFromQueue {
				if _, ok := readyBeforeUpdateSet[txHash]; ok {
					continue
				}
			}
			delete(env.byHash, txHash)
		}
		env.markReadyTxs()
		env.assertState(t)
	}
}

func TestTxStore_UpdateExpiresTransactions(t *testing.T) {
	testTxStoreUpdateExpiresTransactions(t, true)
}

func TestTxStore_UpdateExpiresTransactionsKeepsReadyWhenConfigured(t *testing.T) {
	testTxStoreUpdateExpiresTransactions(t, false)
}

func TestTxStore_ExpiredTxCacheBehavior(t *testing.T) {
	rng := utils.TestRng()

	for _, tc := range []struct {
		name                   string
		keepInvalidTxsInCache  bool
		removeExpiredFromQueue bool
		wantReadyPresent       bool
		wantPendingPresent     bool
		wantReadyRejected      bool
		wantPendingRejected    bool
	}{
		{
			name:                   "remove expired and drop from cache",
			keepInvalidTxsInCache:  false,
			removeExpiredFromQueue: true,
			wantReadyPresent:       false,
			wantPendingPresent:     false,
			wantReadyRejected:      false,
			wantPendingRejected:    false,
		},
		{
			name:                   "remove expired and keep in cache",
			keepInvalidTxsInCache:  true,
			removeExpiredFromQueue: true,
			wantReadyPresent:       false,
			wantPendingPresent:     false,
			wantReadyRejected:      true,
			wantPendingRejected:    true,
		},
		{
			name:                   "keep expired ready and drop expired pending from cache",
			keepInvalidTxsInCache:  false,
			removeExpiredFromQueue: false,
			wantReadyPresent:       true,
			wantPendingPresent:     false,
			wantReadyRejected:      true,
			wantPendingRejected:    false,
		},
		{
			name:                   "keep expired ready and keep expired pending in cache",
			keepInvalidTxsInCache:  true,
			removeExpiredFromQueue: false,
			wantReadyPresent:       true,
			wantPendingPresent:     false,
			wantReadyRejected:      true,
			wantPendingRejected:    true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := TestConfig()
			cfg.CacheSize = 10
			cfg.TTLDuration = utils.Some(time.Second)
			cfg.TTLNumBlocks = utils.None[int64]()
			cfg.KeepInvalidTxsInCache = tc.keepInvalidTxsInCache
			cfg.RemoveExpiredTxsFromQueue = tc.removeExpiredFromQueue

			app := newEVMNonceApp()
			txStore := NewTxStore(cfg, proxy.New(app, proxy.NewMetrics()), NewMetrics())
			env := newTestEnv(rng, txStore, app, 1)
			address := env.accounts[0].address
			env.app.setNonce(address, 7)
			env.app.setBalance(address, 100)

			ready := &WrappedTx{
				hashedTx:     newHashedTx(utils.GenBytes(rng, 32)),
				timestamp:    time.Unix(100, 0),
				priority:     10,
				gasWanted:    1,
				estimatedGas: 1,
				evm: utils.Some(evmTx{
					address:         address,
					seiAddress:      address.Bytes(),
					hash:            genEvmHash(rng),
					nonce:           7,
					requiredBalance: uint256.Int{},
				}),
			}
			pending := &WrappedTx{
				hashedTx:     newHashedTx(utils.GenBytes(rng, 32)),
				timestamp:    time.Unix(100, 0),
				priority:     20,
				gasWanted:    1,
				estimatedGas: 1,
				evm: utils.Some(evmTx{
					address:         address,
					seiAddress:      address.Bytes(),
					hash:            genEvmHash(rng),
					nonce:           8,
					requiredBalance: *uint256.NewInt(200),
				}),
			}

			require.NoError(t, txStore.Insert(ready))
			require.NoError(t, txStore.Insert(pending))

			txStore.Update(updateSpec{
				Now:           time.Unix(102, 0),
				Height:        1,
				TxResults:     map[types.TxHash]bool{},
				Constraints:   NopTxConstraints(),
				NewPriorities: map[types.TxHash]int64{},
			})

			_, readyPresent := txStore.ByHash(ready.Hash())
			_, pendingPresent := txStore.ByHash(pending.Hash())
			require.Equal(t, tc.wantReadyPresent, readyPresent)
			require.Equal(t, tc.wantPendingPresent, pendingPresent)
			require.Equal(t, tc.wantReadyRejected, txStore.ShouldReject(ready.Hash()))
			require.Equal(t, tc.wantPendingRejected, txStore.ShouldReject(pending.Hash()))
		})
	}
}

func TestTxStore_NoncePrunedTxsRejectedAsOldNonce(t *testing.T) {
	rng := utils.TestRng()
	cfg := TestConfig()
	cfg.CacheSize = 10

	app := newEVMNonceApp()
	txStore := NewTxStore(cfg, proxy.New(app, proxy.NewMetrics()), NewMetrics())
	env := newTestEnv(rng, txStore, app, 1)
	address := env.accounts[0].address
	env.app.setNonce(address, 7)
	env.app.setBalance(address, 100)

	prunedReady := makeEvmTxForTest(rng, address, 7, 10, 0)
	prunedPending := makeEvmTxForTest(rng, address, 8, 20, 200)
	require.NoError(t, txStore.Insert(prunedReady))
	require.NoError(t, txStore.Insert(prunedPending))

	env.app.setNonce(address, 9)
	txStore.Update(updateSpec{
		Now:           time.Now(),
		Height:        1,
		TxResults:     map[types.TxHash]bool{},
		Constraints:   NopTxConstraints(),
		NewPriorities: map[types.TxHash]int64{},
	})

	_, readyPresent := txStore.ByHash(prunedReady.Hash())
	_, pendingPresent := txStore.ByHash(prunedPending.Hash())
	require.False(t, readyPresent)
	require.False(t, pendingPresent)
	require.True(t, txStore.ShouldReject(prunedReady.Hash()))
	require.True(t, txStore.ShouldReject(prunedPending.Hash()))
}

func TestTxStore_ReplacesReadyTxByHigherPriority(t *testing.T) {
	rng := utils.TestRng()
	app := newEVMNonceApp()
	txStore := NewTxStore(TestConfig(), proxy.New(app, proxy.NewMetrics()), NewMetrics())
	env := newTestEnv(rng, txStore, app, 1)
	address := env.accounts[0].address
	env.app.setNonce(address, 7)
	env.app.setBalance(address, 100)

	// Insert one ready transaction, then replace it with a higher-priority ready
	// transaction for the same nonce.
	old := makeEvmTxForTest(rng, address, 7, 10, 20)
	require.NoError(t, env.txStore.Insert(old))
	env.byHash = map[types.TxHash]*WrappedTx{old.Hash(): old}
	env.markReadyTxs()
	env.assertState(t)

	replacement := makeEvmTxForTest(rng, address, 7, 20, 30)
	require.NoError(t, env.txStore.Insert(replacement))
	delete(env.byHash, old.Hash())
	env.byHash[replacement.Hash()] = replacement
	env.markReadyTxs()
	env.assertState(t)
	_, ok := env.txStore.ByHash(old.Hash())
	require.False(t, ok)
	_, ok = env.txStore.ByEvmHash(old.evm.OrPanic("evm tx").hash)
	require.False(t, ok)
	got, ok := env.txStore.ByHash(replacement.Hash())
	require.True(t, ok)
	require.Equal(t, replacement.Tx(), got)
	got, ok = env.txStore.ByEvmHash(replacement.evm.OrPanic("evm tx").hash)
	require.True(t, ok)
	require.Equal(t, replacement.Tx(), got)

	// A higher-priority transaction that would no longer be ready must not
	// replace the current ready transaction for the same nonce.
	blocked := makeEvmTxForTest(rng, address, 7, 30, 101)
	require.ErrorIs(t, env.txStore.Insert(blocked), errSameNonce)

	env.assertState(t)
	got, ok = env.txStore.ByHash(replacement.Hash())
	require.True(t, ok)
	require.Equal(t, replacement.Tx(), got)
	_, ok = env.txStore.ByHash(blocked.Hash())
	require.False(t, ok)
}

func TestTxStore_RejectsDuplicateEvmHash(t *testing.T) {
	rng := utils.TestRng()
	app := newEVMNonceApp()
	txStore := NewTxStore(TestConfig(), proxy.New(app, proxy.NewMetrics()), NewMetrics())
	address := genEvmAddress(rng)
	app.setNonce(address, 7)
	app.setBalance(address, 100)

	first := makeEvmTxForTest(rng, address, 7, 10, 0)
	second := makeEvmTxForTest(rng, address, 8, 20, 0)
	second.evm = utils.Some(func() evmTx {
		evm := second.evm.OrPanic("")
		evm.hash = first.evm.OrPanic("").hash
		return evm
	}())

	require.NoError(t, txStore.Insert(first))
	require.ErrorIs(t, txStore.Insert(second), errDuplicateTx)

	got, ok := txStore.ByEvmHash(first.evm.OrPanic("evm tx").hash)
	require.True(t, ok)
	require.Equal(t, first.Tx(), got)
	_, ok = txStore.ByHash(second.Hash())
	require.False(t, ok)
}

func TestTxStore_ReplacesReadyThenPendingTxByHigherPriority(t *testing.T) {
	rng := utils.TestRng()
	app := newEVMNonceApp()
	txStore := NewTxStore(TestConfig(), proxy.New(app, proxy.NewMetrics()), NewMetrics())
	env := newTestEnv(rng, txStore, app, 1)
	address := env.accounts[0].address
	env.app.setNonce(address, 7)
	env.app.setBalance(address, 100)

	becamePending := makeEvmTxForTest(rng, address, 7, 40, 60)
	require.NoError(t, env.txStore.Insert(becamePending))
	env.byHash = map[types.TxHash]*WrappedTx{becamePending.Hash(): becamePending}
	env.markReadyTxs()
	env.assertState(t)

	env.app.setBalance(address, 50)
	env.txStore.Update(updateSpec{
		Now:           time.Now(),
		Height:        1,
		TxResults:     map[types.TxHash]bool{},
		Constraints:   NopTxConstraints(),
		NewPriorities: map[types.TxHash]int64{},
	})
	env.assertState(t)

	becamePendingReplacement := makeEvmTxForTest(rng, address, 7, 50, 70)
	require.NoError(t, env.txStore.Insert(becamePendingReplacement))
	delete(env.byHash, becamePending.Hash())
	env.byHash[becamePendingReplacement.Hash()] = becamePendingReplacement
	env.assertState(t)
	_, ok := env.txStore.ByHash(becamePending.Hash())
	require.False(t, ok)
	got, ok := env.txStore.ByHash(becamePendingReplacement.Hash())
	require.True(t, ok)
	require.Equal(t, becamePendingReplacement.Tx(), got)
}

func TestTxStore_ReplacesPendingTxByHigherPriority(t *testing.T) {
	rng := utils.TestRng()
	app := newEVMNonceApp()
	txStore := NewTxStore(TestConfig(), proxy.New(app, proxy.NewMetrics()), NewMetrics())
	env := newTestEnv(rng, txStore, app, 1)
	address := env.accounts[0].address
	env.app.setNonce(address, 7)
	env.app.setBalance(address, 0)

	pending := makeEvmTxForTest(rng, address, 7, 70, 40)
	require.NoError(t, env.txStore.Insert(pending))
	env.byHash = map[types.TxHash]*WrappedTx{pending.Hash(): pending}
	env.assertState(t)

	pendingReplacement := makeEvmTxForTest(rng, address, 7, 90, 50)
	require.NoError(t, env.txStore.Insert(pendingReplacement))
	delete(env.byHash, pending.Hash())
	env.byHash[pendingReplacement.Hash()] = pendingReplacement
	env.assertState(t)
	_, ok := env.txStore.ByHash(pending.Hash())
	require.False(t, ok)
	got, ok := env.txStore.ByHash(pendingReplacement.Hash())
	require.True(t, ok)
	require.Equal(t, pendingReplacement.Tx(), got)
}

func TestTxStore_InsertCompactionKeepsReadyListInSync(t *testing.T) {
	rng := utils.TestRng()
	cfg := TestConfig()
	cfg.Size = 50
	cfg.PendingSize = 0

	app := newEVMNonceApp()
	txStore := NewTxStore(cfg, proxy.New(app, proxy.NewMetrics()), NewMetrics())
	inserted := map[types.TxHash]*WrappedTx{}

	for range 20 * cfg.Size {
		address := common.BytesToAddress(utils.GenBytes(rng, common.AddressLength))
		wtx := makeEvmTxForTest(rng, address, 0, rng.Int63(), 0)
		inserted[wtx.Hash()] = wtx

		err := txStore.Insert(wtx)
		require.True(t, err == nil || errors.Is(err, errMempoolFull), "unexpected insert error: %v", err)

		expected := make([]*WrappedTx, 0, txStore.State().total.count)
		for txHash, candidate := range inserted {
			if tx, ok := txStore.ByHash(txHash); ok {
				require.Equal(t, candidate.Tx(), tx)
				expected = append(expected, candidate)
			}
		}

		ready := txStore.ReadyTxs()
		require.Equal(t, txStore.State().total.count, txStore.State().ready.count)
		require.ElementsMatch(t, toTxs(expected), toTxs(ready))

		var listed types.Txs
		for el := txStore.readyTxs.Front(); el != nil; el = el.Next() {
			listed = append(listed, el.Value())
		}
		require.ElementsMatch(t, toTxs(expected), listed)
	}
}
