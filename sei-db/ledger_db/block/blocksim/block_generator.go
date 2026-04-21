package blocksim

import (
	"context"

	"github.com/sei-protocol/sei-chain/sei-db/common/rand"
	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/block"
)

const (
	blockHashType = 'b'
	txHashType    = 't'
)

// Asynchronously generates random blocks and feeds them into a channel.
type BlockGenerator struct {
	ctx    context.Context
	config *BlocksimConfig
	rand   *rand.CannedRandom

	// The next block height to be assigned.
	nextHeight uint64

	// Generated blocks are sent to this channel.
	blocksChan chan *block.BinaryBlock
}

// Creates a new BlockGenerator and immediately starts its background goroutine.
// The generator stops when the context is cancelled.
func NewBlockGenerator(
	ctx context.Context,
	config *BlocksimConfig,
	rng *rand.CannedRandom,
	startHeight uint64,
) *BlockGenerator {
	g := &BlockGenerator{
		ctx:        ctx,
		config:     config,
		rand:       rng,
		nextHeight: startHeight,
		blocksChan: make(chan *block.BinaryBlock, config.StagedBlockQueueSize),
	}
	go g.mainLoop()
	return g
}

// NextBlock blocks until the next generated block is available and returns it.
// Returns nil if the context has been cancelled and no more blocks will be produced.
func (g *BlockGenerator) NextBlock() *block.BinaryBlock {
	select {
	case <-g.ctx.Done():
		return nil
	case blk := <-g.blocksChan:
		return blk
	}
}

func (g *BlockGenerator) mainLoop() {
	for {
		blk := g.buildBlock()
		select {
		case <-g.ctx.Done():
			return
		case g.blocksChan <- blk:
		}
	}
}

func (g *BlockGenerator) buildBlock() *block.BinaryBlock {
	height := g.nextHeight
	g.nextHeight++

	txs := make([]*block.BinaryTransaction, g.config.TransactionsPerBlock)
	for i := uint64(0); i < g.config.TransactionsPerBlock; i++ {
		txID := int64(height)*int64(g.config.TransactionsPerBlock) + int64(i) //nolint:gosec
		txs[i] = &block.BinaryTransaction{
			Hash:        g.rand.Address(txHashType, txID, int(g.config.TransactionHashSize)), //nolint:gosec
			Transaction: g.rand.Bytes(int(g.config.BytesPerTransaction)),                     //nolint:gosec
		}
	}

	blockHash := g.rand.Address(blockHashType, int64(height), int(g.config.BlockHashSize)) //nolint:gosec
	blockData := g.rand.Bytes(int(g.config.ExtraBytesPerBlock))                            //nolint:gosec

	return &block.BinaryBlock{
		Height:       height,
		Hash:         blockHash,
		BlockData:    blockData,
		Transactions: txs,
	}
}
