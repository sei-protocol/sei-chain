package pebblecache

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-db/db_engine/types"
)

func newNoOpTestCache(store map[string][]byte) Cache {
	return NewNoOpCache(func(key []byte) ([]byte, bool, error) {
		v, ok := store[string(key)]
		if !ok {
			return nil, false, nil
		}
		return v, true, nil
	})
}

func TestNoOpGetFound(t *testing.T) {
	c := newNoOpTestCache(map[string][]byte{"k": []byte("v")})

	val, found, err := c.Get([]byte("k"), true)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, "v", string(val))
}

func TestNoOpGetNotFound(t *testing.T) {
	c := newNoOpTestCache(map[string][]byte{})

	val, found, err := c.Get([]byte("missing"), true)
	require.NoError(t, err)
	require.False(t, found)
	require.Nil(t, val)
}

func TestNoOpGetError(t *testing.T) {
	dbErr := errors.New("broken")
	c := NewNoOpCache(func(key []byte) ([]byte, bool, error) {
		return nil, false, dbErr
	})

	_, _, err := c.Get([]byte("k"), true)
	require.ErrorIs(t, err, dbErr)
}

func TestNoOpGetIgnoresUpdateLru(t *testing.T) {
	c := newNoOpTestCache(map[string][]byte{"k": []byte("v")})

	val1, _, _ := c.Get([]byte("k"), true)
	val2, _, _ := c.Get([]byte("k"), false)
	require.Equal(t, string(val1), string(val2))
}

func TestNoOpGetAlwaysReadsFromFunc(t *testing.T) {
	store := map[string][]byte{"k": []byte("v1")}
	c := newNoOpTestCache(store)

	val, _, _ := c.Get([]byte("k"), true)
	require.Equal(t, "v1", string(val))

	store["k"] = []byte("v2")

	val, _, _ = c.Get([]byte("k"), true)
	require.Equal(t, "v2", string(val), "should re-read from func, not cache")
}

func TestNoOpSetIsNoOp(t *testing.T) {
	c := newNoOpTestCache(map[string][]byte{})

	c.Set([]byte("k"), []byte("v"))

	_, found, err := c.Get([]byte("k"), true)
	require.NoError(t, err)
	require.False(t, found, "Set should not cache anything")
}

func TestNoOpDeleteIsNoOp(t *testing.T) {
	c := newNoOpTestCache(map[string][]byte{"k": []byte("v")})

	c.Delete([]byte("k"))

	val, found, err := c.Get([]byte("k"), true)
	require.NoError(t, err)
	require.True(t, found, "Delete should not affect reads")
	require.Equal(t, "v", string(val))
}

func TestNoOpBatchSetIsNoOp(t *testing.T) {
	c := newNoOpTestCache(map[string][]byte{})

	err := c.BatchSet([]CacheUpdate{
		{Key: []byte("a"), Value: []byte("1")},
		{Key: []byte("b"), Value: []byte("2")},
	})
	require.NoError(t, err)

	_, found, _ := c.Get([]byte("a"), true)
	require.False(t, found)
	_, found, _ = c.Get([]byte("b"), true)
	require.False(t, found)
}

func TestNoOpBatchSetEmptyAndNil(t *testing.T) {
	c := newNoOpTestCache(map[string][]byte{})

	require.NoError(t, c.BatchSet(nil))
	require.NoError(t, c.BatchSet([]CacheUpdate{}))
}

func TestNoOpBatchGetAllFound(t *testing.T) {
	c := newNoOpTestCache(map[string][]byte{"a": []byte("1"), "b": []byte("2")})

	keys := map[string]types.BatchGetResult{"a": {}, "b": {}}
	require.NoError(t, c.BatchGet(keys))

	require.True(t, keys["a"].Found)
	require.Equal(t, "1", string(keys["a"].Value))
	require.True(t, keys["b"].Found)
	require.Equal(t, "2", string(keys["b"].Value))
}

func TestNoOpBatchGetNotFound(t *testing.T) {
	c := newNoOpTestCache(map[string][]byte{})

	keys := map[string]types.BatchGetResult{"x": {}}
	require.NoError(t, c.BatchGet(keys))
	require.False(t, keys["x"].Found)
}

func TestNoOpBatchGetError(t *testing.T) {
	dbErr := errors.New("fail")
	c := NewNoOpCache(func(key []byte) ([]byte, bool, error) {
		return nil, false, dbErr
	})

	keys := map[string]types.BatchGetResult{"k": {}}
	require.NoError(t, c.BatchGet(keys))
	require.Error(t, keys["k"].Error)
}

func TestNoOpBatchGetEmpty(t *testing.T) {
	c := newNoOpTestCache(map[string][]byte{})

	keys := map[string]types.BatchGetResult{}
	require.NoError(t, c.BatchGet(keys))
}
