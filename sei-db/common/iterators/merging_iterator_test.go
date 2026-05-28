package iterators_test

import (
	"errors"
	"testing"

	"github.com/sei-protocol/sei-chain/sei-db/common/iterators"
	"github.com/stretchr/testify/require"
	dbm "github.com/tendermint/tm-db"
)

var errChild = errors.New("child failed")

func memIter(t *testing.T, keys ...[]byte) dbm.Iterator {
	t.Helper()
	db := dbm.NewMemDB()
	for i, k := range keys {
		require.NoError(t, db.Set(k, []byte{byte('a' + i)}))
	}
	it, err := db.Iterator(nil, nil)
	require.NoError(t, err)
	return it
}

func memIterKV(t *testing.T, pairs ...[2][]byte) dbm.Iterator {
	t.Helper()
	db := dbm.NewMemDB()
	for _, pair := range pairs {
		require.NoError(t, db.Set(pair[0], pair[1]))
	}
	it, err := db.Iterator(nil, nil)
	require.NoError(t, err)
	return it
}

func collect(t *testing.T, it dbm.Iterator) [][2][]byte {
	t.Helper()
	var out [][2][]byte
	for ; it.Valid(); it.Next() {
		out = append(out, [2][]byte{it.Key(), it.Value()})
	}
	require.NoError(t, it.Error())
	return out
}

func TestNewMergingIterator_NilIterator(t *testing.T) {
	_, err := iterators.NewMergingIterator(memIter(t, []byte("a")), nil)
	require.Error(t, err)
}

func TestNewMergingIterator_Empty(t *testing.T) {
	it, err := iterators.NewMergingIterator()
	require.NoError(t, err)
	require.False(t, it.Valid())
	require.Nil(t, it.Key())
	require.Nil(t, it.Value())
	require.NoError(t, it.Close())
}

func TestMergingIterator_Single(t *testing.T) {
	child := memIter(t, []byte("b"), []byte("c"))
	it, err := iterators.NewMergingIterator(child)
	require.NoError(t, err)
	defer it.Close()

	got := collect(t, it)
	require.Equal(t, [][2][]byte{
		{[]byte("b"), []byte("a")},
		{[]byte("c"), []byte("b")},
	}, got)
}

func TestMergingIterator_LexOrder(t *testing.T) {
	a := memIter(t, []byte("a"), []byte("d"))
	b := memIter(t, []byte("b"), []byte("c"), []byte("e"))
	it, err := iterators.NewMergingIterator(a, b)
	require.NoError(t, err)
	defer it.Close()

	keys := make([][]byte, 0, 5)
	for ; it.Valid(); it.Next() {
		keys = append(keys, it.Key())
	}
	require.Equal(t, [][]byte{
		[]byte("a"), []byte("b"), []byte("c"), []byte("d"), []byte("e"),
	}, keys)
}

func TestMergingIterator_DuplicateKeys(t *testing.T) {
	left := memIterKV(t, [2][]byte{[]byte("k"), []byte("v0")}, [2][]byte{[]byte("z"), []byte("z0")})
	right := memIterKV(t, [2][]byte{[]byte("k"), []byte("v1")}, [2][]byte{[]byte("m"), []byte("m1")})
	it, err := iterators.NewMergingIterator(left, right)
	require.NoError(t, err)
	defer it.Close()

	got := collect(t, it)
	require.Equal(t, [][2][]byte{
		{[]byte("k"), []byte("v1")},
		{[]byte("m"), []byte("m1")},
		{[]byte("z"), []byte("z0")},
	}, got)
}

func TestMergingIterator_RightmostWinsOnDuplicateKey(t *testing.T) {
	child0 := memIterKV(t, [2][]byte{[]byte("k"), []byte("v0")}, [2][]byte{[]byte("a"), []byte("a0")})
	child1 := memIter(t, []byte("b"))
	child2 := memIterKV(t, [2][]byte{[]byte("k"), []byte("v2")}, [2][]byte{[]byte("c"), []byte("c0")})
	it, err := iterators.NewMergingIterator(child0, child1, child2)
	require.NoError(t, err)
	defer it.Close()

	got := collect(t, it)
	require.Equal(t, [][2][]byte{
		{[]byte("a"), []byte("a0")},
		{[]byte("b"), []byte("a")},
		{[]byte("c"), []byte("c0")},
		{[]byte("k"), []byte("v2")},
	}, got)
}

func TestMergingIterator_Domain(t *testing.T) {
	db := dbm.NewMemDB()
	it1, err := db.Iterator([]byte("b"), []byte("f"))
	require.NoError(t, err)
	it2, err := db.Iterator([]byte("a"), nil)
	require.NoError(t, err)

	merged, err := iterators.NewMergingIterator(it1, it2)
	require.NoError(t, err)
	defer merged.Close()

	start, end := merged.Domain()
	require.Equal(t, []byte("a"), start)
	require.Nil(t, end)
}

type closeTrackingIterator struct {
	dbm.Iterator
	closed bool
}

func (c *closeTrackingIterator) Close() error {
	c.closed = true
	return c.Iterator.Close()
}

type errOnSecondNextIterator struct {
	dbm.Iterator
	nextCount int
	closed    bool
}

func (child *errOnSecondNextIterator) Next() {
	child.nextCount++
	child.Iterator.Next()
}

func (child *errOnSecondNextIterator) Error() error {
	if child.nextCount >= 2 {
		return errChild
	}
	return child.Iterator.Error()
}

func (child *errOnSecondNextIterator) Close() error {
	child.closed = true
	return child.Iterator.Close()
}

func TestMergingIterator_CachesChildError(t *testing.T) {
	ok := memIter(t, []byte("a"), []byte("c"))
	bad := &errOnSecondNextIterator{Iterator: memIter(t, []byte("b"), []byte("d"))}
	merged, err := iterators.NewMergingIterator(ok, bad)
	require.NoError(t, err)

	require.True(t, merged.Valid())
	merged.Next() // emit "a"
	require.True(t, merged.Valid())
	merged.Next() // emit "b", advances bad once
	require.True(t, merged.Valid())
	merged.Next() // emit "c", advances ok
	require.True(t, merged.Valid())
	merged.Next() // emit "d", advances bad again -> error

	require.False(t, merged.Valid())
	require.ErrorIs(t, merged.Error(), errChild)
	require.Nil(t, merged.Key())
	require.Nil(t, merged.Value())
	merged.Next() // no-op after failure

	require.True(t, bad.closed)
	require.NoError(t, merged.Close())
}

func TestMergingIterator_ClosesChildren(t *testing.T) {
	child := &closeTrackingIterator{Iterator: memIter(t, []byte("x"))}
	it, err := iterators.NewMergingIterator(child)
	require.NoError(t, err)
	require.NoError(t, it.Close())
	require.True(t, child.closed)
}
