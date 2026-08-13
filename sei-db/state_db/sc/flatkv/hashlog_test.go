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
	require.NoError(t, s.ApplyChangeSets(s.Version()+1, []*proto.NamedChangeSet{makeChangeSet(key, padLeft32(0x33), false)}))
	_, err := s.Commit(s.Version() + 1)
	require.NoError(t, err)
	// What gets reported is whatever the hasher has published, so this test is only about reporting once the
	// hasher has caught up with the block just committed.
	require.NoError(t, s.FlushHashes())

	// Categories: the global root plus one per data DB (metadata DB excluded).
	require.Equal(t, []string{
		"flatKV/root",
		"flatKV/db/account",
		"flatKV/db/code",
		"flatKV/db/storage",
		"flatKV/db/misc",
	}, s.HashCategories())

	logger := newCaptureLogger()
	for _, category := range s.HashCategories() {
		require.NoError(t, logger.RegisterHashType(category))
	}
	require.Len(t, logger.registered, 5)

	require.NoError(t, s.RecordHashes(logger, 1))

	// Every category is reported, and the root matches PublishedHash.
	for _, category := range s.HashCategories() {
		_, ok := logger.hashes[category]
		require.True(t, ok, "expected a hash for %q", category)
	}
	require.Equal(t, s.PublishedHash().Hash, logger.hashes["flatKV/root"])

	// Each reported per-DB hash is the checksum of that DB's accumulated LtHash.
	seed := awaitHashSeed(t, s)
	for _, dir := range dataDBDirs {
		checksum := seed.perDBLtHash[dir].Checksum()
		require.Equal(t, checksum[:], logger.hashes["flatKV/db/"+dir])
	}

	// Homomorphic invariant: the per-DB LtHashes sum to the reported root.
	sum := lthash.New()
	for _, dir := range dataDBDirs {
		sum.MixIn(seed.perDBLtHash[dir])
	}
	sumChecksum := sum.Checksum()
	require.Equal(t, sumChecksum[:], logger.hashes["flatKV/root"])
}
