package mempool

import (
	"fmt"
	"math/big"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/sei-protocol/sei-chain/sei-tendermint/abci/example/kvstore"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/proxy"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
	"github.com/sei-protocol/sei-chain/sei-tendermint/types"
)

func newTxStoreForTest() *txStore {
	return NewTxStore(TestConfig(), proxy.New(kvstore.NewApplication(), proxy.NopMetrics()), NopMetrics())
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
	txStore := NewTxStore(TestConfig(), proxy.New(app, proxy.NopMetrics()), NopMetrics())

	type accountCase struct {
		address   common.Address
		baseNonce uint64
		lastNonce uint64
		byNonce   map[uint64]*WrappedTx
		txs       []*WrappedTx
	}

	makeTx := func(address common.Address, nonce uint64) *WrappedTx {
		requiredBalance := big.NewInt(rng.Int63n(256))
		return &WrappedTx{
			hashedTx:     newHashedTx(utils.GenBytes(rng, 32)),
			timestamp:    time.Now(),
			priority:     rng.Int63n(1_000_000) + 1,
			gasWanted:    1,
			estimatedGas: 1,
			evm: utils.Some(evmTx{
				address:         address,
				seiAddress:      address.Bytes(),
				nonce:           nonce,
				requiredBalance: requiredBalance,
			}),
		}
	}

	// Seed the store with sparse per-account nonce ranges so each account has a
	// mix of contiguous ready transactions and gaps that keep later transactions
	// pending.
	accounts := make([]accountCase, 8)
	everReady := map[types.TxHash]struct{}{}
	expectedInserted := 0
	for i := range accounts {
		accounts[i].address = common.BytesToAddress(utils.GenBytes(rng, common.AddressLength))
		accounts[i].baseNonce = uint64(rng.Intn(20) + 1)
		accounts[i].byNonce = map[uint64]*WrappedTx{}
		rangeLen := rng.Intn(16) + 12
		accounts[i].lastNonce = accounts[i].baseNonce + uint64(rangeLen-1)
		app.nextNonce[accounts[i].address] = accounts[i].baseNonce
		app.setBalance(accounts[i].address, big.NewInt(rng.Int63n(256)))
		insertedForAccount := 0
		for offset := range rangeLen {
			if rng.Intn(100) >= 80 {
				continue
			}
			wtx := makeTx(accounts[i].address, accounts[i].baseNonce+uint64(offset))
			accounts[i].txs = append(accounts[i].txs, wtx)
			accounts[i].byNonce[wtx.EVMNonce()] = wtx
			require.NoError(t, txStore.Insert(wtx))
			expectedInserted++
			insertedForAccount++
		}
		require.Positive(t, insertedForAccount)

		rejected := makeTx(accounts[i].address, accounts[i].baseNonce-1)
		require.ErrorIs(t, txStore.Insert(rejected), errOldNonce)
	}

	require.Equal(t, expectedInserted, txStore.State().total.count)

	// Seed the stable-ready history with transactions that are already ready
	// after the initial inserts.
	for _, account := range accounts {
		balance := app.balanceOf(account.address)
		for nonce := account.baseNonce; ; nonce++ {
			wtx, ok := account.byNonce[nonce]
			if !ok {
				break
			}
			if wtx.evm.OrPanic("evm tx").requiredBalance.Cmp(balance) > 0 {
				break
			}
			everReady[wtx.Hash()] = struct{}{}
		}
	}

	// Advance the per-account nonce frontier in several randomized rounds and
	// verify that Update removes every transaction that fell below the account
	// nonce while preserving the rest.
	for height := range int64(5) {
		for _, account := range accounts {
			currentNonce := app.nextNonce[account.address]
			if currentNonce > 0 {
				rejected := makeTx(account.address, currentNonce-1)
				require.ErrorIs(t, txStore.Insert(rejected), errOldNonce)
			}
			maxAdvance := max(0, int(account.lastNonce-currentNonce)+4)
			for range rng.Intn(maxAdvance + 1) {
				app.markMined(account.address)
			}
			app.setBalance(account.address, big.NewInt(rng.Int63n(256)))
		}

		txStore.Update(updateSpec{
			Now:           time.Now(),
			Height:        height + 1,
			TxResults:     map[types.TxHash]bool{},
			Constraints:   NopTxConstraints(),
			NewPriorities: map[types.TxHash]int64{},
		})

		// Derive the expected remaining/ready sets from the test model:
		// all txs at or above the current account nonce remain present, and the
		// ready prefix is the contiguous run starting at the current nonce.
		expectedRemaining := 0
		expectedReady := 0
		expectedStableReady := 0
		for _, account := range accounts {
			currentNonce := app.nextNonce[account.address]
			balance := app.balanceOf(account.address)
			for nonce, wtx := range account.byNonce {
				got, ok := txStore.ByHash(wtx.Hash())
				if nonce < currentNonce {
					require.False(t, ok)
					continue
				}
				require.True(t, ok)
				require.Equal(t, wtx.Tx(), got)
				expectedRemaining++
				if _, wasReady := everReady[wtx.Hash()]; wasReady {
					expectedStableReady++
				}
			}
			for nonce := currentNonce; ; nonce++ {
				wtx, ok := account.byNonce[nonce]
				if !ok {
					break
				}
				if wtx.evm.OrPanic("evm tx").requiredBalance.Cmp(balance) > 0 {
					break
				}
				expectedReady++
				if _, wasReady := everReady[wtx.Hash()]; !wasReady {
					everReady[wtx.Hash()] = struct{}{}
					expectedStableReady++
				}
			}
		}
		state := txStore.State()
		require.Equal(t, expectedRemaining, state.total.count)
		require.Equal(t, expectedReady, state.ready.count)

		// Reap returns the currently ready transactions, while readyTxs is a
		// stable list of transactions that have become ready at least once and
		// have not been removed from the store.
		reaped, _ := txStore.Reap(ReapLimits{
			MaxTxs: utils.Some(uint64(expectedRemaining)),
		}, false)
		listed := make(types.Txs, 0, expectedStableReady)
		listedSet := make(map[types.TxHash]struct{}, expectedStableReady)
		for el := txStore.readyTxs.Front(); el != nil; el = el.Next() {
			tx := el.Value()
			listed = append(listed, tx)
			listedSet[tx.Hash()] = struct{}{}
		}
		require.Len(t, reaped, expectedReady)
		require.Len(t, listed, expectedStableReady)
		for _, tx := range reaped {
			_, ok := listedSet[tx.Hash()]
			require.True(t, ok)
		}
	}
}

type txStoreReplacementTestEnv struct {
	address common.Address
	app     *evmNonceApp
	txStore *txStore
}

func newTxStoreReplacementTestEnv(t *testing.T, rng utils.Rng) txStoreReplacementTestEnv {
	t.Helper()
	app := newEVMNonceApp()
	return txStoreReplacementTestEnv{
		address: common.BytesToAddress(utils.GenBytes(rng, common.AddressLength)),
		app:     app,
		txStore: NewTxStore(TestConfig(), proxy.New(app, proxy.NopMetrics()), NopMetrics()),
	}
}

func (e txStoreReplacementTestEnv) makeTx(rng utils.Rng, nonce uint64, priority int64, requiredBalance int) *WrappedTx {
	return &WrappedTx{
		hashedTx:     newHashedTx(utils.GenBytes(rng, rng.Intn(48)+16)),
		timestamp:    time.Now(),
		priority:     priority,
		gasWanted:    1,
		estimatedGas: 1,
		evm: utils.Some(evmTx{
			address:         e.address,
			seiAddress:      e.address.Bytes(),
			nonce:           nonce,
			requiredBalance: big.NewInt(int64(requiredBalance)),
		}),
	}
}

func (e txStoreReplacementTestEnv) assertState(t *testing.T, ready, pending []*WrappedTx) {
	t.Helper()
	expected := txStoreStateForTest(ready, pending)
	require.Equal(t, expected, e.txStore.State())
	reaped, _ := e.txStore.Reap(ReapLimits{MaxTxs: utils.Some(uint64(expected.total.count))}, false)
	var expectedReady types.Txs
	if len(ready) > 0 {
		expectedReady = make(types.Txs, 0, len(ready))
		for _, wtx := range ready {
			expectedReady = append(expectedReady, wtx.Tx())
		}
	}
	require.Equal(t, expectedReady, reaped)
}

func (e txStoreReplacementTestEnv) assertReadyList(t *testing.T, expected types.Txs) {
	t.Helper()
	var listed types.Txs
	for el := e.txStore.readyTxs.Front(); el != nil; el = el.Next() {
		listed = append(listed, el.Value())
	}
	require.Equal(t, expected, listed)
}

func TestTxStore_ReplacesReadyTxByHigherPriority(t *testing.T) {
	rng := utils.TestRng()
	env := newTxStoreReplacementTestEnv(t, rng)
	env.app.nextNonce[env.address] = 7
	env.app.setBalance(env.address, big.NewInt(100))

	// Insert one ready transaction, then replace it with a higher-priority ready
	// transaction for the same nonce.
	old := env.makeTx(rng, 7, 10, 20)
	require.NoError(t, env.txStore.Insert(old))
	env.assertState(t, []*WrappedTx{old}, nil)
	env.assertReadyList(t, types.Txs{old.Tx()})

	replacement := env.makeTx(rng, 7, 20, 30)
	require.NoError(t, env.txStore.Insert(replacement))
	env.assertState(t, []*WrappedTx{replacement}, nil)
	env.assertReadyList(t, types.Txs{replacement.Tx()})
	_, ok := env.txStore.ByHash(old.Hash())
	require.False(t, ok)
	got, ok := env.txStore.ByHash(replacement.Hash())
	require.True(t, ok)
	require.Equal(t, replacement.Tx(), got)

	// A higher-priority transaction that would no longer be ready must not
	// replace the current ready transaction for the same nonce.
	blocked := env.makeTx(rng, 7, 30, 101)
	require.ErrorIs(t, env.txStore.Insert(blocked), errSameNonce)

	env.assertState(t, []*WrappedTx{replacement}, nil)
	env.assertReadyList(t, types.Txs{replacement.Tx()})
	got, ok = env.txStore.ByHash(replacement.Hash())
	require.True(t, ok)
	require.Equal(t, replacement.Tx(), got)
	_, ok = env.txStore.ByHash(blocked.Hash())
	require.False(t, ok)
}

func TestTxStore_ReplacesReadyThenPendingTxByHigherPriority(t *testing.T) {
	rng := utils.TestRng()
	env := newTxStoreReplacementTestEnv(t, rng)
	env.app.nextNonce[env.address] = 7
	env.app.setBalance(env.address, big.NewInt(100))

	becamePending := env.makeTx(rng, 7, 40, 60)
	require.NoError(t, env.txStore.Insert(becamePending))
	env.assertState(t, []*WrappedTx{becamePending}, nil)
	env.assertReadyList(t, types.Txs{becamePending.Tx()})

	env.app.setBalance(env.address, big.NewInt(50))
	env.txStore.Update(updateSpec{
		Now:           time.Now(),
		Height:        1,
		TxResults:     map[types.TxHash]bool{},
		Constraints:   NopTxConstraints(),
		NewPriorities: map[types.TxHash]int64{},
	})
	env.assertState(t, nil, []*WrappedTx{becamePending})
	env.assertReadyList(t, types.Txs{becamePending.Tx()})

	becamePendingReplacement := env.makeTx(rng, 7, 50, 70)
	require.NoError(t, env.txStore.Insert(becamePendingReplacement))
	env.assertState(t, nil, []*WrappedTx{becamePendingReplacement})
	env.assertReadyList(t, nil)
	_, ok := env.txStore.ByHash(becamePending.Hash())
	require.False(t, ok)
	got, ok := env.txStore.ByHash(becamePendingReplacement.Hash())
	require.True(t, ok)
	require.Equal(t, becamePendingReplacement.Tx(), got)
}

func TestTxStore_ReplacesPendingTxByHigherPriority(t *testing.T) {
	rng := utils.TestRng()
	env := newTxStoreReplacementTestEnv(t, rng)
	env.app.nextNonce[env.address] = 7
	env.app.setBalance(env.address, big.NewInt(0))

	pending := env.makeTx(rng, 7, 70, 40)
	require.NoError(t, env.txStore.Insert(pending))
	env.assertState(t, nil, []*WrappedTx{pending})
	env.assertReadyList(t, nil)

	pendingReplacement := env.makeTx(rng, 7, 90, 50)
	require.NoError(t, env.txStore.Insert(pendingReplacement))
	env.assertState(t, nil, []*WrappedTx{pendingReplacement})
	env.assertReadyList(t, nil)
	_, ok := env.txStore.ByHash(pending.Hash())
	require.False(t, ok)
	got, ok := env.txStore.ByHash(pendingReplacement.Hash())
	require.True(t, ok)
	require.Equal(t, pendingReplacement.Tx(), got)
}
