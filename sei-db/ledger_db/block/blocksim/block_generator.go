package blocksim

import (
	"context"
	"encoding/binary"
	"fmt"

	crand "github.com/sei-protocol/sei-chain/sei-db/common/rand"
)

// Sizes of the consensus artifacts the generated records stand in for: an
// Ed25519 signature is 64 bytes and a SHA-256 digest is 32.
const (
	signatureSizeBytes = 64
	hashSizeBytes      = 32
)

// qcHeaderSizeBytes is the fixed prefix of a QC record's value, holding the
// exclusive end of the range the record covers. A QC is filed under the first
// number of its range, so the end is the one thing resume cannot read back off
// the store itself.
const qcHeaderSizeBytes = 8

// generatedBatch is one write batch: a QC record covering the half-open range
// [first, next), plus one block record for every number in that range.
//
// The store holds values as opaque bytes and never decodes them, so the values
// here carry no structure beyond the QC's range end. They are sized to match the
// consensus records they stand in for, which is what makes the write volume
// representative.
type generatedBatch struct {
	first  uint64
	next   uint64
	blocks [][]byte
	qc     []byte
}

// BlockGenerator asynchronously produces batches and feeds them into a channel.
// The generator stops when the context is cancelled.
//
// This is a DB stress benchmark, so the generator avoids the dominant costs of
// real block production — Ed25519 signing and per-transaction random-byte
// generation — to keep the measurement on the database. Every value is a
// zero-copy sub-slice of a CannedRandom buffer, except a QC's, which is copied
// because its range end has to be written in.
type BlockGenerator struct {
	ctx    context.Context
	config *BlocksimConfig
	rand   *crand.CannedRandom

	// next is the global number the following batch starts at.
	next uint64

	batchChan chan *generatedBatch
}

// NewBlockGenerator creates a BlockGenerator and immediately starts its
// background goroutine. first is the global number to begin at: pass the resume
// point of an existing store, or 0 to start from genesis. It is set before the
// goroutine is launched, so the goroutine observes it without a data race. rand
// must not be shared with any other goroutine.
func NewBlockGenerator(
	ctx context.Context,
	config *BlocksimConfig,
	rand *crand.CannedRandom,
	first uint64,
) *BlockGenerator {
	g := &BlockGenerator{
		ctx:       ctx,
		config:    config,
		rand:      rand,
		next:      first,
		batchChan: make(chan *generatedBatch, config.StagedBlockQueueSize),
	}
	go g.mainLoop()
	return g
}

func (g *BlockGenerator) mainLoop() {
	for {
		batch := g.buildBatch()
		select {
		case <-g.ctx.Done():
			return
		case g.batchChan <- batch:
		}
	}
}

// buildBatch produces the next batch, covering the range that starts where the
// previous one ended so successive QCs stay contiguous.
func (g *BlockGenerator) buildBatch() *generatedBatch {
	first := g.next
	next := first + g.config.BlocksPerQc
	g.next = next

	blocks := make([][]byte, 0, g.config.BlocksPerQc)
	for range g.config.BlocksPerQc {
		blocks = append(blocks, g.rand.Bytes(int(g.config.blockValueBytes()))) //nolint:gosec // bounded by config validation
	}
	return &generatedBatch{first: first, next: next, blocks: blocks, qc: g.makeQCValue(next)}
}

// makeQCValue builds a QC record's value: the exclusive end of the covered range,
// followed by padding the size of the signatures and block-header digests a real
// QC carries. The buffer is allocated rather than sliced out of the CannedRandom
// because the range end has to be written into it and CannedRandom hands out
// immutable sub-slices.
func (g *BlockGenerator) makeQCValue(next uint64) []byte {
	size := int(g.config.qcValueBytes()) //nolint:gosec // bounded by config validation
	value := make([]byte, size)
	binary.BigEndian.PutUint64(value, next)
	copy(value[qcHeaderSizeBytes:], g.rand.Bytes(size-qcHeaderSizeBytes))
	return value
}

// qcRangeEnd reads back the exclusive end of the range a QC record covers.
func qcRangeEnd(value []byte) (uint64, error) {
	if len(value) < qcHeaderSizeBytes {
		return 0, fmt.Errorf("QC record is %d bytes, too short to carry a range end", len(value))
	}
	return binary.BigEndian.Uint64(value), nil
}

// blockHash derives a block record's hash alias from its global number. The store
// requires the alias to be unique, and deriving it from the number keeps that true
// across a restart without having to remember which hashes were already used.
func blockHash(n uint64) []byte {
	hash := make([]byte, hashSizeBytes)
	binary.BigEndian.PutUint64(hash, n)
	return hash
}
