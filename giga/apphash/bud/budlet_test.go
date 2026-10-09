package bud

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math/rand"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"
)

// randomBudlets returns count budlets with distinct random keys, sorted by key.
func randomBudlets(t *testing.T, rng *rand.Rand, count int) []*Budlet {
	t.Helper()

	seen := make(map[string]bool, count)
	budlets := make([]*Budlet, 0, count)
	for len(budlets) < count {
		key := make([]byte, 1+rng.Intn(16))
		_, _ = rng.Read(key)
		if seen[string(key)] {
			continue
		}
		seen[string(key)] = true

		notModifiedSince := rng.Uint64() >> rng.Intn(64)
		budlet, err := NewBudlet(key, randomBudletValue(rng), randomBudletValue(rng), notModifiedSince)
		require.NoError(t, err)
		budlets = append(budlets, budlet)
	}
	slices.SortFunc(budlets, func(a *Budlet, b *Budlet) int {
		return bytes.Compare(a.Key(), b.Key())
	})
	return budlets
}

// randomBudletValue returns nil, standing for a deletion or an absent key, one time in four, and otherwise a non-nil
// value of 0 to 32 random bytes.
func randomBudletValue(rng *rand.Rand) []byte {
	if rng.Intn(4) == 0 {
		return nil
	}
	value := make([]byte, rng.Intn(33))
	_, _ = rng.Read(value)
	return value
}

// newTestBudlet returns NewBudlet(), failing the test on an error.
func newTestBudlet(t *testing.T, key string, value []byte, previousValue []byte, notModifiedSince uint64) *Budlet {
	t.Helper()

	budlet, err := NewBudlet([]byte(key), value, previousValue, notModifiedSince)
	require.NoError(t, err)
	return budlet
}

// newTestDeletionBudlet returns the budlet deleting key, failing the test on an error.
func newTestDeletionBudlet(t *testing.T, key string, previousValue []byte, notModifiedSince uint64) *Budlet {
	t.Helper()

	budlet, err := NewBudlet([]byte(key), nil, previousValue, notModifiedSince)
	require.NoError(t, err)
	return budlet
}

// threeBudlets returns a small block of budlets, sorted by key.
func threeBudlets(t *testing.T) []*Budlet {
	t.Helper()

	return []*Budlet{
		newTestBudlet(t, "evm/a", []byte{0x01}, []byte{0x09}, 3),
		newTestBudlet(t, "evm/b", []byte{0x02, 0x03}, nil, 0),
		newTestDeletionBudlet(t, "evm/c", []byte{0x04}, 9),
	}
}

func TestNewBudletRejectsEmptyKey(t *testing.T) {
	_, err := NewBudlet(nil, []byte{0x01}, nil, 0)
	require.Error(t, err)
	_, err = NewBudlet([]byte{}, nil, nil, 0)
	require.Error(t, err)
}

func TestBudletDistinguishesDeletionFromEmptyWrite(t *testing.T) {
	deletion := newTestDeletionBudlet(t, "evm/a", []byte{0x01}, 3)
	emptyWrite := newTestBudlet(t, "evm/a", []byte{}, []byte{0x01}, 3)
	require.Nil(t, deletion.Value())
	require.NotNil(t, emptyWrite.Value())
	requireBudletsDifferAndRoundTrip(t, deletion, emptyWrite)
}

func TestBudletDistinguishesAbsentFromEmptyPreviousValue(t *testing.T) {
	absent := newTestBudlet(t, "evm/a", []byte{0x01}, nil, 3)
	empty := newTestBudlet(t, "evm/a", []byte{0x01}, []byte{}, 3)
	require.Nil(t, absent.PreviousValue())
	require.NotNil(t, empty.PreviousValue())
	requireBudletsDifferAndRoundTrip(t, absent, empty)
}

// requireBudletsDifferAndRoundTrip requires two budlets to serialize differently, and each to deserialize back to
// itself with nil values kept nil and empty values kept non-nil.
func requireBudletsDifferAndRoundTrip(t *testing.T, first *Budlet, second *Budlet) {
	t.Helper()

	require.NotEqual(t, first.Serialize(), second.Serialize())
	for _, budlet := range []*Budlet{first, second} {
		deserialized, err := DeserializeBudlet(budlet.Serialize())
		require.NoError(t, err)
		require.Equal(t, budlet.Value() == nil, deserialized.Value() == nil)
		require.Equal(t, budlet.PreviousValue() == nil, deserialized.PreviousValue() == nil)
		require.Equal(t, budlet, deserialized)
	}
}

func TestBudletSerializationRoundTrip(t *testing.T) {
	rng := rand.New(rand.NewSource(4))
	for _, budlet := range randomBudlets(t, rng, 64) {
		deserialized, err := DeserializeBudlet(budlet.Serialize())
		require.NoError(t, err)
		require.Equal(t, budlet, deserialized)
	}
}

func TestDeserializeBudletRejectsMalformedInput(t *testing.T) {
	// A serialized value: its deletion flag, its declared length, and its contents.
	type serializedValue struct {
		deletionFlag byte
		length       uint32
		contents     []byte
	}
	serialize := func(key []byte, value serializedValue, previousValue serializedValue) []byte {
		data := binary.BigEndian.AppendUint32(nil, uint32(len(key))) //nolint:gosec // G115 - short test key
		data = append(data, key...)
		for _, v := range []serializedValue{value, previousValue} {
			data = append(data, v.deletionFlag)
			data = binary.BigEndian.AppendUint32(data, v.length)
			data = append(data, v.contents...)
		}
		return binary.BigEndian.AppendUint64(data, 7)
	}
	key := []byte("evm/a")
	written := serializedValue{0, 2, []byte{0xaa, 0xbb}}
	deleted := serializedValue{1, 0, nil}
	valid := serialize(key, written, written)
	_, err := DeserializeBudlet(valid)
	require.NoError(t, err)
	_, err = DeserializeBudlet(serialize(key, deleted, deleted))
	require.NoError(t, err)

	testCases := map[string][]byte{
		"empty key":                      serialize(nil, written, written),
		"deletion with a value":          serialize(key, serializedValue{1, 1, []byte{0xaa}}, written),
		"unknown deletion flag":          serialize(key, serializedValue{2, 0, nil}, written),
		"value length too long":          serialize(key, serializedValue{0, 3, []byte{0xaa, 0xbb}}, written),
		"absent previous with a value":   serialize(key, written, serializedValue{1, 1, []byte{0xaa}}),
		"unknown previous deletion flag": serialize(key, written, serializedValue{2, 0, nil}),
		"previous value length too long": serialize(key, written, serializedValue{0, 3, []byte{0xaa, 0xbb}}),
		"trailing byte":                  append(bytes.Clone(valid), 0),
	}
	for length := 0; length < len(valid); length++ {
		testCases[fmt.Sprintf("truncated to %d bytes", length)] = valid[:length]
	}
	for name, data := range testCases {
		t.Run(name, func(t *testing.T) {
			_, err := DeserializeBudlet(data)
			require.Error(t, err)
		})
	}
}
