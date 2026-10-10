package api

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-wasmvm/types"
)

// reuseIterator serves Key and Value from one buffer per field. Next overwrites
// that buffer, which is the FlatKV iterator contract.
type reuseIterator struct {
	keys   [][]byte
	values [][]byte
	idx    int
	keyBuf []byte
	valBuf []byte
}

var _ types.Iterator = (*reuseIterator)(nil)

func (it *reuseIterator) Domain() (start []byte, end []byte) { return nil, nil }

func (it *reuseIterator) Valid() bool { return it.idx < len(it.keys) }

func (it *reuseIterator) Next() {
	if !it.Valid() {
		return
	}
	it.idx++
	if !it.Valid() {
		return
	}
	it.keyBuf = fillReuseBuf(it.keyBuf, it.keys[it.idx])
	it.valBuf = fillReuseBuf(it.valBuf, it.values[it.idx])
}

func (it *reuseIterator) Key() []byte {
	if !it.Valid() {
		return nil
	}
	it.keyBuf = fillReuseBuf(it.keyBuf, it.keys[it.idx])
	if it.keys[it.idx] == nil {
		return nil
	}
	return it.keyBuf
}

func (it *reuseIterator) Value() []byte {
	if !it.Valid() {
		return nil
	}
	it.valBuf = fillReuseBuf(it.valBuf, it.values[it.idx])
	if it.values[it.idx] == nil {
		return nil
	}
	return it.valBuf
}

func (it *reuseIterator) Error() error { return nil }

func (it *reuseIterator) Close() error { return nil }

// fillReuseBuf copies src into buf, reusing buf's array when it fits.
// A nil src is left untouched. An empty non-nil src stays non-nil, including
// when buf has not been allocated yet.
func fillReuseBuf(buf, src []byte) []byte {
	if src == nil {
		return buf
	}
	if len(src) == 0 {
		if buf == nil {
			return []byte{}
		}
		return buf[:0]
	}
	return append(buf[:0], src...)
}

func TestCopyCurrentThenAdvanceReusedBuffer(t *testing.T) {
	// The second key is shorter, so an in-place overwrite leaves the tail of
	// the first key in the buffer. A copy taken after Next would see that mix.
	it := &reuseIterator{
		keys:   [][]byte{[]byte("abcdef"), []byte("zz")},
		values: [][]byte{[]byte("value-1"), []byte("v2")},
	}

	copied := copyCurrentThenAdvance(it, iteratorKey, iteratorValue)
	require.Equal(t, []byte("abcdef"), copied[0])
	require.Equal(t, []byte("value-1"), copied[1])
	require.Equal(t, 1, it.idx)

	// The buffer now holds the next item. The slices already returned do not.
	require.Equal(t, []byte("zz"), it.Key())
	require.Equal(t, []byte("v2"), it.Value())
	require.Equal(t, []byte("abcdef"), copied[0])
	require.Equal(t, []byte("value-1"), copied[1])
}

func TestCopyCurrentThenAdvanceOneField(t *testing.T) {
	it := &reuseIterator{
		keys:   [][]byte{[]byte("key-1"), []byte("key-2")},
		values: [][]byte{[]byte("value-1"), []byte("value-2")},
	}

	copied := copyCurrentThenAdvance(it, iteratorKey)
	require.Equal(t, [][]byte{[]byte("key-1")}, copied)
	require.Equal(t, []byte("key-2"), it.Key())
	require.Equal(t, []byte("key-1"), copied[0])
}

func TestCopyCurrentThenAdvancePreservesNilAndEmpty(t *testing.T) {
	it := &reuseIterator{
		keys:   [][]byte{nil, {}},
		values: [][]byte{{}, nil},
	}

	first := copyCurrentThenAdvance(it, iteratorKey, iteratorValue)
	require.Nil(t, first[0])
	require.Empty(t, first[1])
	require.NotNil(t, first[1])

	second := copyCurrentThenAdvance(it, iteratorKey, iteratorValue)
	require.Empty(t, second[0])
	require.NotNil(t, second[0])
	require.Nil(t, second[1])
}
