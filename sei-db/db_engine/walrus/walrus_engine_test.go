package walrus

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/sei-protocol/sei-chain/sei-db/db_engine/walrus/statestub"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/stretchr/testify/require"
)

// answer returns what a key held at the end of a block, across every version the model recorded.
func (o oracle) answer(key string, blockNumber uint64) (value string, found bool) {
	for _, version := range o[key] {
		if version.block > blockNumber {
			break
		}
		if version.deleted {
			value, found = "", false
			continue
		}
		value, found = version.value, true
	}
	return value, found
}

// generateBlocks builds a workload with a key written every block, keys written once, keys sharing an eight
// byte prefix, and a key that is deleted and rewritten.
func generateBlocks(firstBlock uint64, count int) []Block {
	blocks := make([]Block, 0, count)
	for index := 0; index < count; index++ {
		number := firstBlock + uint64(index)
		pairs := []*proto.KVPair{
			testPair("hot", fmt.Sprintf("hot-%d", number), false),
			testPair(fmt.Sprintf("cold-%08d", number), fmt.Sprintf("cold-%d", number), false),
			testPair(fmt.Sprintf("shared-prefix-key-%d", number%5),
				fmt.Sprintf("shared-%d", number), false),
		}
		if number%9 == 0 {
			pairs = append(pairs, testPair("flicker", "", true))
		} else {
			pairs = append(pairs, testPair("flicker", fmt.Sprintf("on-%d", number), false))
		}
		blocks = append(blocks, testBlock(number, pairs...))
	}
	return blocks
}

// testConfig returns a configuration whose pods are small enough that a short test produces several.
func testConfig(t *testing.T, directory string) *Config {
	t.Helper()
	config := DefaultConfig(directory, "test", "evm")
	config.TargetPodSize = 2048
	config.PodBuildConcurrency = 3
	config.DisableMetrics = true
	return config
}

// appendAll writes every block and flushes, so all of them are queryable.
func appendAll(t *testing.T, engine Walrus, blocks []Block) {
	t.Helper()
	for _, block := range blocks {
		require.NoError(t, engine.AppendBlock(block))
	}
	require.NoError(t, engine.Flush())
}

// requireAgreesWithOracle checks every key at every block against the model.
func requireAgreesWithOracle(t *testing.T, engine Walrus, model oracle, first uint64, last uint64) {
	t.Helper()
	for key := range model {
		for blockNumber := first; blockNumber <= last; blockNumber++ {
			value, status, err := engine.Get([]byte(key), blockNumber)
			require.NoError(t, err)
			if status == ReadTooOld || status == ReadTooNew {
				continue
			}
			wantValue, wantFound := model.answer(key, blockNumber)
			if !wantFound {
				require.Equal(t, ReadAbsent, status, "key %q at block %d", key, blockNumber)
				continue
			}
			require.Equal(t, ReadFound, status, "key %q at block %d", key, blockNumber)
			require.Equal(t, wantValue, string(value), "key %q at block %d", key, blockNumber)
		}
	}
}

func TestEngineEndToEnd(t *testing.T) {
	directory := t.TempDir()
	engine, err := New(testConfig(t, directory))
	require.NoError(t, err)
	defer func() { require.NoError(t, engine.Close()) }()

	blocks := generateBlocks(1, 120)
	appendAll(t, engine, blocks)

	ok, first, last, err := engine.QueryableBounds()
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, uint64(1), first)
	require.Equal(t, uint64(120), last)

	requireAgreesWithOracle(t, engine, newOracle(blocks), first, last)

	// A key nothing ever wrote is absent at every height, and never a value.
	for _, blockNumber := range []uint64{1, 60, 120} {
		value, status, err := engine.Get([]byte("never-written"), blockNumber)
		require.NoError(t, err)
		require.Equal(t, ReadAbsent, status)
		require.Nil(t, value)
	}

	// A block above what has been written is not an absence, it is a block this engine cannot answer for.
	_, status, err := engine.Get([]byte("hot"), 5000)
	require.NoError(t, err)
	require.Equal(t, ReadTooNew, status)
}

func TestEngineReopen(t *testing.T) {
	directory := t.TempDir()
	blocks := generateBlocks(1, 80)

	engine, err := New(testConfig(t, directory))
	require.NoError(t, err)
	appendAll(t, engine, blocks)
	require.NoError(t, engine.Close())

	reopened, err := New(testConfig(t, directory))
	require.NoError(t, err)
	defer func() { require.NoError(t, reopened.Close()) }()

	ok, first, last, err := reopened.QueryableBounds()
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, uint64(1), first)
	require.Equal(t, uint64(80), last)
	requireAgreesWithOracle(t, reopened, newOracle(blocks), first, last)

	// Appending continues from where the previous session stopped.
	more := generateBlocks(81, 20)
	appendAll(t, reopened, more)
	_, _, last, err = reopened.QueryableBounds()
	require.NoError(t, err)
	require.Equal(t, uint64(100), last)
}

func TestEngineTruncatesPodsAboveAGap(t *testing.T) {
	directory := t.TempDir()
	engine, err := New(testConfig(t, directory))
	require.NoError(t, err)
	appendAll(t, engine, generateBlocks(1, 120))
	require.NoError(t, engine.Close())

	podDirectory := filepath.Join(directory, podsDirName)
	names, err := listDirectory(podDirectory)
	require.NoError(t, err)

	infos := make([]*PodInfo, 0, len(names))
	for _, name := range names {
		if info, ok := ParsePodName(name); ok {
			infos = append(infos, info)
		}
	}
	require.Greater(t, len(infos), 2, "the test needs several pods to punch a hole in")

	// Remove the index of a middle pod, which is what an interrupted build would have left.
	holed := infos[len(infos)/2]
	require.NoError(t, os.Remove(holed.IndexPath(podDirectory)))

	reopened, err := New(testConfig(t, directory))
	require.NoError(t, err)
	defer func() { require.NoError(t, reopened.Close()) }()

	_, _, last, err := reopened.QueryableBounds()
	require.NoError(t, err)
	require.Equal(t, holed.FirstBlock-1, last, "everything at and above the hole should be gone")

	remaining, err := listDirectory(podDirectory)
	require.NoError(t, err)
	for _, name := range remaining {
		info, ok := ParsePodName(name)
		if !ok {
			continue
		}
		require.Less(t, info.FirstBlock, holed.FirstBlock)
	}
}

func TestEngineAnswersFromTheSnapshotFloor(t *testing.T) {
	directory := t.TempDir()
	config := testConfig(t, directory)
	config.RetentionBlocks = 40

	engine, err := New(config)
	require.NoError(t, err)
	defer func() { require.NoError(t, engine.Close()) }()

	stubConfig := statestub.DefaultConfig(filepath.Join(directory, "stub"), "test", "evm")
	stub, err := statestub.New(stubConfig)
	require.NoError(t, err)
	defer func() { require.NoError(t, stub.Close()) }()

	blocks := generateBlocks(1, 200)
	model := newOracle(blocks)

	for index, block := range blocks {
		require.NoError(t, engine.AppendBlock(block))
		require.NoError(t, stub.CommitBlock(block.Number, block.ChangeSets))

		// Snapshot every 25 blocks, which lands snapshots inside pods rather than on their boundaries.
		if (index+1)%25 == 0 {
			require.NoError(t, engine.Flush())
			checkpoint, checkpointBlock, err := stub.Checkpoint()
			require.NoError(t, err)
			require.NoError(t, engine.RetainSnapshot(checkpointBlock, checkpoint))
			require.NoError(t, os.RemoveAll(checkpoint))
		}
	}
	require.NoError(t, engine.Flush())

	ok, first, last, err := engine.QueryableBounds()
	require.NoError(t, err)
	require.True(t, ok)
	require.Greater(t, first, uint64(1), "retention should have dropped the oldest pods")

	// Every answer still matches, including the ones that now come out of a snapshot rather than a pod.
	requireAgreesWithOracle(t, engine, model, first, last)

	// A cold key whose only write predates the retained pods is answered by the floor snapshot, not lost.
	coldKey := fmt.Sprintf("cold-%08d", first+1)
	value, status, err := engine.Get([]byte(coldKey), last)
	require.NoError(t, err)
	require.Equal(t, ReadFound, status)
	require.Equal(t, fmt.Sprintf("cold-%d", first+1), string(value))
}
