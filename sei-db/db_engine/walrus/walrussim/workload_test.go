package walrussim

import (
	"encoding/binary"
	"testing"

	"github.com/stretchr/testify/require"
)

// testWorkloadConfig returns a workload small enough to model exhaustively.
func testWorkloadConfig() *Config {
	config := DefaultConfig()
	config.CannedRandomSize = 1 << 20
	config.KeySize = 53
	config.ValueSize = 32
	config.FirstBlock = 1
	config.DeleteRate = 13
	config.KeyClasses = []KeyClass{
		{KeyCount: 50, Period: 1},
		{KeyCount: 200, Period: 7},
		{KeyCount: 500, Period: 97},
	}
	config.NeverWrittenKeyCount = 20
	return config
}

// idOf reads the key id back out of a generated key, which CannedRandom.Address writes at bytes 9 through 17.
func idOf(key []byte) uint64 {
	return binary.BigEndian.Uint64(key[9:17])
}

// TestWorkloadOracleMatchesProducer replays the produced blocks into a plain model and checks that the
// closed form agrees with it at every block, for every key.
//
// This is the load bearing test for the harness: if the schedule and its inverse ever disagree, walrussim
// would report the engine wrong when the engine was right, or worse, right when it was wrong.
func TestWorkloadOracleMatchesProducer(t *testing.T) {
	config := testWorkloadConfig()
	work := newWorkload(config)

	model := map[uint64]string{}
	present := map[uint64]bool{}
	written := map[uint64]bool{}

	for blockNumber := config.FirstBlock; blockNumber < config.FirstBlock+400; blockNumber++ {
		block := work.block(blockNumber)
		require.Equal(t, blockNumber, block.Number)
		require.Len(t, block.ChangeSets, 1)

		for _, pair := range block.ChangeSets[0].Changeset.Pairs {
			id := idOf(pair.Key)
			written[id] = true
			if pair.Delete {
				present[id] = false
				model[id] = ""
				continue
			}
			present[id] = true
			model[id] = string(pair.Value)
		}

		for id := uint64(0); id < work.neverWrittenLimit; id++ {
			value, found, exists := work.expected(id, blockNumber)
			require.Equal(t, present[id], found, "key %d at block %d", id, blockNumber)
			require.Equal(t, written[id], exists,
				"key %d at block %d: presence is whether anything is there to find, value or tombstone",
				id, blockNumber)
			if found {
				require.Equal(t, model[id], string(value), "key %d at block %d", id, blockNumber)
			}
		}
	}
}

// TestWorkloadNeverWrittenKeysAreNeverWritten checks that the reserved id range really is untouched, since a
// read against it is the deepest walk the engine can be asked for.
func TestWorkloadNeverWrittenKeysAreNeverWritten(t *testing.T) {
	config := testWorkloadConfig()
	work := newWorkload(config)

	for blockNumber := config.FirstBlock; blockNumber < config.FirstBlock+500; blockNumber++ {
		for _, pair := range work.block(blockNumber).ChangeSets[0].Changeset.Pairs {
			require.Less(t, idOf(pair.Key), work.liveIDLimit,
				"a reserved id was written at block %d", blockNumber)
		}
	}

	for id := work.liveIDLimit; id < work.neverWrittenLimit; id++ {
		_, found, exists := work.expected(id, 10_000)
		require.False(t, found, "reserved id %d should never hold a value", id)
		require.False(t, exists, "reserved id %d should have nothing on disk at all", id)
	}
}

// TestWorkloadWritesEveryKeyOncePerPeriod checks that a class's keys really are written exactly once every
// period, which is what makes the class mix a dial on how far a backwards walk travels.
func TestWorkloadWritesEveryKeyOncePerPeriod(t *testing.T) {
	config := testWorkloadConfig()
	work := newWorkload(config)

	for _, class := range work.classes {
		writes := map[uint64]int{}
		// Any window of exactly one period must contain exactly one write of each of the class's keys,
		// whatever block the window starts on.
		for offset := uint64(0); offset < class.period; offset++ {
			block := work.block(config.FirstBlock + 500 + offset)
			for _, pair := range block.ChangeSets[0].Changeset.Pairs {
				id := idOf(pair.Key)
				if id >= class.firstID && id < class.endID {
					writes[id]++
				}
			}
		}
		for id := class.firstID; id < class.endID; id++ {
			require.Equal(t, 1, writes[id], "key %d with period %d", id, class.period)
		}
	}
}

// TestWorkloadKeysAreDistinct checks that two ids never collide, since a collision would make the oracle
// disagree with the engine for reasons that have nothing to do with the engine.
func TestWorkloadKeysAreDistinct(t *testing.T) {
	work := newWorkload(testWorkloadConfig())

	seen := map[string]uint64{}
	for id := uint64(0); id < work.neverWrittenLimit; id++ {
		key := string(work.key(id))
		previous, collision := seen[key]
		require.False(t, collision, "ids %d and %d generate the same key", previous, id)
		seen[key] = id
	}
}
