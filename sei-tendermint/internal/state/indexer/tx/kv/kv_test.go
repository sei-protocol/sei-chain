package kv

import (
	"context"
	"fmt"
	"testing"

	"github.com/gogo/protobuf/proto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	dbm "github.com/tendermint/tm-db"

	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/pubsub/query"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/state/indexer"
	tmrand "github.com/sei-protocol/sei-chain/sei-tendermint/libs/rand"
	"github.com/sei-protocol/sei-chain/sei-tendermint/types"
)

// searchOpts is the zero-value (unbounded, ascending) options used by the tests
// in this package
var searchOpts = indexer.SearchOptions{}

func TestTxIndex(t *testing.T) {
	txIndexer := NewTxIndex(dbm.NewMemDB())

	tx := types.Tx("HELLO WORLD")
	txResult := &abci.TxResultV2{
		Height: 1,
		Index:  0,
		Tx:     tx,
		Result: abci.ExecTxResult{
			Data: []byte{0},
			Code: abci.CodeTypeOK, Log: "", Events: nil,
		},
	}
	hash := tx.Hash()

	batch := indexer.NewBatch(1)
	if err := batch.Add(txResult); err != nil {
		t.Error(err)
	}
	err := txIndexer.Index(batch.Ops)
	require.NoError(t, err)

	loadedTxResult, err := txIndexer.Get(hash.Bytes())
	require.NoError(t, err)
	assert.Equal(t, txResult, loadedTxResult)

	tx2 := types.Tx("BYE BYE WORLD")
	txResult2 := &abci.TxResultV2{
		Height: 1,
		Index:  0,
		Tx:     tx2,
		Result: abci.ExecTxResult{
			Data: []byte{0},
			Code: abci.CodeTypeOK, Log: "", Events: nil,
		},
	}
	hash2 := tx2.Hash()

	err = txIndexer.Index([]*abci.TxResultV2{txResult2})
	require.NoError(t, err)

	loadedTxResult2, err := txIndexer.Get(hash2.Bytes())
	require.NoError(t, err)
	assert.Equal(t, txResult2, loadedTxResult2)
}

func TestTxSearch(t *testing.T) {
	indexer := NewTxIndex(dbm.NewMemDB())

	txResult := txResultWithEvents([]abci.Event{
		{Type: "account", Attributes: []abci.EventAttribute{{Key: []byte("number"), Value: []byte("1"), Index: true}}},
		{Type: "account", Attributes: []abci.EventAttribute{{Key: []byte("owner"), Value: []byte("Ivan"), Index: true}}},
		{Type: "", Attributes: []abci.EventAttribute{{Key: []byte("not_allowed"), Value: []byte("Vlad"), Index: true}}},
	})
	hash := types.Tx(txResult.Tx).Hash()

	err := indexer.Index([]*abci.TxResultV2{txResult})
	require.NoError(t, err)

	testCases := []struct {
		q             string
		resultsLength int
	}{
		// search by hash
		{fmt.Sprintf("tx.hash = '%X'", hash), 1},
		// search by exact match (one key)
		{"account.number = 1", 1},
		// search by exact match (two keys)
		{"account.number = 1 AND account.owner = 'Ivan'", 1},
		// search by exact match (two keys)
		{"account.number = 1 AND account.owner = 'Vlad'", 0},
		{"account.owner = 'Vlad' AND account.number = 1", 0},
		{"account.number >= 1 AND account.owner = 'Vlad'", 0},
		{"account.owner = 'Vlad' AND account.number >= 1", 0},
		{"account.number <= 0", 0},
		{"account.number <= 0 AND account.owner = 'Ivan'", 0},
		// search using a prefix of the stored value
		{"account.owner = 'Iv'", 0},
		// search by range
		{"account.number >= 1 AND account.number <= 5", 1},
		// search by range (lower bound)
		{"account.number >= 1", 1},
		// search by range (upper bound)
		{"account.number <= 5", 1},
		// search using not allowed key
		{"not_allowed = 'boom'", 0},
		// search for not existing tx result
		{"account.number >= 2 AND account.number <= 5", 0},
		// search using not existing key
		{"account.date >= TIME 2013-05-03T14:45:00Z", 0},
		// search using CONTAINS
		{"account.owner CONTAINS 'an'", 1},
		// search for non existing value using CONTAINS
		{"account.owner CONTAINS 'Vlad'", 0},
		// search using the wrong key (of numeric type) using CONTAINS
		{"account.number CONTAINS 'Iv'", 0},
		// search using MATCHES
		{"account.owner MATCHES '.*an.*'", 1},
		// search for non existing value using MATCHES
		{"account.owner MATCHES '.*lad'", 0},
		// search using the wrong key (of numeric type) using MATCHES
		{"account.number MATCHES '.*v.*'", 0},
		// search using EXISTS
		{"account.number EXISTS", 1},
		// search using EXISTS for non existing key
		{"account.date EXISTS", 0},
		// search using height
		{"account.number = 1 AND tx.height = 1", 1},
		// search using incorrect height
		{"account.number = 1 AND tx.height = 3", 0},
		// search using height only
		{"tx.height = 1", 1},
	}

	ctx := t.Context()

	for _, tc := range testCases {
		t.Run(tc.q, func(t *testing.T) {
			results, err := indexer.Search(ctx, query.MustCompile(tc.q), searchOpts)
			assert.NoError(t, err)

			assert.Len(t, results, tc.resultsLength)
			if tc.resultsLength > 0 {
				for _, txr := range results {
					assert.Equal(t, txResult, txr)
				}
			}
		})
	}
}

func TestTxSearchWithCancelation(t *testing.T) {
	indexer := NewTxIndex(dbm.NewMemDB())

	txResult := txResultWithEvents([]abci.Event{
		{Type: "account", Attributes: []abci.EventAttribute{{Key: []byte("number"), Value: []byte("1"), Index: true}}},
		{Type: "account", Attributes: []abci.EventAttribute{{Key: []byte("owner"), Value: []byte("Ivan"), Index: true}}},
		{Type: "", Attributes: []abci.EventAttribute{{Key: []byte("not_allowed"), Value: []byte("Vlad"), Index: true}}},
	})
	err := indexer.Index([]*abci.TxResultV2{txResult})
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	results, err := indexer.Search(ctx, query.MustCompile(`account.number = 1`), searchOpts)
	assert.NoError(t, err)
	assert.Empty(t, results)
}

func TestTxSearchDeprecatedIndexing(t *testing.T) {
	indexer := NewTxIndex(dbm.NewMemDB())

	// index tx using events indexing (composite key)
	txResult1 := txResultWithEvents([]abci.Event{
		{Type: "account", Attributes: []abci.EventAttribute{{Key: []byte("number"), Value: []byte("1"), Index: true}}},
	})
	hash1 := types.Tx(txResult1.Tx).Hash()

	err := indexer.Index([]*abci.TxResultV2{txResult1})
	require.NoError(t, err)

	// index tx also using deprecated indexing (event as key)
	txResult2 := txResultWithEvents(nil)
	txResult2.Tx = types.Tx("HELLO WORLD 2")

	hash2 := types.Tx(txResult2.Tx).Hash()
	b := indexer.store.NewBatch()

	rawBytes, err := proto.Marshal(&abci.TxResult{Height: txResult2.Height, Index: txResult2.Index, Tx: txResult2.Tx, Result: txResult2.Result})
	require.NoError(t, err)

	depKey := []byte(fmt.Sprintf("%s/%s/%d/%d",
		"sender",
		"addr1",
		txResult2.Height,
		txResult2.Index,
	))

	err = b.Set(depKey, hash2.Bytes())
	require.NoError(t, err)
	err = b.Set(KeyFromHeight(txResult2), hash2.Bytes())
	require.NoError(t, err)
	err = b.Set(hash2.Bytes(), rawBytes)
	require.NoError(t, err)
	err = b.Write()
	require.NoError(t, err)

	testCases := []struct {
		q       string
		results []*abci.TxResultV2
	}{
		// search by hash
		{fmt.Sprintf("tx.hash = '%X'", hash1), []*abci.TxResultV2{txResult1}},
		// search by hash
		{fmt.Sprintf("tx.hash = '%X'", hash2), []*abci.TxResultV2{txResult2}},
		// search by exact match (one key)
		{"account.number = 1", []*abci.TxResultV2{txResult1}},
		{"account.number >= 1 AND account.number <= 5", []*abci.TxResultV2{txResult1}},
		// search by range (lower bound)
		{"account.number >= 1", []*abci.TxResultV2{txResult1}},
		// search by range (upper bound)
		{"account.number <= 5", []*abci.TxResultV2{txResult1}},
		// search using not allowed key
		{"not_allowed = 'boom'", []*abci.TxResultV2{}},
		// search for not existing tx result
		{"account.number >= 2 AND account.number <= 5", []*abci.TxResultV2{}},
		// search using not existing key
		{"account.date >= TIME 2013-05-03T14:45:00Z", []*abci.TxResultV2{}},
		// search by deprecated key
		{"sender = 'addr1'", []*abci.TxResultV2{txResult2}},
	}

	ctx := t.Context()

	for _, tc := range testCases {
		t.Run(tc.q, func(t *testing.T) {
			results, err := indexer.Search(ctx, query.MustCompile(tc.q), searchOpts)
			require.NoError(t, err)
			for _, txr := range results {
				for _, tr := range tc.results {
					assert.Equal(t, tr, txr)
				}
			}
		})
	}
}

func TestTxSearchOneTxWithMultipleSameTagsButDifferentValues(t *testing.T) {
	indexer := NewTxIndex(dbm.NewMemDB())

	txResult := txResultWithEvents([]abci.Event{
		{Type: "account", Attributes: []abci.EventAttribute{{Key: []byte("number"), Value: []byte("1"), Index: true}}},
		{Type: "account", Attributes: []abci.EventAttribute{{Key: []byte("number"), Value: []byte("2"), Index: true}}},
	})

	err := indexer.Index([]*abci.TxResultV2{txResult})
	require.NoError(t, err)

	ctx := t.Context()

	results, err := indexer.Search(ctx, query.MustCompile(`account.number >= 1`), searchOpts)
	assert.NoError(t, err)

	assert.Len(t, results, 1)
	for _, txr := range results {
		assert.Equal(t, txResult, txr)
	}
}

func TestTxSearchMultipleTxs(t *testing.T) {
	indexer := NewTxIndex(dbm.NewMemDB())

	// indexed first, but bigger height (to test the order of transactions)
	txResult := txResultWithEvents([]abci.Event{
		{Type: "account", Attributes: []abci.EventAttribute{{Key: []byte("number"), Value: []byte("1"), Index: true}}},
	})

	txResult.Tx = types.Tx("Bob's account")
	txResult.Height = 2
	txResult.Index = 1
	err := indexer.Index([]*abci.TxResultV2{txResult})
	require.NoError(t, err)

	// indexed second, but smaller height (to test the order of transactions)
	txResult2 := txResultWithEvents([]abci.Event{
		{Type: "account", Attributes: []abci.EventAttribute{{Key: []byte("number"), Value: []byte("2"), Index: true}}},
	})
	txResult2.Tx = types.Tx("Alice's account")
	txResult2.Height = 1
	txResult2.Index = 2

	err = indexer.Index([]*abci.TxResultV2{txResult2})
	require.NoError(t, err)

	// indexed third (to test the order of transactions)
	txResult3 := txResultWithEvents([]abci.Event{
		{Type: "account", Attributes: []abci.EventAttribute{{Key: []byte("number"), Value: []byte("3"), Index: true}}},
	})
	txResult3.Tx = types.Tx("Jack's account")
	txResult3.Height = 1
	txResult3.Index = 1
	err = indexer.Index([]*abci.TxResultV2{txResult3})
	require.NoError(t, err)

	// indexed fourth (to test we don't include txs with similar events)
	// https://github.com/tendermint/tendermint/issues/2908
	txResult4 := txResultWithEvents([]abci.Event{
		{Type: "account", Attributes: []abci.EventAttribute{{Key: []byte("number.id"), Value: []byte("1"), Index: true}}},
	})
	txResult4.Tx = types.Tx("Mike's account")
	txResult4.Height = 2
	txResult4.Index = 2
	err = indexer.Index([]*abci.TxResultV2{txResult4})
	require.NoError(t, err)

	ctx := t.Context()

	results, err := indexer.Search(ctx, query.MustCompile(`account.number >= 1`), searchOpts)
	assert.NoError(t, err)

	require.Len(t, results, 3)
}

func TestTxSearchBounded(t *testing.T) {
	idx := NewTxIndex(dbm.NewMemDB())

	// Two txs per height for heights 1..5. Every tx carries app.name=sei; the
	// index-0 tx of each height also carries app.kind=even. Tx bytes are unique
	// per (height, index) so each has a distinct hash.
	for h := int64(1); h <= 5; h++ {
		for i := uint32(0); i < 2; i++ {
			events := []abci.Event{{
				Type:       "app",
				Attributes: []abci.EventAttribute{{Key: []byte("name"), Value: []byte("sei"), Index: true}},
			}}
			if i == 0 {
				events = append(events, abci.Event{
					Type:       "app",
					Attributes: []abci.EventAttribute{{Key: []byte("kind"), Value: []byte("even"), Index: true}},
				})
			}
			res := &abci.TxResultV2{
				Height: h,
				Index:  i,
				Tx:     types.Tx(fmt.Sprintf("tx-%d-%d", h, i)),
				Result: abci.ExecTxResult{Code: abci.CodeTypeOK, Events: events},
			}
			require.NoError(t, idx.Index([]*abci.TxResultV2{res}))
		}
	}

	// hi is a (height, index) pair used to assert result identity/order.
	type hi struct {
		h int64
		i uint32
	}
	pairs := func(results []*abci.TxResultV2) []hi {
		out := make([]hi, len(results))
		for k, r := range results {
			out[k] = hi{r.Height, r.Index}
		}
		return out
	}

	testCases := map[string]struct {
		q    string
		opts indexer.SearchOptions
		want []hi
	}{
		// Fast path: single equality driver, scanned in order_by order and capped
		// at the scan rather than after a full sort.
		"equality desc limit 3": {
			q:    `app.name = 'sei'`,
			opts: indexer.SearchOptions{Limit: 3, OrderDesc: true},
			want: []hi{{5, 1}, {5, 0}, {4, 1}},
		},
		"equality asc limit 3": {
			q:    `app.name = 'sei'`,
			opts: indexer.SearchOptions{Limit: 3, OrderDesc: false},
			want: []hi{{1, 0}, {1, 1}, {2, 0}},
		},
		// A disabled cap (Limit <= 0) returns the full set in order_by order.
		"equality desc unbounded": {
			q:    `app.name = 'sei'`,
			opts: indexer.SearchOptions{Limit: 0, OrderDesc: true},
			want: []hi{{5, 1}, {5, 0}, {4, 1}, {4, 0}, {3, 1}, {3, 0}, {2, 1}, {2, 0}, {1, 1}, {1, 0}},
		},
		// Fast path: two equalities — driver plus a point-probe. app.kind=even
		// only matches the index-0 tx of each height.
		"two equalities desc limit 2": {
			q:    `app.name = 'sei' AND app.kind = 'even'`,
			opts: indexer.SearchOptions{Limit: 2, OrderDesc: true},
			want: []hi{{5, 0}, {4, 0}},
		},
		// Fast path: equality driver with a tx.height range filter.
		"equality and height range desc limit 2": {
			q:    `app.name = 'sei' AND tx.height >= 4`,
			opts: indexer.SearchOptions{Limit: 2, OrderDesc: true},
			want: []hi{{5, 1}, {5, 0}},
		},
		// Fast path: a tx.height equality drives its own (height, index)-ordered
		// prefix.
		"tx.height equality desc": {
			q:    `tx.height = 3`,
			opts: indexer.SearchOptions{Limit: 5, OrderDesc: true},
			want: []hi{{3, 1}, {3, 0}},
		},
		// Fallback path: a tx.height-range-only query has no equality to drive an
		// in-order scan (the height is stored as a decimal string), so it is
		// materialized, then ordered and capped.
		"height range only desc limit 3": {
			q:    `tx.height >= 4`,
			opts: indexer.SearchOptions{Limit: 3, OrderDesc: true},
			want: []hi{{5, 1}, {5, 0}, {4, 1}},
		},
		// Fallback path: CONTAINS cannot be point-probed, but the result set is
		// still ordered and capped.
		"contains fallback desc limit 3": {
			q:    `app.name CONTAINS 'se'`,
			opts: indexer.SearchOptions{Limit: 3, OrderDesc: true},
			want: []hi{{5, 1}, {5, 0}, {4, 1}},
		},
		"contains fallback asc limit 2": {
			q:    `app.name CONTAINS 'se'`,
			opts: indexer.SearchOptions{Limit: 2, OrderDesc: false},
			want: []hi{{1, 0}, {1, 1}},
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			results, err := idx.Search(t.Context(), query.MustCompile(tc.q), tc.opts)
			require.NoError(t, err)
			require.Equal(t, tc.want, pairs(results))
		})
	}

	// A cancelled context makes the bounded scan return the results gathered so
	// far without an error, rather than failing the whole query.
	t.Run("cancelled context returns partial results", func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		results, err := idx.Search(ctx, query.MustCompile(`app.name = 'sei'`), indexer.SearchOptions{Limit: 5, OrderDesc: true})
		require.NoError(t, err)
		require.Empty(t, results)
	})

	// Capping in the scan must not diverge from materialize-then-cap: for any
	// query the bounded top-N equals the first N of the same query run
	// unbounded. This covers both the fast path (equalities/height ranges) and
	// the fallback (CONTAINS), guarding against future divergence between them.
	t.Run("bounded cap matches unbounded prefix", func(t *testing.T) {
		queries := []string{
			`app.name = 'sei'`,                       // fast path: equality driver
			`app.name = 'sei' AND app.kind = 'even'`, // fast path: equality + probe
			`app.name = 'sei' AND tx.height >= 3`,    // fast path: equality + height range
			`tx.height >= 3`,                         // fallback: height-range-only
			`app.name CONTAINS 'se'`,                 // fallback: materialize then bound
		}
		for _, q := range queries {
			for _, desc := range []bool{true, false} {
				full, err := idx.Search(t.Context(), query.MustCompile(q), indexer.SearchOptions{Limit: 0, OrderDesc: desc})
				require.NoError(t, err)
				for n := 1; n <= len(full); n++ {
					capped, err := idx.Search(t.Context(), query.MustCompile(q), indexer.SearchOptions{Limit: n, OrderDesc: desc})
					require.NoError(t, err)
					require.Equalf(t, pairs(full[:n]), pairs(capped), "query %q desc=%v limit=%d", q, desc, n)
				}
			}
		}
	})
}

func txResultWithEvents(events []abci.Event) *abci.TxResultV2 {
	tx := types.Tx("HELLO WORLD")
	return &abci.TxResultV2{
		Height: 1,
		Index:  0,
		Tx:     tx,
		Result: abci.ExecTxResult{
			Data:   []byte{0},
			Code:   abci.CodeTypeOK,
			Log:    "",
			Events: events,
		},
	}
}

func benchmarkTxIndex(txsCount int64, b *testing.B) {
	dir := b.TempDir()

	store, err := dbm.NewDB("tx_index", "goleveldb", dir)
	require.NoError(b, err)
	txIndexer := NewTxIndex(store)

	batch := indexer.NewBatch(txsCount)
	txIndex := uint32(0)
	for i := int64(0); i < txsCount; i++ {
		tx := tmrand.Bytes(250)
		txResult := &abci.TxResultV2{
			Height: 1,
			Index:  txIndex,
			Tx:     tx,
			Result: abci.ExecTxResult{
				Data:   []byte{0},
				Code:   abci.CodeTypeOK,
				Log:    "",
				Events: []abci.Event{},
			},
		}
		if err := batch.Add(txResult); err != nil {
			b.Fatal(err)
		}
		txIndex++
	}

	b.ResetTimer()

	for n := 0; n < b.N; n++ {
		err = txIndexer.Index(batch.Ops)
	}
	if err != nil {
		b.Fatal(err)
	}
}

func BenchmarkTxIndex1(b *testing.B)     { benchmarkTxIndex(1, b) }
func BenchmarkTxIndex500(b *testing.B)   { benchmarkTxIndex(500, b) }
func BenchmarkTxIndex1000(b *testing.B)  { benchmarkTxIndex(1000, b) }
func BenchmarkTxIndex2000(b *testing.B)  { benchmarkTxIndex(2000, b) }
func BenchmarkTxIndex10000(b *testing.B) { benchmarkTxIndex(10000, b) }
