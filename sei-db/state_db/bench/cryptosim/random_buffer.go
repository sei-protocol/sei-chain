package cryptosim

import (
	"encoding/binary"
	"fmt"
	"math/rand"
)

// A utility that buffers randomness in a way that is highly performant for benchmarking.
// It contains a buffer of random bytes that it reuses to avoid generating lots of random numbers
// at runtime.
//
// The goal is to avoid accidentally creating CPU hotspots in the benchmark framework. We want
// to exercise the DB, not the code that feeds it randomness.
//
// This utility is not thread safe.
//
// In case it requires saying, this utility is NOT a suitable source cryptographically secure random numbers.
type RandomBuffer struct {
	// Pre-generated buffer of random bytes.
	buffer []byte

	// Controls the next index to read from the buffer.
	index int64
}

// Creates a new random buffer.
func NewRandomBuffer(
	// The size of the buffer to create. A slice of this size is instantiated, so avoid setting this too large.
	// Do not set this too small though, as the buffer will panic if the requested number of bytes is greater
	// than the buffer size.
	bufferSize int,
	// The seed to use to generate the random bytes. This utility provides deterministic random numbers
	// given the same seed.
	seed int64,
) *RandomBuffer {

	if bufferSize < 8 {
		panic(fmt.Sprintf("buffer size must be at least 8 bytes, got %d", bufferSize))
	}

	// Adjust the buffer size so that (bufferSize % 8) == 1. This way when the index wraps around, it won't align
	// perfectly with the buffer, giving us a longer runway before we repeat the exact same sequence of bytes.
	// TODO verify that this logic works as intended.
	bufferSize += (1 - (bufferSize % 8) + 8) % 8

	source := rand.NewSource(seed)
	rng := rand.New(source)

	buffer := make([]byte, bufferSize)
	rng.Read(buffer)

	return &RandomBuffer{
		buffer: buffer,
		index:  0,
	}
}

// Returns a slice of random bytes.
//
// Returned slice is NOT safe to modify. If modification is required, the caller should make a copy of the slice.
func (rb *RandomBuffer) Bytes(count int) []byte {
	return rb.SeededBytes(count, rb.Int64())
}

// Returns a slice of random bytes from a given seed. Bytes are deterministic given the same seed.
//
// Returned slice is NOT safe to modify. If modification is required, the caller should make a copy of the slice.
func (rb *RandomBuffer) SeededBytes(count int, seed int64) []byte {

	if count < 0 {
		panic(fmt.Sprintf("count must be non-negative, got %d", count))
	}
	if count > len(rb.buffer) {
		panic(fmt.Sprintf("count must be less than or equal to the buffer size, got %d", count))
	} else if count == len(rb.buffer) {
		return rb.buffer
	}

	startIndex := PositiveHash64(seed) % int64(len(rb.buffer)-count)
	return rb.buffer[startIndex : startIndex+int64(count)]
}

// Generate a random-ish int64.
func (rb *RandomBuffer) Int64() int64 {
	bufLen := int64(len(rb.buffer))
	var buf [8]byte
	for i := int64(0); i < 8; i++ {
		buf[i] = rb.buffer[(rb.index+i)%bufLen]
	}
	base := binary.BigEndian.Uint64(buf[:])
	result := Hash64(int64(base) + rb.index)

	// Add 8 to the index to skip the 8 bytes we just read.
	rb.index = (rb.index + 8) % bufLen
	return result
}

// Generate a random address suitable for use simulating an eth-style address. Given the same account ID,
// the address will be deterministic, and any two unique account IDs are guaranteed to generate unique addresses.
func (rb *RandomBuffer) Address(
	// The prefix to use for the address.
	keyPrefix string,
	// The size of the address to generate, not including the key prefix.
	addressSize int,
	// The account ID to use for the address.
	accountID int64,
) []byte {

	if addressSize < 8 {
		panic(fmt.Sprintf("address size must be at least 8 bytes, got %d", addressSize))
	}

	result := make([]byte, len(keyPrefix)+addressSize)
	address := result[len(keyPrefix):]
	copy(result, keyPrefix)

	baseBytes := rb.SeededBytes(addressSize, accountID)

	// Inject the account ID into the middle of the address span (excluding the prefix) to ensure uniqueness.
	accountIDIndex := len(keyPrefix) + (addressSize / 2)
	if accountIDIndex+8 > len(result) {
		accountIDIndex = len(result) - 8
	}

	copy(address, baseBytes)
	binary.BigEndian.PutUint64(result[accountIDIndex:accountIDIndex+8], uint64(accountID))

	return result
}
