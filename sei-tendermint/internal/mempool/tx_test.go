package mempool

import (
	"fmt"
	"math/big"
	"slices"
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
				requiredBalance: big.NewInt(0),
			}),
		}
	}

	// Seed the store with sparse per-account nonce ranges so each account has a
	// mix of contiguous ready transactions and gaps that keep later transactions
	// pending.
	accounts := make([]accountCase, 8)
	expectedInserted := 0
	for i := range accounts {
		accounts[i].address = common.BytesToAddress(utils.GenBytes(rng, common.AddressLength))
		accounts[i].baseNonce = uint64(rng.Intn(20) + 1)
		accounts[i].byNonce = map[uint64]*WrappedTx{}
		rangeLen := rng.Intn(16) + 12
		accounts[i].lastNonce = accounts[i].baseNonce + uint64(rangeLen-1)
		app.nextNonce[accounts[i].address.Hex()] = accounts[i].baseNonce
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

	// Advance the per-account nonce frontier in several randomized rounds and
	// verify that Update removes every transaction that fell below the account
	// nonce while preserving the rest.
	for height := range int64(5) {
		for _, account := range accounts {
			currentNonce := app.nextNonce[account.address.Hex()]
			if currentNonce > 0 {
				rejected := makeTx(account.address, currentNonce-1)
				require.ErrorIs(t, txStore.Insert(rejected), errOldNonce)
			}
			maxAdvance := max(0,int(account.lastNonce-currentNonce) + 4)
			for range rng.Intn(maxAdvance + 1) {
				app.markMined(account.address.Hex())
			}
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
		expectedReaped := make(types.Txs, 0, expectedRemaining)
		for _, account := range accounts {
			currentNonce := app.nextNonce[account.address.Hex()]
			for nonce, wtx := range account.byNonce {
				got, ok := txStore.ByHash(wtx.Hash())
				if nonce < currentNonce {
					require.False(t, ok)
					continue
				}
				require.True(t, ok)
				require.Equal(t, wtx.Tx(), got)
				expectedRemaining++
			}
			for nonce := currentNonce; ; nonce++ {
				wtx, ok := account.byNonce[nonce]
				if !ok {
					break
				}
				expectedReady++
				expectedReaped = append(expectedReaped, wtx.Tx())
			}
		}
		state := txStore.State()
		require.Equal(t, expectedRemaining, state.total.count)
		require.Equal(t, expectedReady, state.ready.count)

		// The ready set must agree across all public/readable surfaces: Reap and
		// the internal ready list.
		reaped, _ := txStore.Reap(ReapLimits{
			MaxTxs: utils.Some(uint64(expectedRemaining)),
		}, false)
		listed := make(types.Txs, 0, expectedReady)
		for el := txStore.readyTxs.Front(); el != nil; el = el.Next() {
			listed = append(listed, el.Value())
		}
		slices.SortFunc(reaped, slices.Compare[types.Tx])
		slices.SortFunc(listed, slices.Compare[types.Tx])
		slices.SortFunc(expectedReaped, slices.Compare[types.Tx])
		require.ElementsMatch(t, expectedReaped, reaped)
		require.ElementsMatch(t, expectedReaped, listed)
	}
}
