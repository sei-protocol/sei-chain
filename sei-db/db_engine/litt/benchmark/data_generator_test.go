package benchmark

import (
	"testing"

	"github.com/Layr-Labs/eigenda/test/random"
	"github.com/stretchr/testify/require"
)

func TestDeterminism(t *testing.T) {
	rand := random.NewTestRandom()

	seed := rand.Int63()
	bufferSize := 1024 * rand.Uint64Range(1, 10)

	generator1 := NewDataGenerator(seed, bufferSize)
	generator2 := NewDataGenerator(seed, bufferSize)

	k1, v1 := generator1.Key(0), generator1.Value(0, 32)
	k2, v2 := generator1.Key(0), generator1.Value(0, 32)
	k3, v3 := generator2.Key(0), generator2.Value(0, 32)
	require.Equal(t, k1, k2)
	require.Equal(t, v1, v2)
	require.Equal(t, k1, k3)
	require.Equal(t, v1, v3)

	require.Equal(t, 32, len(v1))

	index := rand.Uint64()
	size := rand.Uint64Range(1, 100)
	k1, v1 = generator1.Key(index), generator1.Value(index, size)
	k2, v2 = generator1.Key(index), generator1.Value(index, size)
	k3, v3 = generator2.Key(index), generator2.Value(index, size)
	require.Equal(t, k1, k2)
	require.Equal(t, v1, v2)
	require.Equal(t, k1, k3)
	require.Equal(t, v1, v3)

	require.Equal(t, size, uint64(len(v1)))

	index = rand.Uint64()
	k1, v1 = generator1.Key(index), generator1.Value(index, bufferSize*2)
	k2, v2 = generator1.Key(index), generator1.Value(index, bufferSize*2)
	k3, v3 = generator2.Key(index), generator2.Value(index, bufferSize*2)
	require.Equal(t, k1, k2)
	require.Equal(t, v1, v2)
	require.Equal(t, k1, k3)
	require.Equal(t, v1, v3)

	require.Equal(t, bufferSize*2, uint64(len(v1)))
}
