package lthash

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"math/rand"
	"testing"
	"time"
)

func TestLtHashBasic(t *testing.T) {
	lth := New()
	if !lth.IsZero() {
		t.Error("New() should be zero")
	}

	// Test via ComputeLtHash
	lth1, _ := ComputeLtHash(nil, []KVPairWithLastValue{
		{Key: []byte("key"), Value: []byte("value")},
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
	kvPairs := []KVPairWithLastValue{
		{Key: []byte("key"), Value: []byte("test data for determinism")},
	}

	lth1, _ := ComputeLtHash(nil, kvPairs)
	lth2, _ := ComputeLtHash(nil, kvPairs)

	if !bytes.Equal(lth1.Marshal(), lth2.Marshal()) {
		t.Error("ComputeLtHash should be deterministic")
	}

	if lth1.Checksum() != lth2.Checksum() {
		t.Error("Checksum should be deterministic")
	}
}

func TestHashKVNoCollision(t *testing.T) {
	// Verify length-prefixing prevents key||value concatenation collisions
	lth1, _ := ComputeLtHash(nil, []KVPairWithLastValue{
		{Key: []byte("a"), Value: []byte("bc")},
	})
	lth2, _ := ComputeLtHash(nil, []KVPairWithLastValue{
		{Key: []byte("ab"), Value: []byte("c")},
	})

	if lth1.Checksum() == lth2.Checksum() {
		t.Error("Different key/value pairs must produce different hashes")
	}
}

func TestComputeLtHash(t *testing.T) {
	// Empty input
	result, timings := ComputeLtHash(nil, nil)
	if !result.IsZero() {
		t.Error("Empty changeset should produce zero")
	}
	if timings == nil {
		t.Error("Timings should not be nil")
	}

	// Insert
	result, _ = ComputeLtHash(nil, []KVPairWithLastValue{
		{Key: []byte("key1"), Value: []byte("value1")},
	})
	if result.IsZero() {
		t.Error("Insert should produce non-zero result")
	}

	// Insert then delete should cancel out
	result1, _ := ComputeLtHash(nil, []KVPairWithLastValue{
		{Key: []byte("key1"), Value: []byte("value1")},
	})
	result2, _ := ComputeLtHash(result1, []KVPairWithLastValue{
		{Key: []byte("key1"), LastValue: []byte("value1"), Delete: true},
	})
	if !result2.IsZero() {
		t.Error("Insert then delete should cancel out to zero")
	}

	// Update: old value replaced with new value
	initial, _ := ComputeLtHash(nil, []KVPairWithLastValue{
		{Key: []byte("key1"), Value: []byte("value1")},
	})
	updated, _ := ComputeLtHash(initial, []KVPairWithLastValue{
		{Key: []byte("key1"), Value: []byte("value2"), LastValue: []byte("value1")},
	})
	// updated should equal direct insert of value2
	direct, _ := ComputeLtHash(nil, []KVPairWithLastValue{
		{Key: []byte("key1"), Value: []byte("value2")},
	})
	if updated.Checksum() != direct.Checksum() {
		t.Error("Update should produce same result as direct insert")
	}
}

func TestComputeLtHashLarge(t *testing.T) {
	kvPairs := make([]KVPairWithLastValue, 500)
	for i := range kvPairs {
		kvPairs[i] = KVPairWithLastValue{
			Key:   []byte{byte(i >> 8), byte(i)},
			Value: []byte{byte(i), byte(i >> 8)},
		}
	}

	result, timings := ComputeLtHash(nil, kvPairs)
	if result.IsZero() {
		t.Error("Large changeset should produce non-zero result")
	}
	if timings.TotalNs <= 0 {
		t.Error("Total time should be positive")
	}
	if timings.Blake3Ns <= 0 {
		t.Error("Blake3 time should be positive")
	}
}

func TestUnmarshal(t *testing.T) {
	original, _ := ComputeLtHash(nil, []KVPairWithLastValue{
		{Key: []byte("key"), Value: []byte("test data")},
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
	lth, _ := ComputeLtHash(nil, []KVPairWithLastValue{
		{Key: []byte("key"), Value: []byte("hello")},
	})
	checksum := lth.Checksum()
	hexStr := hex.EncodeToString(checksum[:])
	if len(hexStr) != 64 {
		t.Errorf("Hex checksum should be 64 chars, got %d", len(hexStr))
	}
}

func TestReset(t *testing.T) {
	lth, _ := ComputeLtHash(nil, []KVPairWithLastValue{
		{Key: []byte("key"), Value: []byte("data")},
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
	result, _ := ComputeLtHash(nil, []KVPairWithLastValue{
		{Key: nil, Value: []byte("value")},
	})
	if !result.IsZero() {
		t.Error("Empty key should be skipped")
	}

	result, _ = ComputeLtHash(nil, []KVPairWithLastValue{
		{Key: []byte("key"), Value: nil},
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
	kvPairs := make([]KVPairWithLastValue, count)
	for i := 0; i < count; i++ {
		key := fmt.Sprintf("key-%d", i)
		val := fmt.Sprintf("val-%d", i)
		kvPairs[i] = KVPairWithLastValue{
			Key:   []byte(key),
			Value: []byte(val),
		}
	}

	// 1. Run with parallel workers (default)
	parallelResult, _ := ComputeLtHash(nil, kvPairs)

	// 2. Run strictly serial by forcing computeDeltaSerial logic via small chunks or mock?
	// Actually, we can just call computeDeltaSerial directly if we export it or use reflection,
	// BUT simpler way: computeDeltaSerial is called when len < 100.
	// So we can manually split the 500 items into 5 chunks of 100 and combine them serialy.
	serialResult := New()
	chunkSize := 50
	for i := 0; i < count; i += chunkSize {
		end := i + chunkSize
		chunk := kvPairs[i:end]
		// Calling ComputeLtHash with small chunk will trigger serial path
		chunkHash, _ := ComputeLtHash(nil, chunk)
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
	kv1 := []KVPairWithLastValue{{Key: []byte("k1"), Value: []byte("v1")}}
	kv2 := []KVPairWithLastValue{{Key: []byte("k2"), Value: []byte("v2")}}
	kv3 := []KVPairWithLastValue{{Key: []byte("k3"), Value: []byte("v3")}}

	h1, _ := ComputeLtHash(nil, kv1)
	h2, _ := ComputeLtHash(nil, kv2)
	h3, _ := ComputeLtHash(nil, kv3)

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
		op := KVPairWithLastValue{Key: key}
		if rng.Intn(2) == 0 {
			// Insert
			op.Value = val
		} else {
			// Delete (requires we "know" the old value, but here we just test MixOut stability)
			op.LastValue = val
			op.Delete = true
		}

		next, _ := ComputeLtHash(base, []KVPairWithLastValue{op})
		base = next
	}

	if base == nil {
		t.Error("Result should not be nil")
	}
}
