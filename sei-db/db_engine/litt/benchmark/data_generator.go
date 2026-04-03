package benchmark

import (
	"math/rand"
	"sync"
)

// DataGenerator is responsible for generating key-value pairs to be inserted into the database, for the sake of
// benchmarking.
type DataGenerator struct {
	// Pool of random number generators
	randPool *sync.Pool

	// A pool of randomness. Used to generate values.
	dataPool []byte

	// The seed that determines the key/value pairs generated.
	seed int64
}

// NewDataGenerator builds a data generator instance.
func NewDataGenerator(seed int64, poolSize uint64) *DataGenerator {

	randPool := &sync.Pool{
		New: func() interface{} {
			return rand.New(rand.NewSource(seed))
		},
	}

	dataPool := make([]byte, poolSize)
	rng := randPool.Get().(*rand.Rand)
	rng.Read(dataPool)
	randPool.Put(rng)

	return &DataGenerator{
		randPool: randPool,
		dataPool: dataPool,
	}
}

// Key generates a new key. The value is deterministic for the same index and seed.
func (g *DataGenerator) Key(index uint64) []byte {
	rng := g.randPool.Get().(*rand.Rand)
	rng.Seed(g.seed + int64(index))

	key := make([]byte, 32)
	rng.Read(key)
	g.randPool.Put(rng)

	return key
}

// Value generates a new value. The value is deterministic for the same index, seed, and value size.
func (g *DataGenerator) Value(index uint64, valueLength uint64) []byte {
	rng := g.randPool.Get().(*rand.Rand)
	rng.Seed(g.seed + int64(index))

	var value []byte

	if valueLength > uint64(len(g.dataPool)) {
		// Special case: we don't have enough data in the pool to satisfy the request.
		// For the sake of completeness, just generate the data if this happens.
		// This shouldn't be encountered for sane configurations (i.e. with a pool size much larger than value sizes).
		value = make([]byte, valueLength)
		rng.Read(value)
	} else {
		startIndex := rng.Intn(len(g.dataPool) - int(valueLength))
		value = g.dataPool[startIndex : startIndex+int(valueLength)]
	}

	g.randPool.Put(rng)

	return value
}
