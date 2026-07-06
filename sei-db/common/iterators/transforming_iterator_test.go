package iterators_test

import (
	"bytes"
	"errors"
	"testing"

	"github.com/sei-protocol/sei-chain/sei-db/common/iterators"
	"github.com/stretchr/testify/require"
	dbm "github.com/tendermint/tm-db"
)

var errTransform = errors.New("transform failed")

func TestTransformingIterator_SkipsKeys(t *testing.T) {
	parent := memIter(t, []byte("a"), []byte("b"), []byte("c"))
	transformIter, err := iterators.NewTransformingIterator(parent, func(key, value []byte) ([]byte, []byte, bool, error) {
		if key[0] == 'b' {
			return nil, nil, true, nil
		}
		return key, value, false, nil
	})
	require.NoError(t, err)

	got := collect(t, transformIter)
	require.Equal(t, [][2][]byte{
		{[]byte("a"), []byte("a")},
		{[]byte("c"), []byte("c")},
	}, got)
}

func TestTransformingIterator_TransformsKeyValue(t *testing.T) {
	parent := memIter(t, []byte("k"))
	transformIter, err := iterators.NewTransformingIterator(parent, func(key, value []byte) ([]byte, []byte, bool, error) {
		return append([]byte("x"), key...), append([]byte("y"), value...), false, nil
	})
	require.NoError(t, err)

	require.True(t, transformIter.Valid())
	require.Equal(t, []byte("xk"), transformIter.Key())
	require.Equal(t, []byte("ya"), transformIter.Value())
	require.NoError(t, transformIter.Close())
}

func TestTransformingIterator_TransformError(t *testing.T) {
	parent := memIter(t, []byte("a"), []byte("b"))
	transformIter, err := iterators.NewTransformingIterator(parent, func(key, _ []byte) ([]byte, []byte, bool, error) {
		if key[0] == 'b' {
			return nil, nil, false, errTransform
		}
		return key, key, false, nil
	})
	require.NoError(t, err)

	require.True(t, transformIter.Valid())
	require.Equal(t, []byte("a"), transformIter.Key())
	transformIter.Next()
	require.False(t, transformIter.Valid())
	require.ErrorIs(t, transformIter.Error(), errTransform)
}

func TestTransformingIterator_EmptyParent(t *testing.T) {
	parent := memIter(t)
	transformIter, err := iterators.NewTransformingIterator(parent, func(key, value []byte) ([]byte, []byte, bool, error) {
		return key, value, false, nil
	})
	require.NoError(t, err)
	require.False(t, transformIter.Valid())
	require.NoError(t, transformIter.Error())
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

func TestNewTransformingIterator_ParentErrorAfterSkipNext(t *testing.T) {
	// Keys must sort with the skipped key first (memDB iterates in lex order).
	parent := &invalidAfterFirstNextIterator{Iterator: memIter(t, []byte("_meta"), []byte("user"))}
	_, err := iterators.NewTransformingIterator(parent, func(key, value []byte) ([]byte, []byte, bool, error) {
		return key, value, bytes.HasPrefix(key, []byte("_meta")), nil
	})
	require.ErrorIs(t, err, errSkipNext)
}

func TestNewTransformingIterator_NilParent(t *testing.T) {
	_, err := iterators.NewTransformingIterator(nil, func([]byte, []byte) ([]byte, []byte, bool, error) {
		return nil, nil, false, nil
	})
	require.Error(t, err)
}

func TestNewTransformingIterator_NilTransform(t *testing.T) {
	parent := memIter(t, []byte("k"))
	_, err := iterators.NewTransformingIterator(parent, nil)
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

func TestNewTransformingIterator_ParentError(t *testing.T) {
	parent := &errAtConstructionIterator{Iterator: memIter(t, []byte("k"))}
	_, err := iterators.NewTransformingIterator(parent, func(key, value []byte) ([]byte, []byte, bool, error) {
		return key, value, false, nil
	})
	require.ErrorIs(t, err, errConstruction)
}
