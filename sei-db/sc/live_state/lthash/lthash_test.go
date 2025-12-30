package lthash

import (
	"bytes"
	"encoding/hex"
	"testing"
)

func TestLtHashBasic(t *testing.T) {
	lth := New()
	if !lth.IsZero() {
		t.Error("New() should be zero")
	}

	lth1 := Hash([]byte("hello world"))
	if lth1.IsZero() {
		t.Error("Hash should not return zero for non-empty data")
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
	data := []byte("test data for determinism")

	lth1 := Hash(data)
	lth2 := Hash(data)

	if !bytes.Equal(lth1.Marshal(), lth2.Marshal()) {
		t.Error("Hash should be deterministic")
	}

	if lth1.Checksum() != lth2.Checksum() {
		t.Error("Checksum should be deterministic")
	}
}

func TestHashKV(t *testing.T) {
	lth := HashKV("evm", []byte("key"), []byte("value"))
	if lth == nil {
		t.Fatal("HashKV returned nil for valid input")
	}
	if lth.IsZero() {
		t.Error("HashKV should not return zero")
	}

	// Empty key/value returns nil
	if HashKV("evm", nil, []byte("value")) != nil {
		t.Error("Empty key should return nil")
	}
	if HashKV("evm", []byte("key"), nil) != nil {
		t.Error("Empty value should return nil")
	}
}

func TestHashKVDomainIsolation(t *testing.T) {
	key := []byte("samekey")
	value := []byte("samevalue")

	lth1 := HashKV("evm", key, value)
	lth2 := HashKV("bank", key, value)

	if lth1.Checksum() == lth2.Checksum() {
		t.Error("Different dbNames should produce different checksums")
	}
}

func TestComputeLtHashDeltaParallel(t *testing.T) {
	delta, timings := ComputeLtHashDeltaParallel("test", nil, 4)
	if !delta.IsZero() {
		t.Error("Empty changeset should produce zero delta")
	}
	if timings == nil {
		t.Error("Timings should not be nil")
	}

	kvPairs := []KVPairWithOldValue{
		{Key: []byte("key1"), Value: []byte("value1")},
	}
	delta, _ = ComputeLtHashDeltaParallel("test", kvPairs, 4)
	if delta.IsZero() {
		t.Error("Insert should produce non-zero delta")
	}

	// Insert then delete should cancel out
	delta1, _ := ComputeLtHashDeltaParallel("test", []KVPairWithOldValue{
		{Key: []byte("key1"), Value: []byte("value1")},
	}, 4)

	delta2, _ := ComputeLtHashDeltaParallel("test", []KVPairWithOldValue{
		{Key: []byte("key1"), LastFlushValue: []byte("value1"), Deleted: true},
	}, 4)

	combined := New()
	combined.MixIn(delta1)
	combined.MixIn(delta2)
	if !combined.IsZero() {
		t.Error("Insert then delete should cancel out to zero")
	}
}

func TestComputeLtHashDeltaParallelLarge(t *testing.T) {
	kvPairs := make([]KVPairWithOldValue, 500)
	for i := range kvPairs {
		kvPairs[i] = KVPairWithOldValue{
			Key:   []byte{byte(i >> 8), byte(i)},
			Value: []byte{byte(i), byte(i >> 8)},
		}
	}

	delta, timings := ComputeLtHashDeltaParallel("test", kvPairs, 4)
	if delta.IsZero() {
		t.Error("Large changeset should produce non-zero delta")
	}
	if timings.TotalNs <= 0 {
		t.Error("Total time should be positive")
	}
	if timings.Blake3Ns <= 0 {
		t.Error("Blake3 time should be positive")
	}
}

func TestUnmarshal(t *testing.T) {
	original := Hash([]byte("test data"))
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
	lth := Hash([]byte("hello"))
	checksum := lth.Checksum()
	hexStr := hex.EncodeToString(checksum[:])
	if len(hexStr) != 64 {
		t.Errorf("Hex checksum should be 64 chars, got %d", len(hexStr))
	}
}

func TestReset(t *testing.T) {
	lth := Hash([]byte("data"))
	if lth.IsZero() {
		t.Error("Should not be zero after Hash")
	}
	lth.Reset()
	if !lth.IsZero() {
		t.Error("Should be zero after Reset")
	}
}
