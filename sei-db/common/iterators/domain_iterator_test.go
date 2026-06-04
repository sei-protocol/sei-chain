package iterators_test

import (
	"testing"

	"github.com/sei-protocol/sei-chain/sei-db/common/iterators"
	"github.com/stretchr/testify/require"
)

func TestNewDomainIterator_NilParent(t *testing.T) {
	it, err := iterators.NewDomainIterator(nil, []byte("a"), []byte("z"))
	require.Error(t, err)
	require.Nil(t, it)
}

func TestNewDomainIterator_OverridesDomain(t *testing.T) {
	data := map[string][]byte{
		"b": []byte("vb"),
		"a": []byte("va"),
		"c": []byte("vc"),
	}
	parent, err := iterators.NewMapIterator(nil, nil, true, iterators.BytesSerializer, data)
	require.NoError(t, err)

	// Sanity check: the parent reports nil bounds before wrapping.
	pStart, pEnd := parent.Domain()
	require.Nil(t, pStart)
	require.Nil(t, pEnd)

	start, end := []byte("a"), []byte("d")
	it, err := iterators.NewDomainIterator(parent, start, end)
	require.NoError(t, err)
	defer it.Close()

	gotStart, gotEnd := it.Domain()
	require.Equal(t, start, gotStart)
	require.Equal(t, end, gotEnd)
}

func TestNewDomainIterator_DelegatesIteration(t *testing.T) {
	data := map[string][]byte{
		"a": []byte("va"),
		"b": []byte("vb"),
		"c": []byte("vc"),
	}
	parent, err := iterators.NewMapIterator(nil, nil, true, iterators.BytesSerializer, data)
	require.NoError(t, err)

	it, err := iterators.NewDomainIterator(parent, []byte("a"), []byte("d"))
	require.NoError(t, err)
	defer it.Close()

	var got [][2][]byte
	for ; it.Valid(); it.Next() {
		got = append(got, [2][]byte{it.Key(), it.Value()})
	}
	require.NoError(t, it.Error())
	require.Equal(t, [][2][]byte{
		{[]byte("a"), []byte("va")},
		{[]byte("b"), []byte("vb")},
		{[]byte("c"), []byte("vc")},
	}, got)
}
