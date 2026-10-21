package vault

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/giga/apphash"
	"github.com/sei-protocol/sei-chain/sei-db/seiwal"
)

// The EVM chain ID of every test record.
const testChainID uint64 = 7

// testRecord returns a record for blockHeight whose hashes are derived from it.
func testRecord(blockHeight uint64) *apphash.AppHashData {
	var hash [32]byte
	hash[0] = byte(blockHeight)
	hash[1] = byte(blockHeight >> 8)
	return apphash.NewAppHashData(testChainID, blockHeight, hash, hash, hash, hash, hash)
}

// testRecords returns testRecord() for every height from first through last.
func testRecords(first uint64, last uint64) []*apphash.AppHashData {
	records := make([]*apphash.AppHashData, 0, last-first+1)
	for height := first; height <= last; height++ {
		records = append(records, testRecord(height))
	}
	return records
}

// testConfig returns the default WAL configuration for a vault in dir.
func testConfig(dir string) *seiwal.Config {
	return seiwal.DefaultConfig(dir, "test_app_hash_vault")
}

// openTestVault opens a vault in dir with giga activation height 1 that is closed when the test ends.
func openTestVault(t *testing.T, dir string) *StandardHashVault {
	t.Helper()
	return openTestVaultAt(t, dir, 1)
}

// openTestVaultAt opens a vault in dir with giga activation height gigaActivationHeight that is closed when the test
// ends.
func openTestVaultAt(t *testing.T, dir string, gigaActivationHeight uint64) *StandardHashVault {
	t.Helper()
	v, err := NewStandardHashVault(*testConfig(dir), testChainID, gigaActivationHeight)
	require.NoError(t, err)
	t.Cleanup(func() { _ = v.Close() })
	return v
}

// readAll returns every record from start through end.
func readAll(t *testing.T, v HashVault, start uint64, end uint64) []*apphash.AppHashData {
	t.Helper()
	it, err := v.Iterator(start, end)
	require.NoError(t, err)
	defer func() { require.NoError(t, it.Close()) }()
	var records []*apphash.AppHashData
	for {
		ok, err := it.Next()
		require.NoError(t, err)
		if !ok {
			return records
		}
		records = append(records, it.Entry())
	}
}

// mustSerialize returns the serialization of record.
func mustSerialize(t *testing.T, record *apphash.AppHashData) []byte {
	t.Helper()
	data, err := record.Serialize()
	require.NoError(t, err)
	return data
}

// requireSameRecords fails unless expected and actual hold the same records in the same order.
func requireSameRecords(t *testing.T, expected []*apphash.AppHashData, actual []*apphash.AppHashData) {
	t.Helper()
	require.Len(t, actual, len(expected))
	for i := range expected {
		require.Equal(t, mustSerialize(t, expected[i]), mustSerialize(t, actual[i]), "record %d", i)
	}
}

// requireZeroHashRecord fails unless record is the record of the block before gigaActivationHeight.
func requireZeroHashRecord(t *testing.T, record *apphash.AppHashData, gigaActivationHeight uint64) {
	t.Helper()
	require.Equal(t, testChainID, record.ChainID())
	require.Equal(t, gigaActivationHeight-1, record.BlockHeight())
	require.Equal(t, [32]byte{}, record.AppHash())
}

func TestEmptyVault(t *testing.T) {
	v := openTestVault(t, t.TempDir())
	lowest, highest, err := v.Bounds()
	require.NoError(t, err)
	require.Equal(t, uint64(0), lowest)
	require.Equal(t, uint64(0), highest)
	_, err = v.Iterator(1, 1)
	require.Error(t, err)
}

func TestEmptyVaultKnowsBlockBeforeGigaActivation(t *testing.T) {
	dir := t.TempDir()
	v := openTestVaultAt(t, dir, 5)
	lowest, highest, err := v.Bounds()
	require.NoError(t, err)
	require.Equal(t, uint64(4), lowest)
	require.Equal(t, uint64(4), highest)
	head, err := v.GetLatestBlock()
	require.NoError(t, err)
	require.Equal(t, uint64(4), head)

	records := readAll(t, v, 4, 4)
	require.Len(t, records, 1)
	requireZeroHashRecord(t, records[0], 5)
	_, err = v.Iterator(3, 4)
	require.Error(t, err, "start below the block before the giga activation height")
	_, err = v.Iterator(4, 5)
	require.Error(t, err, "end above the highest known block")

	require.NoError(t, v.Close())
	lowest, highest, err = GetBounds(*testConfig(dir), 5)
	require.NoError(t, err)
	require.Equal(t, uint64(4), lowest)
	require.Equal(t, uint64(4), highest)
}

func TestBoundsStartBeforeGigaActivation(t *testing.T) {
	v := openTestVaultAt(t, t.TempDir(), 5)
	require.NoError(t, v.Append(testRecords(5, 9)))

	lowest, highest, err := v.Bounds()
	require.NoError(t, err)
	require.Equal(t, uint64(4), lowest)
	require.Equal(t, uint64(9), highest)

	records := readAll(t, v, 4, 9)
	require.Len(t, records, 6)
	requireZeroHashRecord(t, records[0], 5)
	requireSameRecords(t, testRecords(5, 9), records[1:])
}

func TestAppendRejectsZeroHashRecord(t *testing.T) {
	v := openTestVaultAt(t, t.TempDir(), 5)
	require.Error(t, v.Append([]*apphash.AppHashData{apphash.NewZeroAppHashData(testChainID, 5)}))
}

func TestZeroGigaActivationHeightFails(t *testing.T) {
	_, err := NewStandardHashVault(*testConfig(t.TempDir()), testChainID, 0)
	require.Error(t, err)
	_, _, err = GetBounds(*testConfig(t.TempDir()), 0)
	require.Error(t, err)
}

func TestAppendAndIterate(t *testing.T) {
	v := openTestVault(t, t.TempDir())
	require.NoError(t, v.Append(testRecords(5, 9)))
	require.NoError(t, v.Append(testRecords(10, 12)))

	lowest, highest, err := v.Bounds()
	require.NoError(t, err)
	require.Equal(t, uint64(5), lowest)
	require.Equal(t, uint64(12), highest)

	requireSameRecords(t, testRecords(5, 12), readAll(t, v, 5, 12))
	requireSameRecords(t, testRecords(7, 10), readAll(t, v, 7, 10))
}

func TestRecordsSurviveReopen(t *testing.T) {
	dir := t.TempDir()
	v := openTestVault(t, dir)
	require.NoError(t, v.Append(testRecords(1, 20)))
	require.NoError(t, v.Close())

	reopened := openTestVault(t, dir)
	requireSameRecords(t, testRecords(1, 20), readAll(t, reopened, 1, 20))
	require.NoError(t, reopened.Append(testRecords(21, 22)))
	requireSameRecords(t, testRecords(19, 22), readAll(t, reopened, 19, 22))
}

func TestAppendRejectsGap(t *testing.T) {
	v := openTestVault(t, t.TempDir())
	require.NoError(t, v.Append(testRecords(1, 3)))
	require.Error(t, v.Append(testRecords(5, 5)))
}

func TestAppendRejectsGapAfterReopen(t *testing.T) {
	dir := t.TempDir()
	v := openTestVault(t, dir)
	require.NoError(t, v.Append(testRecords(1, 3)))
	require.NoError(t, v.Close())

	reopened := openTestVault(t, dir)
	require.Error(t, reopened.Append(testRecords(5, 5)))
}

func TestIteratorRange(t *testing.T) {
	v := openTestVault(t, t.TempDir())
	require.NoError(t, v.Append(testRecords(5, 9)))

	_, err := v.Iterator(4, 9)
	require.Error(t, err, "start below the lowest record")
	_, err = v.Iterator(5, 10)
	require.Error(t, err, "end above the highest record")
}

func TestIteratorUnaffectedByLaterAppends(t *testing.T) {
	v := openTestVault(t, t.TempDir())
	require.NoError(t, v.Append(testRecords(1, 5)))

	it, err := v.Iterator(1, 5)
	require.NoError(t, err)
	defer func() { require.NoError(t, it.Close()) }()
	require.NoError(t, v.Append(testRecords(6, 10)))

	var heights []uint64
	for {
		ok, err := it.Next()
		require.NoError(t, err)
		if !ok {
			break
		}
		heights = append(heights, it.Entry().BlockHeight())
	}
	require.Equal(t, []uint64{1, 2, 3, 4, 5}, heights)
}

func TestPruneKeepsRecordsAtAndAboveHeight(t *testing.T) {
	v := openTestVault(t, t.TempDir())
	require.NoError(t, v.Append(testRecords(1, 10)))
	require.NoError(t, v.PruneHistory(6))

	lowest, highest, err := v.Bounds()
	require.NoError(t, err)
	require.LessOrEqual(t, lowest, uint64(6))
	require.Equal(t, uint64(10), highest)
	requireSameRecords(t, testRecords(6, 10), readAll(t, v, 6, 10))
}

// openOneRecordPerFileVault opens a vault in dir with giga activation height 1, closed when the test ends, that
// seals a file after every record, so a prune removes every record below its height.
func openOneRecordPerFileVault(t *testing.T, dir string) *StandardHashVault {
	t.Helper()
	config := testConfig(dir)
	config.TargetFileSize = 1
	v, err := NewStandardHashVault(*config, testChainID, 1)
	require.NoError(t, err)
	t.Cleanup(func() { _ = v.Close() })
	return v
}

func TestIteratorFromPrunedBlockFails(t *testing.T) {
	v := openOneRecordPerFileVault(t, t.TempDir())
	require.NoError(t, v.Append(testRecords(1, 10)))
	require.NoError(t, v.PruneHistory(6))

	// Bounds() is handled after the prune, so it observes the records removed.
	lowest, _, err := v.Bounds()
	require.NoError(t, err)
	require.Equal(t, uint64(6), lowest)

	_, err = v.Iterator(3, 10)
	require.Error(t, err)
	requireSameRecords(t, testRecords(6, 10), readAll(t, v, 6, 10))
}

func TestPruningPastGigaActivationForgetsBlockBeforeIt(t *testing.T) {
	v := openOneRecordPerFileVault(t, t.TempDir())
	require.NoError(t, v.Append(testRecords(1, 10)))
	lowest, _, err := v.Bounds()
	require.NoError(t, err)
	require.Equal(t, uint64(0), lowest)

	require.NoError(t, v.PruneHistory(2))
	lowest, _, err = v.Bounds()
	require.NoError(t, err)
	require.Equal(t, uint64(2), lowest)
	_, err = v.Iterator(0, 10)
	require.Error(t, err)
	_, err = v.Iterator(0, 0)
	require.Error(t, err)
}

func TestPruneAboveHeadKeepsNewestRecord(t *testing.T) {
	v := openOneRecordPerFileVault(t, t.TempDir())
	require.NoError(t, v.Append(testRecords(1, 10)))
	require.NoError(t, v.PruneHistory(100))

	lowest, highest, err := v.Bounds()
	require.NoError(t, err)
	require.Equal(t, uint64(10), lowest)
	require.Equal(t, uint64(10), highest)
	requireSameRecords(t, testRecords(10, 10), readAll(t, v, 10, 10))
}

func TestHeadFollowsAppends(t *testing.T) {
	v := openTestVault(t, t.TempDir())
	require.NoError(t, v.Append(testRecords(5, 9)))
	head, err := v.GetLatestBlock()
	require.NoError(t, err)
	require.Equal(t, uint64(9), head)

	require.NoError(t, v.Append(testRecords(10, 12)))
	head, err = v.GetLatestBlock()
	require.NoError(t, err)
	require.Equal(t, uint64(12), head)
}

func TestHeadSurvivesReopen(t *testing.T) {
	dir := t.TempDir()
	v := openTestVaultAt(t, dir, 3)
	require.NoError(t, v.Append(testRecords(3, 7)))
	require.NoError(t, v.Close())

	v = openTestVaultAt(t, dir, 3)
	head, err := v.GetLatestBlock()
	require.NoError(t, err)
	require.Equal(t, uint64(7), head)
}

func TestRollbackFloor(t *testing.T) {
	v := openTestVault(t, t.TempDir())
	require.NoError(t, v.Append(testRecords(1, 4)))
	require.Equal(t, uint64(4), v.GetRollbackFloor(0))
	require.Equal(t, uint64(1), v.GetRollbackFloor(3))
	require.Equal(t, uint64(0), v.GetRollbackFloor(4))
	require.Equal(t, uint64(0), v.GetRollbackFloor(10))
}

func TestRecordCodec(t *testing.T) {
	record := testRecord(42)
	data, err := serializeRecord(record)
	require.NoError(t, err)
	decoded, err := deserializeRecord(data)
	require.NoError(t, err)
	require.Equal(t, mustSerialize(t, record), mustSerialize(t, decoded))
	require.Equal(t, record.AppHash(), decoded.AppHash())
}

func TestRecordCodecRejectsMalformedData(t *testing.T) {
	data, err := serializeRecord(testRecord(42))
	require.NoError(t, err)

	_, err = deserializeRecord(nil)
	require.Error(t, err, "empty")

	wrongVersion := append([]byte{}, data...)
	wrongVersion[0] = recordVersion + 1
	_, err = deserializeRecord(wrongVersion)
	require.Error(t, err, "unsupported record version")

	_, err = deserializeRecord(data[:len(data)-1])
	require.Error(t, err, "truncated")
}

func TestConfigRequiresPath(t *testing.T) {
	_, err := NewStandardHashVault(*testConfig(""), testChainID, 1)
	require.Error(t, err)
}

func TestGapsRejectedEvenIfPermitted(t *testing.T) {
	config := testConfig(t.TempDir())
	config.PermitGaps = true
	v, err := NewStandardHashVault(*config, testChainID, 1)
	require.NoError(t, err)
	t.Cleanup(func() { _ = v.Close() })

	require.NoError(t, v.Append(testRecords(1, 3)))
	require.Error(t, v.Append(testRecords(5, 5)))
	require.True(t, config.PermitGaps, "the caller's config is left unchanged")
}

func TestGetBoundsMatchesBounds(t *testing.T) {
	dir := t.TempDir()
	v := openTestVault(t, dir)
	require.NoError(t, v.Append(testRecords(3, 7)))
	require.NoError(t, v.Close())

	lowest, highest, err := GetBounds(*testConfig(dir), 1)
	require.NoError(t, err)
	require.Equal(t, uint64(3), lowest)
	require.Equal(t, uint64(7), highest)
}

func TestGetBoundsFailsWhileVaultIsOpen(t *testing.T) {
	dir := t.TempDir()
	openTestVault(t, dir)
	_, _, err := GetBounds(*testConfig(dir), 1)
	require.Error(t, err)
}
