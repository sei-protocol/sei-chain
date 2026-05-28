package iterators_test

import (
	"bytes"
	"errors"
	"testing"

	"github.com/sei-protocol/sei-chain/sei-db/common/iterators"
	"github.com/stretchr/testify/require"
	dbm "github.com/tendermint/tm-db"
)

var errRemap = errors.New("remap failed")

func TestMappingIterator_SkipsKeys(t *testing.T) {
	parent := memIter(t, []byte("a"), []byte("b"), []byte("c"))
	mapIter, err := iterators.NewMappingIterator(parent, func(key, value []byte) ([]byte, []byte, bool, error) {
		if key[0] == 'b' {
			return nil, nil, true, nil
		}
		return key, value, false, nil
	})
	require.NoError(t, err)

	got := collect(t, mapIter)
	require.Equal(t, [][2][]byte{
		{[]byte("a"), []byte("a")},
		{[]byte("c"), []byte("c")},
	}, got)
}

func TestMappingIterator_RemapsKeyValue(t *testing.T) {
	parent := memIter(t, []byte("k"))
	mapIter, err := iterators.NewMappingIterator(parent, func(key, value []byte) ([]byte, []byte, bool, error) {
		return append([]byte("x"), key...), append([]byte("y"), value...), false, nil
	})
	require.NoError(t, err)

	require.True(t, mapIter.Valid())
	require.Equal(t, []byte("xk"), mapIter.Key())
	require.Equal(t, []byte("ya"), mapIter.Value())
	require.NoError(t, mapIter.Close())
}

func TestMappingIterator_RemapperError(t *testing.T) {
	parent := memIter(t, []byte("a"), []byte("b"))
	mapIter, err := iterators.NewMappingIterator(parent, func(key, _ []byte) ([]byte, []byte, bool, error) {
		if key[0] == 'b' {
			return nil, nil, false, errRemap
		}
		return key, key, false, nil
	})
	require.NoError(t, err)

	require.True(t, mapIter.Valid())
	require.Equal(t, []byte("a"), mapIter.Key())
	mapIter.Next()
	require.False(t, mapIter.Valid())
	require.ErrorIs(t, mapIter.Error(), errRemap)
}

func TestMappingIterator_EmptyParent(t *testing.T) {
	parent := memIter(t)
	mapIter, err := iterators.NewMappingIterator(parent, func(key, value []byte) ([]byte, []byte, bool, error) {
		return key, value, false, nil
	})
	require.NoError(t, err)
	require.False(t, mapIter.Valid())
	require.NoError(t, mapIter.Error())
}

var errSkipNext = errors.New("skip next failed")

// invalidAfterFirstNextIterator becomes invalid with a sticky error after the first
// Next(), matching pebble/tm-db behavior when Next hits an I/O failure.
type invalidAfterFirstNextIterator struct {
	dbm.Iterator
	didNext bool
}

func (child *invalidAfterFirstNextIterator) Next() {
	child.didNext = true
	child.Iterator.Next()
}

func (child *invalidAfterFirstNextIterator) Valid() bool {
	if child.didNext {
		return false
	}
	return child.Iterator.Valid()
}

func (child *invalidAfterFirstNextIterator) Error() error {
	if child.didNext {
		return errSkipNext
	}
	return child.Iterator.Error()
}

func TestMappingIterator_ParentErrorAfterSkipNext(t *testing.T) {
	// Keys must sort with the skipped key first (memDB iterates in lex order).
	parent := &invalidAfterFirstNextIterator{Iterator: memIter(t, []byte("_meta"), []byte("user"))}
	mapIter, err := iterators.NewMappingIterator(parent, func(key, value []byte) ([]byte, []byte, bool, error) {
		return key, value, bytes.HasPrefix(key, []byte("_meta")), nil
	})
	require.NoError(t, err)

	require.False(t, mapIter.Valid())
	require.ErrorIs(t, mapIter.Error(), errSkipNext)
}

func TestNewMappingIterator_NilParent(t *testing.T) {
	_, err := iterators.NewMappingIterator(nil, func([]byte, []byte) ([]byte, []byte, bool, error) {
		return nil, nil, false, nil
	})
	require.Error(t, err)
}

func TestNewMappingIterator_NilRemapper(t *testing.T) {
	parent := memIter(t, []byte("k"))
	_, err := iterators.NewMappingIterator(parent, nil)
	require.Error(t, err)
}

var errConstruction = errors.New("open failed")

// errAtConstructionIterator reports a sticky error from construction.
type errAtConstructionIterator struct {
	dbm.Iterator
}

func (child *errAtConstructionIterator) Error() error {
	return errConstruction
}

func TestNewMappingIterator_ParentError(t *testing.T) {
	parent := &errAtConstructionIterator{Iterator: memIter(t, []byte("k"))}
	_, err := iterators.NewMappingIterator(parent, func(key, value []byte) ([]byte, []byte, bool, error) {
		return key, value, false, nil
	})
	require.ErrorIs(t, err, errConstruction)
}
