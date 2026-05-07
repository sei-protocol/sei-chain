package blocksim

import (
	"context"
	"time"

	"github.com/sei-protocol/sei-chain/sei-db/common/rand"
	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/block"
)

const (
	blockHashType = 'b'
	txHashType    = 't'
)

// genTx is a synthetic transaction that satisfies block.Transaction.
type genTx struct {
	hash   []byte
	bytes  []byte
	height uint64
	index  uint32
}

func (t *genTx) Hash() []byte           { return t.hash }
func (t *genTx) Bytes() []byte          { return t.bytes }
func (t *genTx) Result() ([]byte, bool) { return nil, false }
func (t *genTx) Height() uint64         { return t.height }
func (t *genTx) Index() uint32          { return t.index }

// genBlock is a synthetic block that satisfies block.Block. extra is held to
// simulate block-level metadata bytes — the BlockDB contract has no field for
// it, but the bytes still occupy memory (and serialized space, for backends
// that materialize the whole Block).
type genBlock struct {
	hash   []byte
	height uint64
	time   time.Time
	txs    []block.Transaction
	extra  []byte
}

func (b *genBlock) Hash() []byte                      { return b.hash }
func (b *genBlock) Height() uint64                    { return b.height }
func (b *genBlock) Time() time.Time                   { return b.time }
func (b *genBlock) Transactions() []block.Transaction { return b.txs }

// Asynchronously generates random blocks and feeds them into a channel.
type BlockGenerator struct {
	ctx    context.Context
	config *BlocksimConfig
	rand   *rand.CannedRandom

	// The next block height to be assigned.
	nextHeight uint64

	// Generated blocks are sent to this channel.
	blocksChan chan *genBlock
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
		blocksChan: make(chan *genBlock, config.StagedBlockQueueSize),
	}
	go g.mainLoop()
	return g
}

// NextBlock blocks until the next generated block is available and returns it.
// Returns nil if the context has been cancelled and no more blocks will be produced.
func (g *BlockGenerator) NextBlock() *genBlock {
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

func (g *BlockGenerator) buildBlock() *genBlock {
	height := g.nextHeight
	g.nextHeight++

	txs := make([]block.Transaction, g.config.TransactionsPerBlock)
	for i := uint64(0); i < g.config.TransactionsPerBlock; i++ {
		txID := int64(height)*int64(g.config.TransactionsPerBlock) + int64(i) //nolint:gosec
		txs[i] = &genTx{
			hash:   g.rand.Address(txHashType, txID, int(g.config.TransactionHashSize)), //nolint:gosec
			bytes:  g.rand.Bytes(int(g.config.BytesPerTransaction)),                     //nolint:gosec
			height: height,
			index:  uint32(i), //nolint:gosec
		}
	}

	blockHash := g.rand.Address(blockHashType, int64(height), int(g.config.BlockHashSize)) //nolint:gosec
	extra := g.rand.Bytes(int(g.config.ExtraBytesPerBlock))                                //nolint:gosec

	return &genBlock{
		hash:   blockHash,
		height: height,
		txs:    txs,
		extra:  extra,
	}
}
