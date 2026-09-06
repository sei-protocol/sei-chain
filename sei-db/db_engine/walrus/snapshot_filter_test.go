package walrus

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/cockroachdb/pebble"
	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-db/db_engine/walrus/statestub"
)

// retainedTestSnapshot commits blocks to a state stub and retains a checkpoint of it.
//
// It also returns a second, independent checkpoint. A retained snapshot holds pebble's directory lock, so
// a test that wants to read the same tables through different options needs its own copy rather than a
// second handle on that one.
func retainedTestSnapshot(t *testing.T, blockCount int) (snapshot *pebbleSnapshot, control string) {
	t.Helper()

	directory := t.TempDir()
	stub, err := statestub.New(statestub.DefaultConfig(filepath.Join(directory, "stub"), "test", "evm"))
	require.NoError(t, err)
	defer func() { require.NoError(t, stub.Close()) }()

	blocks := generateBlocks(1, blockCount)
	for _, block := range blocks[:len(blocks)-1] {
		require.NoError(t, stub.CommitBlock(block.Number, block.ChangeSets))
	}
	control, _, err = stub.Checkpoint()
	require.NoError(t, err)

	// One more block, so that the checkpoint below lands in a directory of its own.
	last := blocks[len(blocks)-1]
	require.NoError(t, stub.CommitBlock(last.Number, last.ChangeSets))
	checkpoint, blockNumber, err := stub.Checkpoint()
	require.NoError(t, err)

	root := filepath.Join(directory, snapshotsDirName)
	require.NoError(t, os.MkdirAll(root, 0o750))
	snapshot, err = retainSnapshot(root, blockNumber, checkpoint)
	require.NoError(t, err)
	require.NoError(t, os.RemoveAll(checkpoint))
	return snapshot, control
}

// readAbsentKeys asks for keys the stub never wrote, which is the case a bloom filter answers without
// reading a data block.
func readAbsentKeys(t *testing.T, get func(key []byte) (bool, error), count int) {
	t.Helper()

	for index := 0; index < count; index++ {
		found, err := get([]byte(fmt.Sprintf("never-written-%08d", index)))
		require.NoError(t, err)
		require.False(t, found)
	}
}

// TestSnapshotReadsUseTheFilterTheStubWrote covers both halves of the filter at once.
//
// A filter is only useful when the writer emitted one and the reader was configured to resolve it, and
// either half alone fails silently: a filter written and not read is a slowdown and nothing else. The
// negative control is what gives the assertion meaning, since a test that only exercised the configured
// reader would pass just as well if no filter were ever consulted.
func TestSnapshotReadsUseTheFilterTheStubWrote(t *testing.T) {
	const absentKeys = 500

	snapshot, control := retainedTestSnapshot(t, 200)
	defer func() { require.NoError(t, snapshot.Delete()) }()

	readAbsentKeys(t, func(key []byte) (bool, error) {
		_, found, err := snapshot.Get(key)
		return found, err
	}, absentKeys)

	filter := snapshot.database.Metrics().Filter
	require.Positive(t, filter.Hits,
		"the snapshot read no filter: either the stub wrote none or the reader did not resolve it")

	// The same tables, read by a reader that registers no policy, never consult a filter at all.
	unfiltered, err := pebble.Open(control, &pebble.Options{
		ReadOnly: true,
		Logger:   silentPebbleLogger{},
	})
	require.NoError(t, err)
	defer func() { require.NoError(t, unfiltered.Close()) }()

	readAbsentKeys(t, func(key []byte) (bool, error) {
		_, closer, err := unfiltered.Get(key)
		if errors.Is(err, pebble.ErrNotFound) {
			return false, nil
		}
		if err != nil {
			return false, err
		}
		return true, closer.Close()
	}, absentKeys)

	unfilteredMetrics := unfiltered.Metrics().Filter
	require.Zero(t, unfilteredMetrics.Hits+unfilteredMetrics.Misses,
		"a reader with no policy should never consult a filter, so this test would prove nothing")
}

// TestSnapshotAnswersAfterFilteringStillMatchTheStub checks that the filter did not cost any answers.
//
// A bloom filter may only be wrong in one direction. One that reported a key absent would make a walk
// answer ReadAbsent for a key the snapshot holds, which is a wrong answer rather than a slow one.
func TestSnapshotAnswersAfterFilteringStillMatchTheStub(t *testing.T) {
	const blockCount = 200

	snapshot, _ := retainedTestSnapshot(t, blockCount)
	defer func() { require.NoError(t, snapshot.Delete()) }()

	for number := uint64(1); number <= blockCount; number++ {
		key := fmt.Sprintf("cold-%08d", number)
		value, found, err := snapshot.Get([]byte(key))
		require.NoError(t, err)
		require.True(t, found, "the filter lost %s", key)
		require.Equal(t, fmt.Sprintf("cold-%d", number), string(value))
	}
}
