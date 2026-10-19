package flatkv

import (
	"context"
	"encoding/binary"
	"testing"

	"github.com/sei-protocol/sei-chain/sei-db/common/iterators"
	"github.com/sei-protocol/sei-chain/sei-db/common/keys"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/ktype"
	"github.com/stretchr/testify/require"
)

// setupNonceOnlyAccounts returns a store holding n committed accounts that carry a nonce and nothing
// else: every one is a physical account row that the code-hash and balance lanes read and skip.
func setupNonceOnlyAccounts(t *testing.T, n int) *CommitStore {
	t.Helper()
	s := setupTestStore(t)
	pairs := make([]*proto.KVPair, 0, n)
	for i := 0; i < n; i++ {
		var addr ktype.Address
		binary.BigEndian.PutUint32(addr[16:], uint32(i)) //nolint:gosec // small test index
		pairs = append(pairs, noncePair(addr, 1))
	}
	require.NoError(t, s.ApplyChangeSets(s.Version()+1, []*proto.NamedChangeSet{namedCS(pairs...)}))
	commitAndCheck(t, s)
	return s
}

// evmPrefixRange returns the logical [start, end) range covering one EVM key kind.
func evmPrefixRange(t *testing.T, kind keys.EVMKeyKind) ([]byte, []byte) {
	t.Helper()
	prefix, ok := keys.EVMKeyPrefixByte(kind)
	require.True(t, ok)
	return []byte{prefix}, []byte{prefix + 1}
}

// TestBudgetedIterator_ChargesRowsTheProjectedLanesSkip pins that a code-hash or balance scan over
// accounts whose field is zero is charged for every account row it reads, though it yields none, and
// that the charge stops the scan while the iterator is still being built.
func TestBudgetedIterator_ChargesRowsTheProjectedLanesSkip(t *testing.T) {
	const accounts = 64
	s := setupNonceOnlyAccounts(t, accounts)
	defer func() { require.NoError(t, s.Close()) }()

	for _, kind := range []keys.EVMKeyKind{keys.EVMKeyCodeHash, keys.EVMKeyBalance} {
		start, end := evmPrefixRange(t, kind)

		unbounded, err := s.Iterator(keys.EVMStoreKey, start, end, true)
		require.NoError(t, err)
		require.Empty(t, collectIterEntries(t, unbounded))
		require.NoError(t, unbounded.Close())

		exact, err := s.BudgetedIterator(iterators.NewScanBudget(t.Context(), accounts), keys.EVMStoreKey, start, end, true)
		require.NoError(t, err, "kind %v: a budget of one row per account must cover the scan", kind)
		require.Empty(t, collectIterEntries(t, exact))
		require.NoError(t, exact.Close())

		_, err = s.BudgetedIterator(iterators.NewScanBudget(t.Context(), accounts-1), keys.EVMStoreKey, start, end, true)
		require.ErrorIs(t, err, iterators.ErrScanBudgetExceeded, "kind %v", kind)

		_, err = s.BudgetedIterator(iterators.NewScanBudget(t.Context(), accounts-1), keys.EVMStoreKey, start, end, false)
		require.ErrorIs(t, err, iterators.ErrScanBudgetExceeded, "kind %v, descending", kind)
	}
}

// TestBudgetedIterator_ChargesRowsItYields pins that rows a lane yields are charged as well, so a scan
// that yields every row it reads ends mid-iteration when the budget runs out.
func TestBudgetedIterator_ChargesRowsItYields(t *testing.T) {
	const accounts = 64
	s := setupNonceOnlyAccounts(t, accounts)
	defer func() { require.NoError(t, s.Close()) }()

	start, end := evmPrefixRange(t, keys.EVMKeyNonce)
	iter, err := s.BudgetedIterator(iterators.NewScanBudget(t.Context(), 10), keys.EVMStoreKey, start, end, true)
	require.NoError(t, err)
	yielded := 0
	for ; iter.Valid(); iter.Next() {
		yielded++
	}
	require.Equal(t, 10, yielded)
	require.ErrorIs(t, iter.Error(), iterators.ErrScanBudgetExceeded)
	require.NoError(t, iter.Close())
}

// TestBudgetedIterator_StopsSkippingWhenTheContextEnds pins that a cancelled scan stops inside the
// skip loop rather than reading the rest of the account rows.
func TestBudgetedIterator_StopsSkippingWhenTheContextEnds(t *testing.T) {
	s := setupNonceOnlyAccounts(t, 16)
	defer func() { require.NoError(t, s.Close()) }()

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	start, end := evmPrefixRange(t, keys.EVMKeyBalance)
	_, err := s.BudgetedIterator(iterators.NewScanBudget(ctx, 1_000), keys.EVMStoreKey, start, end, true)
	require.ErrorIs(t, err, context.Canceled)
}
