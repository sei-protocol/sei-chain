package walrus

import (
	"testing"

	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/stretchr/testify/require"
)

func TestAccumulatorCutsWhenTheNextBlockWouldNotFit(t *testing.T) {
	config := DefaultConfig(t.TempDir(), "test", "evm")
	config.TargetPodSize = 400
	accumulator := newPodAccumulator(config)

	// Each block encodes to well under the target, so several accumulate before one is cut.
	var cut [][]Block
	for number := uint64(1); number <= 20; number++ {
		pod, err := accumulator.Add(testBlock(number, testPair("key", "value", false)))
		require.NoError(t, err)
		if pod != nil {
			cut = append(cut, pod)
		}
	}
	require.NotEmpty(t, cut, "no pod was ever cut")

	// A cut pod is contiguous, and the block that triggered the cut starts the next one rather than being
	// split across the boundary.
	expected := uint64(1)
	for _, pod := range cut {
		for _, block := range pod {
			require.Equal(t, expected, block.Number)
			expected++
		}
	}

	remaining := accumulator.Drain()
	require.NotEmpty(t, remaining)
	require.Equal(t, expected, remaining[0].Number)
	require.Nil(t, accumulator.Drain(), "a drained accumulator has nothing left")
}

func TestAccumulatorRejectsGapsAndOversizedBlocks(t *testing.T) {
	config := DefaultConfig(t.TempDir(), "test", "evm")
	config.TargetPodSize = 4096
	accumulator := newPodAccumulator(config)

	_, err := accumulator.Add(testBlock(10, testPair("key", "value", false)))
	require.NoError(t, err)

	_, err = accumulator.Add(testBlock(12, testPair("key", "value", false)))
	require.ErrorContains(t, err, "does not follow")

	// A block too large for an empty pod cannot be written at all: its entries could not be addressed by the
	// uint32 offset a pod index records.
	huge := &proto.KVPair{Key: []byte("huge"), Value: make([]byte, 8192)}
	_, err = accumulator.Add(testBlock(11, huge))
	require.ErrorContains(t, err, "more than the")
}

func TestEncodedBlockSizeCoversWhatIsWritten(t *testing.T) {
	directory := t.TempDir()
	blocks := []Block{
		testBlock(1, testPair("alpha", "one", false), testPair("beta", "two", true)),
		testBlock(2, testPair("gamma", "three", false)),
	}

	estimated := uint64(0)
	for _, block := range blocks {
		estimated += encodedBlockSize(block)
	}

	config := DefaultConfig(directory, "test", "evm")
	pod, err := newPodBuilder(directory, config).Build(blocks)
	require.NoError(t, err)

	// The estimate decides where a pod is cut, so it must never fall short of what is actually written.
	actual := pod.Data.Size() - int64(podDataHeaderSize)
	require.GreaterOrEqual(t, int64(estimated), actual,
		"the size estimate is below what was written, so a pod could overrun its cap")
}
