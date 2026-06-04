package iterators_test

import (
	"testing"

	"github.com/sei-protocol/sei-chain/sei-db/common/iterators"
	"github.com/stretchr/testify/require"
	dbm "github.com/tendermint/tm-db"
)

func TestNewMapIterator_Empty(t *testing.T) {
	it, err := iterators.NewMapIterator(nil, nil, true, iterators.BytesSerializer)
	require.NoError(t, err)
	require.False(t, it.Valid())
	require.Nil(t, it.Key())
	require.Nil(t, it.Value())
	require.NoError(t, it.Error())
	start, end := it.Domain()
	require.Nil(t, start)
	require.Nil(t, end)
	require.NoError(t, it.Close())
	require.False(t, it.Valid())
}

func TestNewMapIterator_Ascending(t *testing.T) {
	data := map[string][]byte{
		"c": []byte("vc"),
		"a": []byte("va"),
		"b": []byte("vb"),
	}
	it, err := iterators.NewMapIterator(nil, nil, true, iterators.BytesSerializer, data)
	require.NoError(t, err)
	defer it.Close()

	got := collectMapIterPairs(t, it)
	require.Equal(t, [][2][]byte{
		{[]byte("a"), []byte("va")},
		{[]byte("b"), []byte("vb")},
		{[]byte("c"), []byte("vc")},
	}, got)
}

func TestNewMapIterator_Descending(t *testing.T) {
	data := map[string][]byte{
		"c": []byte("vc"),
		"a": []byte("va"),
		"b": []byte("vb"),
	}
	it, err := iterators.NewMapIterator(nil, nil, false, iterators.BytesSerializer, data)
	require.NoError(t, err)
	defer it.Close()

	got := collectMapIterPairs(t, it)
	require.Equal(t, [][2][]byte{
		{[]byte("c"), []byte("vc")},
		{[]byte("b"), []byte("vb")},
		{[]byte("a"), []byte("va")},
	}, got)
}

func TestNewMapIterator_CombinesMaps(t *testing.T) {
	left := map[string][]byte{"a": []byte("1"), "c": []byte("3")}
	right := map[string][]byte{"b": []byte("2")}
	it, err := iterators.NewMapIterator(nil, nil, true, iterators.BytesSerializer, left, right)
	require.NoError(t, err)
	defer it.Close()

	got := collectMapIterPairs(t, it)
	require.Equal(t, [][2][]byte{
		{[]byte("a"), []byte("1")},
		{[]byte("b"), []byte("2")},
		{[]byte("c"), []byte("3")},
	}, got)
}

func TestNewMapIterator_DuplicateKey(t *testing.T) {
	left := map[string][]byte{"k": []byte("v0")}
	right := map[string][]byte{"k": []byte("v1")}
	_, err := iterators.NewMapIterator(nil, nil, true, iterators.BytesSerializer, left, right)
	require.Error(t, err)
	require.Contains(t, err.Error(), `duplicate key "k"`)
}

func TestNewMapIterator_Domain(t *testing.T) {
	data := map[string][]byte{
		"a": []byte("1"),
		"b": []byte("2"),
		"c": []byte("3"),
		"d": []byte("4"),
	}
	start := []byte("b")
	end := []byte("d")
	it, err := iterators.NewMapIterator(start, end, true, iterators.BytesSerializer, data)
	require.NoError(t, err)
	defer it.Close()

	got := collectMapIterPairs(t, it)
	require.Equal(t, [][2][]byte{
		{[]byte("b"), []byte("2")},
		{[]byte("c"), []byte("3")},
	}, got)

	domainStart, domainEnd := it.Domain()
	require.Equal(t, start, domainStart)
	require.Equal(t, end, domainEnd)
}

func TestNewMapIterator_StartInclusiveEndExclusive(t *testing.T) {
	data := map[string][]byte{
		"k1": []byte("v1"),
		"k2": []byte("v2"),
	}
	it, err := iterators.NewMapIterator([]byte("k1"), []byte("k1"), true, iterators.BytesSerializer, data)
	require.NoError(t, err)
	require.False(t, it.Valid())
	require.NoError(t, it.Close())

	it, err = iterators.NewMapIterator([]byte("k1"), []byte("k2"), true, iterators.BytesSerializer, data)
	require.NoError(t, err)
	got := collectMapIterPairs(t, it)
	require.Equal(t, [][2][]byte{{[]byte("k1"), []byte("v1")}}, got)
}

func TestNewMapIterator_InvalidRange(t *testing.T) {
	data := map[string][]byte{"a": []byte("1")}
	it, err := iterators.NewMapIterator([]byte("z"), []byte("a"), true, iterators.BytesSerializer, data)
	require.NoError(t, err)
	require.False(t, it.Valid())
	require.NoError(t, it.Close())
}

func TestNewMapIterator_IsolatedFromMapMutations(t *testing.T) {
	data := map[string][]byte{"k": []byte("v")}
	it, err := iterators.NewMapIterator(nil, nil, true, iterators.BytesSerializer, data)
	require.NoError(t, err)
	require.True(t, it.Valid())

	data["k"] = []byte("mutated")
	delete(data, "k")

	require.Equal(t, []byte("k"), it.Key())
	require.Equal(t, []byte("v"), it.Value())
	require.NoError(t, it.Close())
}

func TestMapIterator_NextAfterExhausted(t *testing.T) {
	it, err := iterators.NewMapIterator(nil, nil, true, iterators.BytesSerializer, map[string][]byte{"a": []byte("1")})
	require.NoError(t, err)
	require.True(t, it.Valid())
	it.Next()
	require.False(t, it.Valid())
	require.Nil(t, it.Key())
	require.Nil(t, it.Value())
	it.Next() // no-op
	require.False(t, it.Valid())
	require.NoError(t, it.Close())
}

func collectMapIterPairs(t *testing.T, it dbm.Iterator) [][2][]byte {
	t.Helper()
	var got [][2][]byte
	for ; it.Valid(); it.Next() {
		got = append(got, [2][]byte{it.Key(), it.Value()})
	}
	require.NoError(t, it.Error())
	return got
}
