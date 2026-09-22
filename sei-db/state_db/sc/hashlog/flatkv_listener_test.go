package hashlog

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/lthash"
)

// flatKVTestHashTypes names the columns a flatKV store's hashes go into. Spelled out rather than
// taken from flatkv.HashTypes, which this package cannot import: a column name that drifts from the
// store's own list has to fail here rather than agree with it by construction.
func flatKVTestHashTypes() []string {
	return []string{
		FlatKVRootHashType,
		FlatKVDBHashPrefix + "account",
		FlatKVDBHashPrefix + "code",
		FlatKVDBHashPrefix + "storage",
		FlatKVDBHashPrefix + "misc",
	}
}

// distinctLtHash returns an LtHash unlike any other seed's, so that a hash recorded under the wrong
// column is visible rather than matching by accident.
func distinctLtHash(t *testing.T, seed byte) *lthash.LtHash {
	t.Helper()
	hash, err := lthash.Unmarshal(bytes.Repeat([]byte{seed}, lthash.LtHashBytes))
	require.NoError(t, err)
	return hash
}

// checksumOf returns an LtHash's checksum as a slice, which is the form a hash is recorded in.
func checksumOf(hash *lthash.LtHash) []byte {
	checksum := hash.Checksum()
	return checksum[:]
}

// flatKVBlockHash returns a block hash with a distinct root and a distinct hash for each of flatKV's
// data databases.
func flatKVBlockHash(t *testing.T, blockNumber int64) *lthash.BlockHash {
	t.Helper()
	return &lthash.BlockHash{
		BlockNumber: blockNumber,
		Global:      distinctLtHash(t, 0x01),
		PerDB: map[string]*lthash.LtHash{
			"account": distinctLtHash(t, 0x10),
			"code":    distinctLtHash(t, 0x11),
			"storage": distinctLtHash(t, 0x12),
			"misc":    distinctLtHash(t, 0x13),
		},
	}
}

// The listener's whole job, checked through a real logger and read back off disk: every column the
// node declares holds the hash it names.
//
// A column left empty is what a mismatch between FlatKVHashTypes and what the store publishes looks
// like, and an empty column is worse than a missing one — it reads as a hash of nothing.
func TestTheListenerFillsEveryColumn(t *testing.T) {
	const block = 7

	archiveDir := t.TempDir()
	cfg := DefaultHashLoggerConfig(archiveDir, "flatkv-listener-test")
	cfg.HashTypes = flatKVTestHashTypes()
	hl, err := NewHashLogger(cfg)
	require.NoError(t, err)

	hash := flatKVBlockHash(t, block)
	require.NoError(t, hl.HashListener(t.Context(), block, hash))

	// The changeset column is the logger's own, and a block is only written once every column has an
	// answer. A nil changeset is how a caller that has none completes the block.
	hl.ReportChangeset(block, nil)
	require.NoError(t, hl.Close())

	reports, err := ReadHashForBlock(archiveDir, block)
	require.NoError(t, err)
	require.Len(t, reports, 1)

	recorded := reports[0].Hashes
	for _, hashType := range flatKVTestHashTypes() {
		require.NotEmpty(t, recorded[hashType], "column %s holds no hash", hashType)
	}
	require.Equal(t, checksumOf(hash.Global), recorded[FlatKVRootHashType])
	for dataDB, dbHash := range hash.PerDB {
		require.Equal(t, checksumOf(dbHash), recorded[FlatKVDBHashPrefix+dataDB],
			"column for %s holds another database's hash", dataDB)
	}
}

// A refusal is reported rather than swallowed: the store registering this listener is what decides
// what a failed hash log costs it.
func TestTheListenerReportsALoggerThatRefuses(t *testing.T) {
	cfg := DefaultHashLoggerConfig(t.TempDir(), "flatkv-listener-test")
	cfg.HashTypes = flatKVTestHashTypes()
	hl, err := NewHashLogger(cfg)
	require.NoError(t, err)
	require.NoError(t, hl.Close())

	err = hl.HashListener(t.Context(), 1, flatKVBlockHash(t, 1))
	require.ErrorContains(t, err, "closed")
}
