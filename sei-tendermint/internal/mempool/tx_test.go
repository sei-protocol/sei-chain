package mempool

import (
	"fmt"
	"sort"
	"testing"
	"time"

	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
	"github.com/sei-protocol/sei-chain/sei-tendermint/types"
)

func TestTxStore_GetTxBySender(t *testing.T) {
	txs := NewTxStore()
	wtx := &WrappedTx{
		hashedTx:  newHashedTx(types.Tx("test_tx")),
		sender:    "foo",
		priority:  1,
		timestamp: time.Now(),
	}

	res := txs.GetTxBySender(wtx.sender)
	require.Nil(t, res)

	txs.SetTx(wtx)

	res = txs.GetTxBySender(wtx.sender)
	require.NotNil(t, res)
	require.Equal(t, wtx, res)
}

func TestTxStore_GetTxByHash(t *testing.T) {
	txs := NewTxStore()
	wtx := &WrappedTx{
		hashedTx:  newHashedTx(types.Tx("test_tx")),
		sender:    "foo",
		priority:  1,
		timestamp: time.Now(),
	}

	key := wtx.Key()
	res := txs.GetTxByHash(key)
	require.Nil(t, res)

	txs.SetTx(wtx)

	res = txs.GetTxByHash(key)
	require.NotNil(t, res)
	require.Equal(t, wtx, res)
}

func TestTxStore_SetTx(t *testing.T) {
	txs := NewTxStore()
	wtx := &WrappedTx{
		hashedTx:  newHashedTx(types.Tx("test_tx")),
		priority:  1,
		timestamp: time.Now(),
	}

	key := wtx.Key()
	txs.SetTx(wtx)

	res := txs.GetTxByHash(key)
	require.NotNil(t, res)
	require.Equal(t, wtx, res)

	wtx.sender = "foo"
	txs.SetTx(wtx)

	res = txs.GetTxByHash(key)
	require.NotNil(t, res)
	require.Equal(t, wtx, res)
}

func TestTxStore_IsTxRemoved(t *testing.T) {
	// Initialize the store
	txs := NewTxStore()

	// Current time for timestamping transactions
	now := time.Now()

	// Tests setup as a slice of anonymous structs
	tests := []struct {
		name        string
		wtx         *WrappedTx
		setup       func(*TxStore, *WrappedTx) // Optional setup function to manipulate store state
		wantRemoved bool
	}{
		{
			name: "Existing transaction not removed",
			wtx: &WrappedTx{
				hashedTx:  newHashedTx(types.Tx("tx_hash_1")),
				removed:   false,
				timestamp: now,
			},
			setup: func(ts *TxStore, w *WrappedTx) {
				ts.SetTx(w)
			},
			wantRemoved: false,
		},
		{
			name: "Existing transaction marked as removed",
			wtx: &WrappedTx{
				hashedTx:  newHashedTx(types.Tx("tx_hash_2")),
				removed:   true,
				timestamp: now,
			},
			setup: func(ts *TxStore, w *WrappedTx) {
				ts.SetTx(w)
			},
			wantRemoved: true,
		},
		{
			name: "Non-existing transaction",
			wtx: &WrappedTx{
				hashedTx:  newHashedTx(types.Tx("tx_hash_3")),
				removed:   false,
				timestamp: now,
			},
			wantRemoved: false,
		},
		{
			name: "Non-existing transaction but marked as removed",
			wtx: &WrappedTx{
				hashedTx:  newHashedTx(types.Tx("tx_hash_4")),
				removed:   true,
				timestamp: now,
			},
			wantRemoved: true,
		},
	}

	// Execute test scenarios
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.setup != nil {
				tt.setup(txs, tt.wtx)
			}
			removed := txs.IsTxRemoved(tt.wtx)
			require.Equal(t, tt.wantRemoved, removed)
		})
	}
}

func TestTxStore_GetOrSetPeerByTxHash(t *testing.T) {
	txs := NewTxStore()
	wtx := &WrappedTx{
		hashedTx:  newHashedTx(types.Tx("test_tx")),
		priority:  1,
		timestamp: time.Now(),
	}

	key := wtx.Key()
	txs.SetTx(wtx)

	res, ok := txs.GetOrSetPeerByTxHash(types.Tx([]byte("test_tx_2")).Key(), 15)
	require.Nil(t, res)
	require.False(t, ok)

	res, ok = txs.GetOrSetPeerByTxHash(key, 15)
	require.NotNil(t, res)
	require.False(t, ok)

	res, ok = txs.GetOrSetPeerByTxHash(key, 15)
	require.NotNil(t, res)
	require.True(t, ok)

	require.True(t, txs.TxHasPeer(key, 15))
	require.False(t, txs.TxHasPeer(key, 16))
}

func TestTxStore_RemoveTx(t *testing.T) {
	txs := NewTxStore()
	wtx := &WrappedTx{
		hashedTx:  newHashedTx(types.Tx("test_tx")),
		priority:  1,
		timestamp: time.Now(),
	}

	txs.SetTx(wtx)

	key := wtx.Key()
	res := txs.GetTxByHash(key)
	require.NotNil(t, res)

	txs.RemoveTx(res)

	res = txs.GetTxByHash(key)
	require.Nil(t, res)
}

func TestTxStore_Size(t *testing.T) {
	txStore := NewTxStore()
	numTxs := 1000

	for i := range numTxs {
		txStore.SetTx(&WrappedTx{
			hashedTx:  newHashedTx(fmt.Appendf(nil, "test_tx_%d", i)),
			priority:  int64(i),
			timestamp: time.Now(),
		})
	}

	require.Equal(t, numTxs, txStore.Size())
}

func TestWrappedTxList(t *testing.T) {
	list := NewWrappedTxList()
	rng := utils.TestRng()
	now := time.Now()

	t.Log("insert a bunch of random transactions")
	var txs []*WrappedTx
	for range 100 {
		tx := make(types.Tx, 32)
		_, err := rng.Read(tx)
		require.NoError(t, err)

		wtx := &WrappedTx{
			hashedTx:  newHashedTx(tx),
			height:    rng.Int63(),
			timestamp: now.Add(time.Duration(rng.Int63n(1000000000000)) * time.Nanosecond),
		}
		txs = append(txs, wtx)
		list.Insert(wtx)
	}

	t.Log("remove some of them")
	n := 50
	rng.Shuffle(len(txs), func(i, j int) { txs[i], txs[j] = txs[j], txs[i] })
	for _, wtx := range txs[:n] {
		list.Remove(wtx)
	}
	txs = txs[n:]

	t.Log("purge by timestamp")
	sort.Slice(txs, func(i, j int) bool { return txs[i].timestamp.Before(txs[j].timestamp) })
	n = 10
	want := map[types.TxHash]struct{}{}
	for _, wtx := range txs[:n] {
		want[wtx.Key()] = struct{}{}
	}
	got := map[types.TxHash]struct{}{}
	for _, wtx := range list.Purge(utils.Some(txs[n].timestamp), utils.None[int64]()) {
		got[wtx.Key()] = struct{}{}
	}
	require.Equal(t, want, got)
	txs = txs[n:]

	t.Log("purge by height")
	sort.Slice(txs, func(i, j int) bool { return txs[i].height < txs[j].height })
	n = 15
	want = map[types.TxHash]struct{}{}
	for _, wtx := range txs[:n] {
		want[wtx.Key()] = struct{}{}
	}
	got = map[types.TxHash]struct{}{}
	for _, wtx := range list.Purge(utils.None[time.Time](), utils.Some(txs[n].height)) {
		got[wtx.Key()] = struct{}{}
	}
	require.Equal(t, want, got)
	txs = txs[n:]

	t.Log("reset the list")
	list.Reset()
	require.Equal(t, 0, list.Size())
}

func TestPendingTxsPopTxsGood(t *testing.T) {
	pendingTxs := NewPendingTxs(DefaultConfig())
	for _, test := range []struct {
		origLen    int
		popIndices []int
		expected   []int
	}{
		{
			origLen:    1,
			popIndices: []int{},
			expected:   []int{0},
		}, {
			origLen:    1,
			popIndices: []int{0},
			expected:   []int{},
		}, {
			origLen:    2,
			popIndices: []int{0},
			expected:   []int{1},
		}, {
			origLen:    2,
			popIndices: []int{1},
			expected:   []int{0},
		}, {
			origLen:    2,
			popIndices: []int{0, 1},
			expected:   []int{},
		}, {
			origLen:    3,
			popIndices: []int{1},
			expected:   []int{0, 2},
		}, {
			origLen:    3,
			popIndices: []int{0, 2},
			expected:   []int{1},
		}, {
			origLen:    3,
			popIndices: []int{0, 1, 2},
			expected:   []int{},
		}, {
			origLen:    5,
			popIndices: []int{0, 1, 4},
			expected:   []int{2, 3},
		}, {
			origLen:    5,
			popIndices: []int{1, 3},
			expected:   []int{0, 2, 4},
		},
	} {
		for inner := range pendingTxs.inner.Lock() {
			inner.txs = []TxWithResponse{}
			pendingTxs.sizeBytes.Store(0)
			for i := 0; i < test.origLen; i++ {
				inner.txs = append(inner.txs, TxWithResponse{
					tx:     &WrappedTx{hashedTx: newHashedTx(types.Tx{})},
					txInfo: TxInfo{SenderID: uint16(i)},
				})
			}
			pendingTxs.popTxsAtIndices(inner, test.popIndices)
			require.Equal(t, len(test.expected), len(inner.txs))
			for i, e := range test.expected {
				require.Equal(t, e, int(inner.txs[i].txInfo.SenderID))
			}
		}
	}
}

func TestPendingTxsPopTxsBad(t *testing.T) {
	pendingTxs := NewPendingTxs(DefaultConfig())
	// out of range
	require.Panics(t, func() {
		for inner := range pendingTxs.inner.Lock() {
			pendingTxs.popTxsAtIndices(inner, []int{0})
		}
	})
	// out of order
	for inner := range pendingTxs.inner.Lock() {
		inner.txs = []TxWithResponse{{}, {}, {}}
	}
	require.Panics(t, func() {
		for inner := range pendingTxs.inner.Lock() {
			pendingTxs.popTxsAtIndices(inner, []int{1, 0})
		}
	})
	// duplicate
	require.Panics(t, func() {
		for inner := range pendingTxs.inner.Lock() {
			pendingTxs.popTxsAtIndices(inner, []int{2, 2})
		}
	})
}

func TestPendingTxs_InsertCondition(t *testing.T) {
	mempoolCfg := DefaultConfig()

	// First test exceeding number of txs
	mempoolCfg.PendingSize = 2

	pendingTxs := NewPendingTxs(mempoolCfg)

	// Transaction setup
	tx1 := &WrappedTx{
		hashedTx: newHashedTx(types.Tx("tx1_data")),
		priority: 1,
	}
	tx1Size := tx1.Size()

	tx2 := &WrappedTx{
		hashedTx: newHashedTx(types.Tx("tx2_data")),
		priority: 2,
	}
	tx2Size := tx2.Size()

	err := pendingTxs.Insert(tx1, &abci.ResponseCheckTxV2{}, TxInfo{})
	require.Nil(t, err)

	err = pendingTxs.Insert(tx2, &abci.ResponseCheckTxV2{}, TxInfo{})
	require.Nil(t, err)

	// Should fail due to pending store size limit
	tx3 := &WrappedTx{
		hashedTx: newHashedTx(types.Tx("tx3_data_exceeding_pending_size")),
		priority: 3,
	}

	err = pendingTxs.Insert(tx3, &abci.ResponseCheckTxV2{}, TxInfo{})
	require.NotNil(t, err)

	// Second test exceeding byte size condition
	mempoolCfg.PendingSize = 5
	pendingTxs = NewPendingTxs(mempoolCfg)
	mempoolCfg.MaxPendingTxsBytes = int64(tx1Size + tx2Size)

	err = pendingTxs.Insert(tx1, &abci.ResponseCheckTxV2{}, TxInfo{})
	require.Nil(t, err)

	err = pendingTxs.Insert(tx2, &abci.ResponseCheckTxV2{}, TxInfo{})
	require.Nil(t, err)

	// Should fail due to exceeding max pending transaction bytes
	tx3 = &WrappedTx{
		hashedTx: newHashedTx(types.Tx("tx3_small_but_exceeds_byte_limit")),
		priority: 3,
	}

	err = pendingTxs.Insert(tx3, &abci.ResponseCheckTxV2{}, TxInfo{})
	require.NotNil(t, err)
}
