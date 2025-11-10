package mempool

import (
	"fmt"
	"sort"
	"testing"
	"time"

	abci "github.com/tendermint/tendermint/abci/types"
	"github.com/tendermint/tendermint/config"
	"github.com/tendermint/tendermint/libs/utils"
	"github.com/tendermint/tendermint/libs/utils/require"
	"github.com/tendermint/tendermint/types"
)

func TestTxStore_GetTxBySender(t *testing.T) {
	txs := NewTxStore()
	wtx := &WrappedTx{
		tx:        []byte("test_tx"),
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
		tx:        []byte("test_tx"),
		sender:    "foo",
		priority:  1,
		timestamp: time.Now(),
	}

	key := wtx.tx.Key()
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
		tx:        []byte("test_tx"),
		priority:  1,
		timestamp: time.Now(),
	}

	key := wtx.tx.Key()
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
				tx:        types.Tx("tx_hash_1"),
				hash:      types.Tx("tx_hash_1").Key(),
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
				tx:        types.Tx("tx_hash_2"),
				hash:      types.Tx("tx_hash_2").Key(),
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
				tx:        types.Tx("tx_hash_3"),
				hash:      types.Tx("tx_hash_3").Key(),
				removed:   false,
				timestamp: now,
			},
			wantRemoved: false,
		},
		{
			name: "Non-existing transaction but marked as removed",
			wtx: &WrappedTx{
				tx:        types.Tx("tx_hash_4"),
				hash:      types.Tx("tx_hash_4").Key(),
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
		tx:        []byte("test_tx"),
		priority:  1,
		timestamp: time.Now(),
	}

	key := wtx.tx.Key()
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
		tx:        []byte("test_tx"),
		priority:  1,
		timestamp: time.Now(),
	}

	txs.SetTx(wtx)

	key := wtx.tx.Key()
	res := txs.GetTxByHash(key)
	require.NotNil(t, res)

	txs.RemoveTx(res)

	res = txs.GetTxByHash(key)
	require.Nil(t, res)
}

func TestTxStore_Size(t *testing.T) {
	txStore := NewTxStore()
	numTxs := 1000

	for i := 0; i < numTxs; i++ {
		txStore.SetTx(&WrappedTx{
			tx:        []byte(fmt.Sprintf("test_tx_%d", i)),
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
		wtx := &WrappedTx{
			height:    rng.Int63(),
			timestamp: now.Add(time.Duration(rng.Int63n(1000000000000)) * time.Nanosecond),
		}
		_, err := rng.Read(wtx.hash[:])
		require.NoError(t, err)
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
	want := map[types.TxKey]struct{}{}
	for _, wtx := range txs[:n] {
		want[wtx.hash] = struct{}{}
	}
	got := map[types.TxKey]struct{}{}
	for _, wtx := range list.Purge(utils.Some(txs[n].timestamp), utils.None[int64]()) {
		got[wtx.hash] = struct{}{}
	}
	require.Equal(t, want, got)
	txs = txs[n:]

	t.Log("purge by height")
	sort.Slice(txs, func(i, j int) bool { return txs[i].height < txs[j].height })
	n = 15
	want = map[types.TxKey]struct{}{}
	for _, wtx := range txs[:n] {
		want[wtx.hash] = struct{}{}
	}
	got = map[types.TxKey]struct{}{}
	for _, wtx := range list.Purge(utils.None[time.Time](), utils.Some(txs[n].height)) {
		got[wtx.hash] = struct{}{}
	}
	require.Equal(t, want, got)
	txs = txs[n:]

	t.Log("reset the list")
	list.Reset()
	require.Equal(t, 0, list.Size())
}

func TestPendingTxsPopTxsGood(t *testing.T) {
	pendingTxs := NewPendingTxs(config.TestMempoolConfig())
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
		pendingTxs.txs = []TxWithResponse{}
		for i := 0; i < test.origLen; i++ {
			pendingTxs.txs = append(pendingTxs.txs, TxWithResponse{
				tx:     &WrappedTx{tx: []byte{}},
				txInfo: TxInfo{SenderID: uint16(i)}})
		}
		pendingTxs.popTxsAtIndices(test.popIndices)
		require.Equal(t, len(test.expected), len(pendingTxs.txs))
		for i, e := range test.expected {
			require.Equal(t, e, int(pendingTxs.txs[i].txInfo.SenderID))
		}
	}
}

func TestPendingTxsPopTxsBad(t *testing.T) {
	pendingTxs := NewPendingTxs(config.TestMempoolConfig())
	// out of range
	require.Panics(t, func() { pendingTxs.popTxsAtIndices([]int{0}) })
	// out of order
	pendingTxs.txs = []TxWithResponse{{}, {}, {}}
	require.Panics(t, func() { pendingTxs.popTxsAtIndices([]int{1, 0}) })
	// duplicate
	require.Panics(t, func() { pendingTxs.popTxsAtIndices([]int{2, 2}) })
}

func TestPendingTxs_InsertCondition(t *testing.T) {
	mempoolCfg := config.TestMempoolConfig()

	// First test exceeding number of txs
	mempoolCfg.PendingSize = 2

	pendingTxs := NewPendingTxs(mempoolCfg)

	// Transaction setup
	tx1 := &WrappedTx{
		tx:       types.Tx("tx1_data"),
		priority: 1,
	}
	tx1Size := tx1.Size()

	tx2 := &WrappedTx{
		tx:       types.Tx("tx2_data"),
		priority: 2,
	}
	tx2Size := tx2.Size()

	err := pendingTxs.Insert(tx1, &abci.ResponseCheckTxV2{}, TxInfo{})
	require.Nil(t, err)

	err = pendingTxs.Insert(tx2, &abci.ResponseCheckTxV2{}, TxInfo{})
	require.Nil(t, err)

	// Should fail due to pending store size limit
	tx3 := &WrappedTx{
		tx:       types.Tx("tx3_data_exceeding_pending_size"),
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
		tx:       types.Tx("tx3_small_but_exceeds_byte_limit"),
		priority: 3,
	}

	err = pendingTxs.Insert(tx3, &abci.ResponseCheckTxV2{}, TxInfo{})
	require.NotNil(t, err)
}
