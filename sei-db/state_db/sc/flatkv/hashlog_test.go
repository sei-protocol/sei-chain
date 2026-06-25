package flatkv

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-db/proto"
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

	// Write some EVM storage and commit so the store has a non-trivial committed LtHash.
	key := memiavlStorageKey(Address{0x11}, Slot{0x22})
	require.NoError(t, s.ApplyChangeSets([]*proto.NamedChangeSet{makeChangeSet(key, []byte{0x33}, false)}))
	_, err := s.Commit()
	require.NoError(t, err)

	// v6.5.0 flatKV tracks only a global LtHash (no per-DB tracking), so the only category is the root.
	require.Equal(t, []string{"flatKV/root"}, s.HashCategories())

	logger := newCaptureLogger()
	for _, category := range s.HashCategories() {
		require.NoError(t, logger.RegisterHashType(category))
	}
	require.Len(t, logger.registered, 1)

	require.NoError(t, s.RecordHashes(logger, 1))

	// The root category is reported and matches CommittedRootHash.
	_, ok := logger.hashes["flatKV/root"]
	require.True(t, ok, "expected a hash for the flatKV root")
	require.Equal(t, s.CommittedRootHash(), logger.hashes["flatKV/root"])
}
