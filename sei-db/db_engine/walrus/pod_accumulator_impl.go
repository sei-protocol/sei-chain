package walrus

import (
	"encoding/binary"
	"fmt"
)

var _ PodAccumulator = (*podAccumulator)(nil)

// podAccumulator gathers blocks until they fill a pod.
type podAccumulator struct {
	// The size the accumulated blocks may reach before a pod is cut.
	targetSize uint64

	// The blocks gathered so far, which become the next pod.
	blocks []Block

	// What those blocks will occupy in a pod's data section.
	size uint64

	// The last block added, used to enforce contiguity across a pod boundary as well as within a pod.
	lastBlock uint64

	// Whether any block has been added, since block 0 is a legitimate first block.
	started bool
}

// Add stores a block, returning the pod it completed if it did not fit alongside what came before.
func (a *podAccumulator) Add(block Block) (pod []Block, err error) {
	if a.started && block.Number != a.lastBlock+1 {
		return nil, fmt.Errorf("block %d does not follow block %d", block.Number, a.lastBlock)
	}

	size := encodedBlockSize(block)
	if size > a.targetSize {
		return nil, fmt.Errorf("block %d encodes to %d bytes, more than the %d byte pod size",
			block.Number, size, a.targetSize)
	}

	if len(a.blocks) > 0 && a.size+size > a.targetSize {
		pod = a.blocks
		a.blocks = nil
		a.size = 0
	}

	a.blocks = append(a.blocks, block)
	a.size += size
	a.lastBlock = block.Number
	a.started = true
	return pod, nil
}

// Drain returns the blocks gathered so far and resets, or nil if there are none.
func (a *podAccumulator) Drain() []Block {
	if len(a.blocks) == 0 {
		return nil
	}
	pod := a.blocks
	a.blocks = nil
	a.size = 0
	return pod
}

// newPodAccumulator creates an accumulator that cuts a pod once its blocks reach the configured size.
func newPodAccumulator(config *Config) *podAccumulator {
	return &podAccumulator{targetSize: config.TargetPodSize}
}

// encodedBlockSize reports what a block will occupy in a pod's data section.
//
// The block delta is counted at its widest rather than at the width it will actually take, because a block
// that starts a fresh pod has a smaller delta than the same block appended to the pod being filled. Erring
// wide by a few bytes per block cuts pods marginally early, where erring narrow would overrun the size cap.
func encodedBlockSize(block Block) uint64 {
	size := uint64(binary.MaxVarintLen64)
	entries := uint64(0)
	for _, changeSet := range block.ChangeSets {
		for _, pair := range changeSet.Changeset.Pairs {
			entries++
			size += uvarintLen(uint64(len(pair.Key))) + uint64(len(pair.Key))
			size++
			size += uvarintLen(uint64(len(pair.Value))) + uint64(len(pair.Value))
		}
	}
	// The entry count prefix, plus the trailing CRC over the whole block record.
	return size + uvarintLen(entries) + 4
}

// uvarintLen reports how many bytes binary.AppendUvarint will write for value.
func uvarintLen(value uint64) uint64 {
	length := uint64(1)
	for value >= 0x80 {
		value >>= 7
		length++
	}
	return length
}
