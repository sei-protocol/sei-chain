package flatkv

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/config"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/ktype"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/lthash"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/hashlog"
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
	// The categories are not registered here: the store registers what it reports when it takes the
	// logger, so a test that registered them itself would be staging an arrangement production never
	// produces.
	logger := newCaptureLogger()

	s := setupTestStoreWithHashLogger(t, config.DefaultTestConfig(t), logger)
	defer func() { require.NoError(t, s.Close()) }()

	// Constructing the store is what puts the columns on the logger, before any block is finalized.
	require.Len(t, logger.registered, 5)

	// Write some EVM storage so the account/storage DBs have non-empty LtHashes.
	key := evmStorageKey(ktype.Address{0x11}, ktype.Slot{0x22})
	require.NoError(t, s.ApplyChangeSets(s.Version()+1, []*proto.NamedChangeSet{makeChangeSet(key, padLeft32(0x33), false)}))
	_, err := s.Commit(s.Version() + 1)
	require.NoError(t, err)

	// Categories: the global root plus one per data DB.
	require.Equal(t, []string{
		"flatKV/root",
		"flatKV/db/account",
		"flatKV/db/code",
		"flatKV/db/storage",
		"flatKV/db/misc",
	}, s.HashCategories())

	require.NoError(t, s.FlushHashes())

	// Every category is reported, and the root matches CommittedRootHash.
	for _, category := range s.HashCategories() {
		_, ok := logger.hashes[category]
		require.True(t, ok, "expected a hash for %q", category)
	}
	require.Equal(t, rootHash(s), logger.hashes["flatKV/root"])

	// Each reported per-DB hash is the checksum of the LtHash that database actually recorded. Read back
	// off disk rather than from the store's load-time copy: the finalizer writes it, so disk is the only
	// place the two can be compared.
	//
	require.NoError(t, s.reloadLocalMeta())

	for _, dir := range dataDBDirs {
		checksum := s.localMeta[dir].LtHash.Checksum()
		require.Equal(t, checksum[:], logger.hashes["flatKV/db/"+dir])
	}

	// Homomorphic invariant: the per-DB LtHashes sum to the committed global LtHash.
	sum := lthash.New()
	for _, dir := range dataDBDirs {
		sum.MixIn(s.localMeta[dir].LtHash)
	}
	require.True(t, sum.Equal(s.maintainedHashes().Global))
}

// TestFlatKVHashesReachARealArchive drives a real hash logger, opened the way the node opens it, and
// requires flatKV's hashes to be readable back off disk afterwards.
//
// The logger is configured with no caller columns, which is what rootmulti's openHashLogger does. Every
// other test in this package supplies a double, and a double cannot tell whether the column a hash is
// reported under exists — so this is the only place the registration path is exercised end to end.
func TestFlatKVHashesReachARealArchive(t *testing.T) {
	const blocks = 2

	archiveDir := t.TempDir()
	hl, err := hashlog.NewHashLogger(hashlog.DefaultHashLoggerConfig(archiveDir, "flatkv-archive-test"))
	require.NoError(t, err)

	s := setupTestStoreWithHashLogger(t, config.DefaultTestConfig(t), hl)
	defer func() { require.NoError(t, s.Close()) }()

	for height := int64(1); height <= blocks; height++ {
		key := evmStorageKey(ktype.Address{0x11}, ktype.Slot{byte(height)})
		changeSets := []*proto.NamedChangeSet{makeChangeSet(key, padLeft32(byte(height)), false)}
		require.NoError(t, s.ApplyChangeSets(height, changeSets), "apply block %d", height)
		_, err := s.Commit(height)
		require.NoError(t, err, "commit block %d", height)

		// The changeset column is the logger's own and only the caller can supply it. Without it no
		// block is ever complete and none reaches disk. baseapp plays this part in production.
		hl.ReportChangeset(uint64(height), changeSets)
	}

	// Hashes are reported off the commit path, and the archive is only sealed by Close.
	require.NoError(t, s.FlushHashes())
	require.NoError(t, hl.Close())

	for height := uint64(1); height <= blocks; height++ {
		reports, err := hashlog.ReadHashForBlock(archiveDir, height)
		require.NoError(t, err)
		require.Len(t, reports, 1, "block %d should appear exactly once in the archive", height)

		for _, category := range hashCategories() {
			require.NotEmpty(t, reports[0].Hashes[category],
				"block %d recorded no %s hash", height, category)
		}
	}
}
