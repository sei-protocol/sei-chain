package memiavl

import (
	"testing"

	"github.com/stretchr/testify/require"
	dbm "github.com/tendermint/tm-db"
)

func TestIterator(t *testing.T) {
	tree := New(0)
	require.Equal(t, ExpectItems[0], collectIter(tree.Iterator(nil, nil, true)))

	for _, changes := range ChangeSets {
		tree.ApplyChangeSet(changes)
		_, v, err := tree.SaveVersion(true)
		require.NoError(t, err)
		require.Equal(t, ExpectItems[v], collectIter(tree.Iterator(nil, nil, true)))
		require.Equal(t, reverse(ExpectItems[v]), collectIter(tree.Iterator(nil, nil, false)))
	}
}

func TestIteratorRange(t *testing.T) {
	tree := New(0)
	for _, changes := range ChangeSets[:6] {
		tree.ApplyChangeSet(changes)
		_, _, err := tree.SaveVersion(true)
		require.NoError(t, err)
	}

	expItems := []pair{
		{[]byte("aello05"), []byte("world1")},
		{[]byte("aello06"), []byte("world1")},
		{[]byte("aello07"), []byte("world1")},
		{[]byte("aello08"), []byte("world1")},
		{[]byte("aello09"), []byte("world1")},
	}
	require.Equal(t, expItems, collectIter(tree.Iterator([]byte("aello05"), []byte("aello10"), true)))
	require.Equal(t, reverse(expItems), collectIter(tree.Iterator([]byte("aello05"), []byte("aello10"), false)))
}

type pair struct {
	key, value []byte
}

func collectIter(iter dbm.Iterator) []pair {
	result := []pair{}
	for ; iter.Valid(); iter.Next() {
		result = append(result, pair{key: iter.Key(), value: iter.Value()})
	}
	return result
}

func reverse[S ~[]E, E any](s S) S {
	r := make(S, len(s))
	for i, j := 0, len(s)-1; i <= j; i, j = i+1, j-1 {
		r[i], r[j] = s[j], s[i]
	}
	return r
}
