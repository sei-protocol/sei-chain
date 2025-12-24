package lthash

import (
	"encoding/binary"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLtHashBasic(t *testing.T) {
	lth1 := NewEmptyLtHash()
	assert.True(t, lth1.IsIdentity())

	data1 := []byte("hello")
	lth1 = FromBytes(data1)
	assert.False(t, lth1.IsIdentity())

	lth2 := FromBytes(data1)
	assert.Equal(t, lth1.limbs, lth2.limbs)
	assert.Equal(t, lth1.Checksum(), lth2.Checksum())

	data2 := []byte("world")
	lth2 = FromBytes(data2)
	assert.NotEqual(t, lth1.limbs, lth2.limbs)

	// Test MixIn
	lthSum := NewEmptyLtHash()
	lthSum.MixIn(lth1)
	lthSum.MixIn(lth2)

	lthSum2 := NewEmptyLtHash()
	lthSum2.MixIn(lth2)
	lthSum2.MixIn(lth1)
	assert.Equal(t, lthSum.limbs, lthSum2.limbs) // Commutative

	// Test MixOut
	lthSum.MixOut(lth1)
	assert.Equal(t, lth2.limbs, lthSum.limbs)
	lthSum.MixOut(lth2)
	assert.True(t, lthSum.IsIdentity())
}

func TestLtHashSerialization(t *testing.T) {
	lth1 := FromBytes([]byte("test data"))
	bz := lth1.Bytes()
	assert.Equal(t, LtHashBytes, len(bz))

	lth2, err := FromRaw(bz)
	require.NoError(t, err)
	assert.Equal(t, lth1.limbs, lth2.limbs)
}

func TestLtHashParallel(t *testing.T) {
	dbName := "bank"
	kvs := []KVPairWithOldValue{
		{Key: []byte("key1"), Value: []byte("val1"), LastFlushValue: nil},
		{Key: []byte("key2"), Value: []byte("val2"), LastFlushValue: nil},
		{Key: []byte("key3"), Value: []byte("val3"), LastFlushValue: []byte("old3")},
		{Key: []byte("key4"), Value: nil, LastFlushValue: []byte("old4"), Deleted: true},
	}

	delta, _ := ComputeLtHashDeltaParallel(dbName, kvs, 2)

	// Manual computation
	expected := NewEmptyLtHash()
	// key1: add new
	expected.MixIn(FromBytes(SerializeForLtHash(dbName, []byte("key1"), []byte("val1"))))
	// key2: add new
	expected.MixIn(FromBytes(SerializeForLtHash(dbName, []byte("key2"), []byte("val2"))))
	// key3: sub old, add new
	expected.MixOut(FromBytes(SerializeForLtHash(dbName, []byte("key3"), []byte("old3"))))
	expected.MixIn(FromBytes(SerializeForLtHash(dbName, []byte("key3"), []byte("val3"))))
	// key4: sub old
	expected.MixOut(FromBytes(SerializeForLtHash(dbName, []byte("key4"), []byte("old4"))))

	assert.Equal(t, expected.limbs, delta.limbs)
}

func TestSerializeForLtHash(t *testing.T) {
	// Test serialization format: len || dbName || key || value
	dbName := "evm"
	key := []byte("test_key")
	value := []byte("test_value")

	bz := SerializeForLtHash(dbName, key, value)

	// Verify format
	dbNameLen := binary.LittleEndian.Uint16(bz[0:2])
	assert.Equal(t, uint16(len(dbName)), dbNameLen)
	assert.Equal(t, []byte(dbName), bz[2:2+len(dbName)])
	assert.Equal(t, key, bz[2+len(dbName):2+len(dbName)+len(key)])
	assert.Equal(t, value, bz[2+len(dbName)+len(key):])

	// Test nil cases
	assert.Nil(t, SerializeForLtHash(dbName, nil, value))
	assert.Nil(t, SerializeForLtHash(dbName, key, nil))
	assert.Nil(t, SerializeForLtHash(dbName, []byte{}, value))
	assert.Nil(t, SerializeForLtHash(dbName, key, []byte{}))
}

func TestSerializeDomainIsolation(t *testing.T) {
	// Same key/value in different stores should produce different hashes
	key := []byte("same_key")
	value := []byte("same_value")

	bz1 := SerializeForLtHash("evm", key, value)
	bz2 := SerializeForLtHash("bank", key, value)

	// Serializations should be different
	assert.NotEqual(t, bz1, bz2)

	// Hashes should be different
	lth1 := FromBytes(bz1)
	lth2 := FromBytes(bz2)
	assert.NotEqual(t, lth1.limbs, lth2.limbs)
}

func BenchmarkLtHashDeltaParallel(b *testing.B) {
	dbName := "storage"
	numKVs := 1000
	kvs := make([]KVPairWithOldValue, numKVs)
	for i := 0; i < numKVs; i++ {
		key := make([]byte, 52)
		key[0] = byte(i % 256)
		val := make([]byte, 32)
		val[0] = byte(i % 256)
		kvs[i] = KVPairWithOldValue{
			Key:            key,
			Value:          val,
			LastFlushValue: val, // Just for benchmark
			Deleted:        false,
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = ComputeLtHashDeltaParallel(dbName, kvs, runtime.NumCPU())
	}
}
