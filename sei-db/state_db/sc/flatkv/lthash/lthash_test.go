package lthash

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"math/rand"
	"testing"
	"time"

	gigatypes "github.com/sei-protocol/sei-chain/sei-db/state_db/giga/types"
)

func TestLtHashBasic(t *testing.T) {
	lth := New()
	if !lth.IsZero() {
		t.Error("New() should be zero")
	}

	// Test via ComputeLtHash
	lth1 := ComputeLtHash(nil, []gigatypes.Mutation{
		gigatypes.NewMutation("key", []byte("value"), nil),
	})
	if lth1.IsZero() {
		t.Error("ComputeLtHash should not return zero for non-empty data")
	}

	lth2 := lth1.Clone()
	if !bytes.Equal(lth1.Marshal(), lth2.Marshal()) {
		t.Error("Clone should produce identical bytes")
	}

	lth3 := New()
	lth3.MixIn(lth1)
	lth3.MixOut(lth1)
	if !lth3.IsZero() {
		t.Error("MixIn then MixOut should return to zero")
	}

	checksum := lth1.Checksum()
	if len(checksum) != 32 {
		t.Errorf("Checksum should be 32 bytes, got %d", len(checksum))
	}
}

func TestLtHashDeterminism(t *testing.T) {
	mutations := []gigatypes.Mutation{
		gigatypes.NewMutation("key", []byte("test data for determinism"), nil),
	}

	lth1 := ComputeLtHash(nil, mutations)
	lth2 := ComputeLtHash(nil, mutations)

	if !bytes.Equal(lth1.Marshal(), lth2.Marshal()) {
		t.Error("ComputeLtHash should be deterministic")
	}

	if lth1.Checksum() != lth2.Checksum() {
		t.Error("Checksum should be deterministic")
	}
}

func TestHashKVNoCollision(t *testing.T) {
	// Verify length-prefixing prevents key||value concatenation collisions
	lth1 := ComputeLtHash(nil, []gigatypes.Mutation{
		gigatypes.NewMutation("a", []byte("bc"), nil),
	})
	lth2 := ComputeLtHash(nil, []gigatypes.Mutation{
		gigatypes.NewMutation("ab", []byte("c"), nil),
	})

	if lth1.Checksum() == lth2.Checksum() {
		t.Error("Different key/value pairs must produce different hashes")
	}
}

func TestComputeLtHash(t *testing.T) {
	// Empty input
	result := ComputeLtHash(nil, nil)
	if !result.IsZero() {
		t.Error("Empty changeset should produce zero")
	}
	// Insert
	result = ComputeLtHash(nil, []gigatypes.Mutation{
		gigatypes.NewMutation("key1", []byte("value1"), nil),
	})
	if result.IsZero() {
		t.Error("Insert should produce non-zero result")
	}

	// Insert then delete should cancel out
	result1 := ComputeLtHash(nil, []gigatypes.Mutation{
		gigatypes.NewMutation("key1", []byte("value1"), nil),
	})
	result2 := ComputeLtHash(result1, []gigatypes.Mutation{
		gigatypes.NewMutation("key1", nil, []byte("value1")),
	})
	if !result2.IsZero() {
		t.Error("Insert then delete should cancel out to zero")
	}

	// Update: old value replaced with new value
	initial := ComputeLtHash(nil, []gigatypes.Mutation{
		gigatypes.NewMutation("key1", []byte("value1"), nil),
	})
	updated := ComputeLtHash(initial, []gigatypes.Mutation{
		gigatypes.NewMutation("key1", []byte("value2"), []byte("value1")),
	})
	// updated should equal direct insert of value2
	direct := ComputeLtHash(nil, []gigatypes.Mutation{
		gigatypes.NewMutation("key1", []byte("value2"), nil),
	})
	if updated.Checksum() != direct.Checksum() {
		t.Error("Update should produce same result as direct insert")
	}
}

func TestComputeLtHashLarge(t *testing.T) {
	mutations := make([]gigatypes.Mutation, 500)
	for i := range mutations {
		key := string([]byte{byte(i >> 8), byte(i)})
		mutations[i] = gigatypes.NewMutation(key, []byte{byte(i), byte(i >> 8)}, nil)
	}

	result := ComputeLtHash(nil, mutations)
	if result.IsZero() {
		t.Error("Large changeset should produce non-zero result")
	}
}

func TestUnmarshal(t *testing.T) {
	original := ComputeLtHash(nil, []gigatypes.Mutation{
		gigatypes.NewMutation("key", []byte("test data"), nil),
	})
	rawBytes := original.Marshal()

	restored, err := Unmarshal(rawBytes)
	if err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if !bytes.Equal(original.Marshal(), restored.Marshal()) {
		t.Error("Unmarshal should restore identical LtHash")
	}

	_, err = Unmarshal([]byte("too short"))
	if err == nil {
		t.Error("Unmarshal should fail for invalid length")
	}
}

func TestChecksumHex(t *testing.T) {
	lth := ComputeLtHash(nil, []gigatypes.Mutation{
		gigatypes.NewMutation("key", []byte("hello"), nil),
	})
	checksum := lth.Checksum()
	hexStr := hex.EncodeToString(checksum[:])
	if len(hexStr) != 64 {
		t.Errorf("Hex checksum should be 64 chars, got %d", len(hexStr))
	}
}

func TestReset(t *testing.T) {
	lth := ComputeLtHash(nil, []gigatypes.Mutation{
		gigatypes.NewMutation("key", []byte("data"), nil),
	})
	if lth.IsZero() {
		t.Error("Should not be zero after ComputeLtHash")
	}
	lth.Reset()
	if !lth.IsZero() {
		t.Error("Should be zero after Reset")
	}
}

func TestEmptyKeyOrValue(t *testing.T) {
	// Empty key or value should be skipped
	result := ComputeLtHash(nil, []gigatypes.Mutation{
		gigatypes.NewMutation("", []byte("value"), nil),
	})
	if !result.IsZero() {
		t.Error("Empty key should be skipped")
	}

	result = ComputeLtHash(nil, []gigatypes.Mutation{
		gigatypes.NewMutation("key", nil, nil),
	})
	if !result.IsZero() {
		t.Error("Empty value should be skipped")
	}
}

// TestParallelConsistency verifies that parallel execution produces
// the exact same result as serial execution (using small batch).
func TestParallelConsistency(t *testing.T) {
	// Create enough pairs to trigger parallel path (> 100)
	count := 500
	mutations := make([]gigatypes.Mutation, count)
	for i := 0; i < count; i++ {
		key := fmt.Sprintf("key-%d", i)
		val := fmt.Sprintf("val-%d", i)
		mutations[i] = gigatypes.NewMutation(key, []byte(val), nil)
	}

	// 1. Run with parallel workers (default)
	parallelResult := ComputeLtHash(nil, mutations)

	// 2. Run strictly serial by forcing computeDeltaSerial logic via small chunks or mock?
	// Actually, we can just call computeDeltaSerial directly if we export it or use reflection,
	// BUT simpler way: computeDeltaSerial is called when len < 100.
	// So we can manually split the 500 items into 5 chunks of 100 and combine them serialy.
	serialResult := New()
	chunkSize := 50
	for i := 0; i < count; i += chunkSize {
		end := i + chunkSize
		chunk := mutations[i:end]
		// Calling ComputeLtHash with small chunk will trigger serial path
		chunkHash := ComputeLtHash(nil, chunk)
		serialResult.MixIn(chunkHash)
	}

	if parallelResult.Checksum() != serialResult.Checksum() {
		t.Errorf("Parallel result %x != Serial result %x", parallelResult.Checksum(), serialResult.Checksum())
	}
}

// TestHomomorphicProperties verifies mathematical properties:
// Commutativity: A + B = B + A
// Associativity: (A + B) + C = A + (B + C)
func TestHomomorphicProperties(t *testing.T) {
	kv1 := []gigatypes.Mutation{gigatypes.NewMutation("k1", []byte("v1"), nil)}
	kv2 := []gigatypes.Mutation{gigatypes.NewMutation("k2", []byte("v2"), nil)}
	kv3 := []gigatypes.Mutation{gigatypes.NewMutation("k3", []byte("v3"), nil)}

	h1 := ComputeLtHash(nil, kv1)
	h2 := ComputeLtHash(nil, kv2)
	h3 := ComputeLtHash(nil, kv3)

	// Commutativity: h1 + h2 == h2 + h1
	sum12 := h1.Clone()
	sum12.MixIn(h2)

	sum21 := h2.Clone()
	sum21.MixIn(h1)

	if sum12.Checksum() != sum21.Checksum() {
		t.Error("Commutativity failed")
	}

	// Associativity: (h1 + h2) + h3 == h1 + (h2 + h3)
	// Left side: (h1 + h2) + h3
	left := sum12.Clone()
	left.MixIn(h3)

	// Right side: h1 + (h2 + h3)
	right := h1.Clone()
	sum23 := h2.Clone()
	sum23.MixIn(h3)
	right.MixIn(sum23)

	if left.Checksum() != right.Checksum() {
		t.Error("Associativity failed")
	}
}

// TestFuzz runs random operations to ensure stability
func TestFuzz(t *testing.T) {
	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	base := New()

	// Perform 1000 random operations
	for i := 0; i < 1000; i++ {
		key := make([]byte, 8)
		binary.LittleEndian.PutUint64(key, rng.Uint64())
		val := make([]byte, 8)
		binary.LittleEndian.PutUint64(val, rng.Uint64())

		// Randomly insert or delete
		var op gigatypes.Mutation
		if rng.Intn(2) == 0 {
			// Insert
			op = gigatypes.NewMutation(string(key), val, nil)
		} else {
			// Delete (requires we "know" the old value, but here we just test MixOut stability)
			op = gigatypes.NewMutation(string(key), nil, val)
		}

		next := ComputeLtHash(base, []gigatypes.Mutation{op})
		base = next
	}

	if base == nil {
		t.Error("Result should not be nil")
	}
}
