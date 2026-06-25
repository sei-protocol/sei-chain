package memiavl

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-db/proto"
	iavl "github.com/sei-protocol/sei-chain/sei-iavl"
)

// setupCS builds a CommitStore with stores "test" and "other"; "test" has committed data.
func setupCS(t *testing.T) *CommitStore {
	t.Helper()
	dir := t.TempDir()
	cs := NewCommitStore(dir, Config{})
	cs.Initialize([]string{"test", "other"})

	_, err := cs.LoadVersion(0, false)
	require.NoError(t, err)
	t.Cleanup(func() { _ = cs.Close() })

	err = cs.ApplyChangeSets([]*proto.NamedChangeSet{
		{Name: "test", Changeset: iavl.ChangeSet{Pairs: []*iavl.KVPair{
			{Key: []byte("k1"), Value: []byte("v1")},
			{Key: []byte("k2"), Value: []byte("v2")},
			{Key: []byte("k3"), Value: []byte("v3")},
		}}},
	})
	require.NoError(t, err)
	_, err = cs.Commit()
	require.NoError(t, err)
	return cs
}

// captureLogger is a HashLogger test double that records registered categories and reported hashes.
type captureLogger struct {
	registered map[string]struct{}
	hashes     map[string][]byte
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

func (c *captureLogger) ReportChangeset(uint64, []*proto.NamedChangeSet) {}

func (c *captureLogger) Close() error { return nil }

func TestMemIAVLHashReporting(t *testing.T) {
	cs := setupCS(t) // stores "test" and "other"; "test" has committed data

	// One category per tree (no root — that is owned by the cosmos layer).
	require.ElementsMatch(t, []string{"memIAVL/mod/test", "memIAVL/mod/other"}, cs.HashCategories())

	logger := newCaptureLogger()
	for _, category := range cs.HashCategories() {
		require.NoError(t, logger.RegisterHashType(category))
	}
	require.Len(t, logger.registered, 2)

	require.NoError(t, cs.RecordHashes(logger, 1))

	// Each module's reported hash matches its commit info store hash.
	for _, storeInfo := range cs.LastCommitInfo().StoreInfos {
		reported, ok := logger.hashes["memIAVL/mod/"+storeInfo.Name]
		require.True(t, ok, "expected a hash for module %q", storeInfo.Name)
		require.Equal(t, storeInfo.CommitId.Hash, reported)
	}
}

// A store that is not loaded reports no categories and records nothing, without panicking.
func TestMemIAVLHashReportingNilSafe(t *testing.T) {
	var cs *CommitStore
	require.Nil(t, cs.HashCategories())
	logger := newCaptureLogger()
	require.NoError(t, cs.RecordHashes(logger, 1))
	require.Empty(t, logger.hashes)
}
