package cryptosim

import (
	"encoding/binary"
	"fmt"
	"math"
	"math/rand"
)

// CannedRandom provides pre-generated randomness for benchmarking.
// It contains a buffer of random bytes that it reuses to avoid generating lots of random numbers
// at runtime.
//
// The goal is to avoid accidentally creating CPU hotspots in the benchmark framework. We want
// to exercise the DB, not the code that feeds it randomness.
//
// This utility is not thread safe.
//
// In case it requires saying, this utility is NOT a suitable source cryptographically secure random numbers.
type CannedRandom struct {
	// Pre-generated buffer of random bytes.
	buffer []byte

	// Controls the next index to read from the buffer.
	index int64
}

// NewCannedRandom creates a new CannedRandom.
func NewCannedRandom(
	// The size of the buffer to create. A slice of this size is instantiated, so avoid setting this too large.
	// Do not set this too small though, as the buffer will panic if the requested number of bytes is greater
	// than the buffer size.
	bufferSize int,
	// The seed to use to generate the random bytes. This utility provides deterministic random numbers
	// given the same seed.
	seed int64,
) *CannedRandom {

	if bufferSize < 8 {
		panic(fmt.Sprintf("buffer size must be at least 8 bytes, got %d", bufferSize))
	}

	// Adjust the buffer size so that (bufferSize % 8) == 1. This way when the index wraps around, it won't align
	// perfectly with the buffer, giving us a longer runway before we repeat the exact same sequence of bytes.
	// The expression maps (bufferSize%8) -> add: 0->1, 1->0, 2->7, 3->6, 4->5, 5->4, 6->3, 7->2.
	bufferSize += (1 - (bufferSize % 8) + 8) % 8

	source := rand.NewSource(seed)
	rng := rand.New(source)

	buffer := make([]byte, bufferSize)
	rng.Read(buffer)

	return &CannedRandom{
		buffer: buffer,
		index:  0,
	}
}

// Returns a slice of random bytes.
//
// Returned slice is NOT safe to modify. If modification is required, the caller should make a copy of the slice.
func (cr *CannedRandom) Bytes(count int) []byte {
	return cr.SeededBytes(count, cr.Int64())
}

// Returns a slice of random bytes from a given seed. Bytes are deterministic given the same seed.
//
// Returned slice is NOT safe to modify. If modification is required, the caller should make a copy of the slice.
func (cr *CannedRandom) SeededBytes(count int, seed int64) []byte {
	if count < 0 {
		panic(fmt.Sprintf("count must be non-negative, got %d", count))
	}
	if count > len(cr.buffer) {
		panic(fmt.Sprintf("count must be less than or equal to the buffer size, got %d", count))
	} else if count == len(cr.buffer) {
		return cr.buffer
	}

	startIndex := PositiveHash64(seed) % int64(len(cr.buffer)-count)
	return cr.buffer[startIndex : startIndex+int64(count)]
}

// Generate a random-ish int64.
func (cr *CannedRandom) Int64() int64 {
	bufLen := int64(len(cr.buffer))
	var buf [8]byte
	for i := int64(0); i < 8; i++ {
		buf[i] = cr.buffer[(cr.index+i)%bufLen]
	}
	base := binary.BigEndian.Uint64(buf[:])
	result := Hash64(int64(base) + cr.index)

	// Add 8 to the index to skip the 8 bytes we just read.
	cr.index = (cr.index + 8) % bufLen
	return result
}

// Int64Range returns a random int64 in [min, max). Min is inclusive, max is exclusive.
// If min == max, returns min.
func (cr *CannedRandom) Int64Range(min int64, max int64) int64 {
	if min == max {
		return min
	}
	if max < min {
		panic(fmt.Sprintf("max must be >= min, got min=%d max=%d", min, max))
	}
	return min + int64(uint64(cr.Int64())%uint64(max-min))
}

// Float64 returns a random float64 in the range [0.0, 1.0].
// It uses Int64() internally and converts the result to the proper range.
func (cr *CannedRandom) Float64() float64 {
	return float64(uint64(cr.Int64())) / float64(math.MaxUint64)
}

// Bool returns a random boolean.
func (cr *CannedRandom) Bool() bool {
	return cr.Int64()%2 == 0
}

// Generate a random 20 byte address suitable for use simulating an eth-style address.
// For the same input arguments, a canned random generator with the same seed and size will produce
// deterministic results addresses.
//
// Addresses have the following shape:
//
//		1 byte addressType
//		8 bytes of random data
//		8 bytes containing the ID
//	    3 bytes of random data (to bring the total to 20 bytes)
//
// The ID is not included in the begining so that adjacent IDs will not appear close to each other if addresses
// are sorted in lexicographic order.
func (cr *CannedRandom) Address(
	// A one-char byte descriptor. Allows for keys for different types of things to have different values
	// even if they have the same ID.
	addressType uint8,
	// A unique ID for the key.
	id int64,

) []byte {

	result := make([]byte, 20)

	baseBytes := cr.SeededBytes(20, id)
	copy(result, baseBytes)

	result[0] = addressType
	binary.BigEndian.PutUint64(result[9:], uint64(id))

	return result
}
