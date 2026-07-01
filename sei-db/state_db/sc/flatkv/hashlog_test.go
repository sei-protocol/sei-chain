package flatkv

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/ktype"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/lthash"
)

// captureLogger is a HashLogger test double that records registered categories and reported hashes.
type captureLogger struct {
	registered map[string]struct{}
	hashes     map[string][]byte
	changesets int
}

func newCaptureLogger() *captureLogger {
	return &captureLogger{registered: map[string]struct{}{}, hashes: map[string][]byte{}}
}

func (c *captureLogger) RegisterHashType(hashType string) error {
	c.registered[hashType] = struct{}{}
	return nil
}

func (c *captureLogger) UnregisterHashType(hashType string) error {
	delete(c.registered, hashType)
	return nil
}

func (c *captureLogger) ReportHash(_ uint64, hashType string, hash []byte) error {
	c.hashes[hashType] = hash
	return nil
}

func (c *captureLogger) ReportChangeset(uint64, []*proto.NamedChangeSet) { c.changesets++ }

func (c *captureLogger) Close() error { return nil }

func TestFlatKVHashReporting(t *testing.T) {
	s := setupTestStore(t)
	defer func() { require.NoError(t, s.Close()) }()

	// Write some EVM storage so the account/storage DBs have non-empty LtHashes.
	key := evmStorageKey(ktype.Address{0x11}, ktype.Slot{0x22})
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{makeChangeSet(key, padLeft32(0x33), false)}))
	_, err := s.Commit()
	require.NoError(t, err)

	// Categories: the global root plus one per data DB (metadata DB excluded).
	require.Equal(t, []string{
		"flatKV/root",
		"flatKV/db/account",
		"flatKV/db/code",
		"flatKV/db/storage",
		"flatKV/db/legacy",
	}, s.HashCategories())

	logger := newCaptureLogger()
	for _, category := range s.HashCategories() {
		require.NoError(t, logger.RegisterHashType(category))
	}
	require.Len(t, logger.registered, 5)

	require.NoError(t, s.RecordHashes(logger, 1))

	// Every category is reported, and the root matches CommittedRootHash.
	for _, category := range s.HashCategories() {
		_, ok := logger.hashes[category]
		require.True(t, ok, "expected a hash for %q", category)
	}
	require.Equal(t, s.CommittedRootHash(), logger.hashes["flatKV/root"])

	// Each reported per-DB hash is the checksum of that DB's committed LtHash.
	for _, dir := range dataDBDirs {
		checksum := s.localMeta[dir].LtHash.Checksum()
		require.Equal(t, checksum[:], logger.hashes["flatKV/db/"+dir])
	}

	// Homomorphic invariant: the per-DB LtHashes sum to the committed global LtHash.
	sum := lthash.New()
	for _, dir := range dataDBDirs {
		sum.MixIn(s.localMeta[dir].LtHash)
	}
	require.True(t, sum.Equal(s.committedLtHash))
}
