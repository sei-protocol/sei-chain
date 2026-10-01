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

		previousHeight := rng.Uint64() >> rng.Intn(64)
		// A deletion one time in four; otherwise a write of 0 to 32 bytes, which is never nil.
		var value []byte
		if rng.Intn(4) != 0 {
			value = make([]byte, rng.Intn(33))
			_, _ = rng.Read(value)
		}
		budlet, err := NewBudlet(key, value, previousHeight)
		require.NoError(t, err)
		budlets = append(budlets, budlet)
	}
	slices.SortFunc(budlets, func(a *Budlet, b *Budlet) int {
		return bytes.Compare(a.Key(), b.Key())
	})
	return budlets
}

// newTestBudlet returns NewBudlet(), failing the test on an error.
func newTestBudlet(t *testing.T, key string, value []byte, previousHeight uint64) *Budlet {
	t.Helper()

	budlet, err := NewBudlet([]byte(key), value, previousHeight)
	require.NoError(t, err)
	return budlet
}

// newTestDeletionBudlet returns the budlet deleting key, failing the test on an error.
func newTestDeletionBudlet(t *testing.T, key string, previousHeight uint64) *Budlet {
	t.Helper()

	budlet, err := NewBudlet([]byte(key), nil, previousHeight)
	require.NoError(t, err)
	return budlet
}

// threeBudlets returns a small block of budlets, sorted by key.
func threeBudlets(t *testing.T) []*Budlet {
	t.Helper()

	return []*Budlet{
		newTestBudlet(t, "evm/a", []byte{0x01}, 3),
		newTestBudlet(t, "evm/b", []byte{0x02, 0x03}, 0),
		newTestDeletionBudlet(t, "evm/c", 9),
	}
}

func TestNewBudletRejectsEmptyKey(t *testing.T) {
	_, err := NewBudlet(nil, []byte{0x01}, 0)
	require.Error(t, err)
	_, err = NewBudlet([]byte{}, nil, 0)
	require.Error(t, err)
}

func TestBudletDistinguishesDeletionFromEmptyWrite(t *testing.T) {
	deletion := newTestDeletionBudlet(t, "evm/a", 3)
	emptyWrite := newTestBudlet(t, "evm/a", []byte{}, 3)
	require.Nil(t, deletion.Value())
	require.NotNil(t, emptyWrite.Value())
	require.NotEqual(t, deletion.Serialize(), emptyWrite.Serialize())

	for _, budlet := range []*Budlet{deletion, emptyWrite} {
		deserialized, err := DeserializeBudlet(budlet.Serialize())
		require.NoError(t, err)
		require.Equal(t, budlet.Value() == nil, deserialized.Value() == nil)
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
	serialize := func(key []byte, deletionFlag byte, valueLength uint32, value []byte) []byte {
		data := binary.BigEndian.AppendUint32(nil, uint32(len(key))) //nolint:gosec // G115 - short test key
		data = append(data, key...)
		data = append(data, deletionFlag)
		data = binary.BigEndian.AppendUint32(data, valueLength)
		data = append(data, value...)
		return binary.BigEndian.AppendUint64(data, 7)
	}
	valid := serialize([]byte("evm/a"), 0, 2, []byte{0xaa, 0xbb})
	_, err := DeserializeBudlet(valid)
	require.NoError(t, err)

	testCases := map[string][]byte{
		"empty key":             serialize(nil, 0, 1, []byte{0xaa}),
		"deletion with a value": serialize([]byte("evm/a"), 1, 1, []byte{0xaa}),
		"unknown deletion flag": serialize([]byte("evm/a"), 2, 0, nil),
		"value length too long": serialize([]byte("evm/a"), 0, 3, []byte{0xaa, 0xbb}),
		"trailing byte":         append(bytes.Clone(valid), 0),
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
