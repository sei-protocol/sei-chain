package vault

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/giga/apphash"
	"github.com/sei-protocol/sei-chain/sei-db/seiwal"
)

// testRecord returns a record for blockHeight whose hashes are derived from it.
func testRecord(blockHeight uint64) *apphash.AppHashData {
	var hash [32]byte
	hash[0] = byte(blockHeight)
	hash[1] = byte(blockHeight >> 8)
	return apphash.NewAppHashData(7, blockHeight, hash, hash, hash, hash, hash)
}

// testRecords returns testRecord() for every height from first through last.
func testRecords(first uint64, last uint64) []*apphash.AppHashData {
	records := make([]*apphash.AppHashData, 0, last-first+1)
	for height := first; height <= last; height++ {
		records = append(records, testRecord(height))
	}
	return records
}

// openTestVault opens a vault in dir that is closed when the test ends.
func openTestVault(t *testing.T, dir string) *StandardHashVault {
	t.Helper()
	v, err := NewStandardHashVault(*seiwal.DefaultConfig(dir, "test_app_hash_vault"))
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

// requireSameRecords fails unless expected and actual hold the same records in the same order.
func requireSameRecords(t *testing.T, expected []*apphash.AppHashData, actual []*apphash.AppHashData) {
	t.Helper()
	require.Len(t, actual, len(expected))
	for i := range expected {
		require.Equal(t, expected[i].Serialize(), actual[i].Serialize(), "record %d", i)
	}
}

func TestEmptyVault(t *testing.T) {
	v := openTestVault(t, t.TempDir())
	ok, _, _, err := v.Bounds()
	require.NoError(t, err)
	require.False(t, ok)
	_, err = v.Iterator(1, 1)
	require.Error(t, err)
}

func TestAppendAndIterate(t *testing.T) {
	v := openTestVault(t, t.TempDir())
	require.NoError(t, v.Append(testRecords(5, 9)))
	require.NoError(t, v.Append(testRecords(10, 12)))

	ok, lowest, highest, err := v.Bounds()
	require.NoError(t, err)
	require.True(t, ok)
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
	require.NoError(t, v.Prune(6))

	ok, lowest, highest, err := v.Bounds()
	require.NoError(t, err)
	require.True(t, ok)
	require.LessOrEqual(t, lowest, uint64(6))
	require.Equal(t, uint64(10), highest)
	requireSameRecords(t, testRecords(6, 10), readAll(t, v, 6, 10))
}

func TestRecordCodec(t *testing.T) {
	record := testRecord(42)
	decoded, err := deserializeRecord(serializeRecord(record))
	require.NoError(t, err)
	require.Equal(t, record.Serialize(), decoded.Serialize())
	require.Equal(t, record.AppHash(), decoded.AppHash())
}

func TestRecordCodecRejectsMalformedData(t *testing.T) {
	data := serializeRecord(testRecord(42))

	_, err := deserializeRecord(nil)
	require.Error(t, err, "empty")

	wrongVersion := append([]byte{}, data...)
	wrongVersion[0] = recordVersion + 1
	_, err = deserializeRecord(wrongVersion)
	require.Error(t, err, "unsupported record version")

	_, err = deserializeRecord(data[:len(data)-1])
	require.Error(t, err, "truncated")
}

func TestConfigRequiresPath(t *testing.T) {
	_, err := NewStandardHashVault(*seiwal.DefaultConfig("", "test_app_hash_vault"))
	require.Error(t, err)
}

func TestGapsRejectedEvenIfPermitted(t *testing.T) {
	config := seiwal.DefaultConfig(t.TempDir(), "test_app_hash_vault")
	config.PermitGaps = true
	v, err := NewStandardHashVault(*config)
	require.NoError(t, err)
	t.Cleanup(func() { _ = v.Close() })

	require.NoError(t, v.Append(testRecords(1, 3)))
	require.Error(t, v.Append(testRecords(5, 5)))
	require.True(t, config.PermitGaps, "the caller's config is left unchanged")
}
