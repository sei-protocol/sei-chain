package historical

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/fnv"
	"strings"
)

const (
	DefaultFoundationDBPrefix                     = "sei_history"
	DefaultFoundationDBAPIVersion                 = 730
	DefaultFoundationDBShards                     = 256
	DefaultFoundationDBTransactionTimeoutMS       = 10_000
	DefaultFoundationDBTransactionRetryLimit      = 10
	DefaultFoundationDBTransactionMaxRetryDelayMS = 1_000
	DefaultFoundationDBTransactionSizeLimitBytes  = 9_000_000
	minFoundationDBAPIVersion                     = 610
	minFoundationDBTransactionSizeLimitBytes      = 32
	maxFoundationDBTransactionSizeLimitBytes      = 10_000_000

	foundationDBMutationPrefix = byte('m')
	foundationDBVersionPrefix  = byte('v')
	foundationDBUpgradePrefix  = byte('u')

	foundationDBDeleted byte = 1
	foundationDBAlive   byte = 0
)

const (
	maxFoundationDBUint16Int   = 1<<16 - 1
	maxFoundationDBUint32Int   = 1<<32 - 1
	maxFoundationDBInt64Uint64 = 1<<63 - 1
)

var ErrFoundationDBUnavailable = errors.New("foundationdb support not compiled in; build with -tags foundationdb and install the FoundationDB client library")

type FoundationDBConfig struct {
	Enabled                    bool
	ClusterFile                string
	Prefix                     string
	APIVersion                 int
	Shards                     int
	TransactionTimeoutMS       int
	TransactionRetryLimit      int
	TransactionMaxRetryDelayMS int
	TransactionSizeLimitBytes  int
}

func (c *FoundationDBConfig) ApplyDefaults() {
	if c.Prefix == "" {
		c.Prefix = DefaultFoundationDBPrefix
	}
	if c.APIVersion == 0 {
		c.APIVersion = DefaultFoundationDBAPIVersion
	}
	if c.Shards == 0 {
		c.Shards = DefaultFoundationDBShards
	}
	if c.TransactionTimeoutMS == 0 {
		c.TransactionTimeoutMS = DefaultFoundationDBTransactionTimeoutMS
	}
	if c.TransactionRetryLimit == 0 {
		c.TransactionRetryLimit = DefaultFoundationDBTransactionRetryLimit
	}
	if c.TransactionMaxRetryDelayMS == 0 {
		c.TransactionMaxRetryDelayMS = DefaultFoundationDBTransactionMaxRetryDelayMS
	}
	if c.TransactionSizeLimitBytes == 0 {
		c.TransactionSizeLimitBytes = DefaultFoundationDBTransactionSizeLimitBytes
	}
}

func (c FoundationDBConfig) Configured() bool {
	return c.Enabled ||
		strings.TrimSpace(c.ClusterFile) != "" ||
		strings.TrimSpace(c.Prefix) != ""
}

func (c *FoundationDBConfig) Validate() error {
	if c.APIVersion < 0 || c.APIVersion > DefaultFoundationDBAPIVersion {
		return fmt.Errorf("foundationdb api version must be between %d and %d", minFoundationDBAPIVersion, DefaultFoundationDBAPIVersion)
	}
	if c.APIVersion > 0 && c.APIVersion < minFoundationDBAPIVersion {
		return fmt.Errorf("foundationdb api version must be between %d and %d", minFoundationDBAPIVersion, DefaultFoundationDBAPIVersion)
	}
	if c.Shards < 0 || c.Shards > maxFoundationDBUint16Int {
		return fmt.Errorf("foundationdb shards must be between 1 and %d", maxFoundationDBUint16Int)
	}
	if c.TransactionTimeoutMS < 0 {
		return fmt.Errorf("foundationdb transaction timeout must be non-negative")
	}
	if c.TransactionRetryLimit < 0 {
		return fmt.Errorf("foundationdb transaction retry limit must be non-negative")
	}
	if c.TransactionMaxRetryDelayMS < 0 {
		return fmt.Errorf("foundationdb transaction max retry delay must be non-negative")
	}
	if c.TransactionSizeLimitBytes < 0 {
		return fmt.Errorf("foundationdb transaction size limit must be non-negative")
	}
	if c.TransactionSizeLimitBytes > 0 && c.TransactionSizeLimitBytes < minFoundationDBTransactionSizeLimitBytes {
		return fmt.Errorf("foundationdb transaction size limit must be at least %d bytes", minFoundationDBTransactionSizeLimitBytes)
	}
	if c.TransactionSizeLimitBytes > maxFoundationDBTransactionSizeLimitBytes {
		return fmt.Errorf("foundationdb transaction size limit must be at most %d bytes", maxFoundationDBTransactionSizeLimitBytes)
	}
	return nil
}

type FoundationDBWrite struct {
	Key   []byte
	Value []byte
}

func FoundationDBMutationKey(prefix, storeName string, key []byte, version int64, shards int) []byte {
	rowPrefix := FoundationDBMutationKeyPrefix(prefix, storeName, key, shards)
	return append(rowPrefix, foundationDBInvertedVersion(version)...)
}

func FoundationDBMutationKeyPrefix(prefix, storeName string, key []byte, shards int) []byte {
	shards = normalizeFoundationDBShards(shards)
	shard := foundationDBShard(storeName, key, shards)
	keyspace := foundationDBKeyspace(prefix)
	out := make([]byte, len(keyspace)+1+2+2+len(storeName)+4+len(key))
	copy(out, keyspace)
	offset := len(keyspace)
	out[offset] = foundationDBMutationPrefix
	binary.BigEndian.PutUint16(out[offset+1:], shard)
	binary.BigEndian.PutUint16(out[offset+3:], foundationDBUint16FromBoundedInt(len(storeName)))
	copy(out[offset+5:], storeName)
	keyOffset := offset + 5 + len(storeName)
	binary.BigEndian.PutUint32(out[keyOffset:], foundationDBUint32FromBoundedInt(len(key)))
	copy(out[keyOffset+4:], key)
	return out
}

func FoundationDBVersionKey(prefix string, version int64) []byte {
	rowPrefix := foundationDBVersionKeyPrefix(prefix, VersionBucket(version))
	return append(rowPrefix, foundationDBInvertedVersion(version)...)
}

func FoundationDBUpgradeKey(prefix string, version int64, name string) []byte {
	keyspace := foundationDBKeyspace(prefix)
	out := make([]byte, len(keyspace)+1+8+2+len(name))
	copy(out, keyspace)
	offset := len(keyspace)
	out[offset] = foundationDBUpgradePrefix
	copy(out[offset+1:], foundationDBInvertedVersion(version))
	binary.BigEndian.PutUint16(out[offset+9:], foundationDBUint16FromBoundedInt(len(name)))
	copy(out[offset+11:], name)
	return out
}

func FoundationDBVersionFromKey(prefix string, key []byte) (int64, bool) {
	keyspace := foundationDBKeyspace(prefix)
	if !bytes.HasPrefix(key, keyspace) {
		return 0, false
	}
	rest := key[len(keyspace):]
	switch {
	case len(rest) >= 1+2+8 && rest[0] == foundationDBVersionPrefix:
		return foundationDBDecodeInvertedVersion(rest[3:11])
	case len(rest) >= 8 && rest[0] == foundationDBMutationPrefix:
		return foundationDBDecodeInvertedVersion(rest[len(rest)-8:])
	default:
		return 0, false
	}
}

func FoundationDBMutationValue(value []byte, deleted bool) []byte {
	if deleted {
		return []byte{foundationDBDeleted}
	}
	out := make([]byte, 1+len(value))
	out[0] = foundationDBAlive
	copy(out[1:], value)
	return out
}

func FoundationDBUpgradeValue(renameFrom string, deleted bool) []byte {
	return FoundationDBMutationValue([]byte(renameFrom), deleted)
}

func FoundationDBValueFromKeyValue(prefix string, key, value []byte) (Value, error) {
	version, ok := FoundationDBVersionFromKey(prefix, key)
	if !ok {
		return Value{}, fmt.Errorf("invalid foundationdb mutation key")
	}
	if len(value) == 0 || value[0] != foundationDBAlive {
		return Value{}, ErrNotFound
	}
	return Value{Bytes: bytes.Clone(value[1:]), Version: version}, nil
}

func foundationDBVersionKeyPrefix(prefix string, bucket int) []byte {
	keyspace := foundationDBKeyspace(prefix)
	out := make([]byte, len(keyspace)+1+2)
	copy(out, keyspace)
	offset := len(keyspace)
	out[offset] = foundationDBVersionPrefix
	binary.BigEndian.PutUint16(out[offset+1:], foundationDBUint16FromBoundedInt(bucket))
	return out
}

func foundationDBPrefixEnd(prefix []byte) []byte {
	end := bytes.Clone(prefix)
	for i := len(end) - 1; i >= 0; i-- {
		if end[i] != 0xff {
			end[i]++
			return end[:i+1]
		}
	}
	return nil
}

func foundationDBKeyspace(prefix string) []byte {
	if prefix == "" {
		prefix = DefaultFoundationDBPrefix
	}
	out := make([]byte, len(prefix)+1)
	copy(out, prefix)
	return out
}

func foundationDBInvertedVersion(version int64) []byte {
	out := make([]byte, 8)
	binary.BigEndian.PutUint64(out, ^foundationDBUint64FromNonNegativeInt64(version))
	return out
}

func foundationDBDecodeInvertedVersion(encoded []byte) (int64, bool) {
	version := ^binary.BigEndian.Uint64(encoded)
	if version > maxFoundationDBInt64Uint64 {
		return 0, false
	}
	// #nosec G115 -- version is checked above to fit in int64.
	return int64(version), true
}

func foundationDBShard(storeName string, key []byte, shards int) uint16 {
	h := fnv.New32a()
	_, _ = h.Write([]byte(storeName))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write(key)
	return foundationDBUint16FromBoundedUint32(h.Sum32() % foundationDBUint32FromBoundedInt(shards))
}

func normalizeFoundationDBShards(shards int) int {
	if shards <= 0 {
		return DefaultFoundationDBShards
	}
	if shards > maxFoundationDBUint16Int {
		return maxFoundationDBUint16Int
	}
	return shards
}

func foundationDBUint16FromBoundedInt(value int) uint16 {
	if value < 0 || value > maxFoundationDBUint16Int {
		panic(fmt.Sprintf("foundationdb value %d exceeds uint16", value))
	}
	// #nosec G115 -- value is checked above to fit in uint16.
	return uint16(value)
}

func foundationDBUint32FromBoundedInt(value int) uint32 {
	if value < 0 || value > maxFoundationDBUint32Int {
		panic(fmt.Sprintf("foundationdb value %d exceeds uint32", value))
	}
	// #nosec G115 -- value is checked above to fit in uint32.
	return uint32(value)
}

func foundationDBUint16FromBoundedUint32(value uint32) uint16 {
	if value > maxFoundationDBUint16Int {
		panic(fmt.Sprintf("foundationdb value %d exceeds uint16", value))
	}
	// #nosec G115 -- value is checked above to fit in uint16.
	return uint16(value)
}

func foundationDBUint64FromNonNegativeInt64(value int64) uint64 {
	if value < 0 {
		panic(fmt.Sprintf("foundationdb version %d is negative", value))
	}
	// #nosec G115 -- value is checked above to be non-negative.
	return uint64(value)
}
