package bud

import (
	"bytes"
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

// randomBudletValue returns a non-nil value of 0 to 32 random bytes.
func randomBudletValue(rng *rand.Rand) []byte {
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

// threeBudlets returns a small block of budlets, sorted by key.
func threeBudlets(t *testing.T) []*Budlet {
	t.Helper()

	return []*Budlet{
		newTestBudlet(t, "evm/a", []byte{0x01}, []byte{0x09}, 3),
		newTestBudlet(t, "evm/b", []byte{0x02, 0x03}, []byte{}, 0),
		newTestBudlet(t, "evm/c", []byte{}, []byte{0x04}, 9),
	}
}

func TestNewBudletRejectsEmptyKey(t *testing.T) {
	_, err := NewBudlet(nil, []byte{0x01}, []byte{}, 0)
	require.Error(t, err)
	_, err = NewBudlet([]byte{}, []byte{0x01}, []byte{}, 0)
	require.Error(t, err)
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
	valid := newTestBudlet(t, "evm/a", []byte{0xaa, 0xbb}, []byte{0xcc}, 7).Serialize()
	_, err := DeserializeBudlet(valid)
	require.NoError(t, err)

	testCases := map[string][]byte{
		"empty key":     slices.Concat([]byte{0, 0, 0, 0}, valid[4+len("evm/a"):]),
		"trailing byte": append(bytes.Clone(valid), 0),
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
