package blocksim

import (
	"context"
	"time"

	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
)

// generatedBatch is a contiguous run of finalized blocks together with the
// FullCommitQC that finalizes them. The blocks occupy global numbers
// [first, next).
type generatedBatch struct {
	first  types.GlobalBlockNumber
	next   types.GlobalBlockNumber
	blocks []*types.Block
	qc     *types.FullCommitQC
}

// BlockGenerator asynchronously produces finalized batches (blocks + their QC)
// and feeds them into a channel. The generator stops when the context is
// cancelled.
type BlockGenerator struct {
	ctx       context.Context
	config    *BlocksimConfig
	rng       utils.Rng
	committee *types.Committee
	keys      []types.SecretKey

	// The QC finalized in the previous batch; chains successive batches into
	// contiguous global ranges.
	prev utils.Option[*types.CommitQC]

	batchChan chan *generatedBatch
}

// NewBlockGenerator creates a BlockGenerator and immediately starts its
// background goroutine. prev seeds the chain: pass the last persisted QC to
// resume after existing on-disk history, or utils.None to start from genesis.
// prev is set on the struct before the goroutine is launched, so the goroutine
// observes it without a data race.
func NewBlockGenerator(
	ctx context.Context,
	config *BlocksimConfig,
	rng utils.Rng,
	committee *types.Committee,
	keys []types.SecretKey,
	prev utils.Option[*types.CommitQC],
) *BlockGenerator {
	g := &BlockGenerator{
		ctx:       ctx,
		config:    config,
		rng:       rng,
		committee: committee,
		keys:      keys,
		prev:      prev,
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

func (g *BlockGenerator) buildBatch() *generatedBatch {
	fqc, blocks := g.buildFullCommitQC()
	r := fqc.QC().GlobalRange()
	g.prev = utils.Some(fqc.QC())
	return &generatedBatch{first: r.First, next: r.Next, blocks: blocks, qc: fqc}
}

// makePayload builds a Payload of the configured size:
// TransactionsPerBlock transactions of BytesPerTransaction random bytes each.
func (g *BlockGenerator) makePayload() *types.Payload {
	txs := make([][]byte, g.config.TransactionsPerBlock)
	for i := range txs {
		txs[i] = utils.GenBytes(g.rng, int(g.config.BytesPerTransaction)) //nolint:gosec // payload sizes are bounded by config validation
	}
	return utils.OrPanic1(types.PayloadBuilder{
		CreatedAt: time.Now(),
		Txs:       txs,
	}.Build())
}

// buildFullCommitQC mirrors the construction in data.TestCommitQC, but with a
// configurable block count and configurable payload size. Blocks are chained
// off the previous QC so successive QCs cover contiguous global ranges.
func (g *BlockGenerator) buildFullCommitQC() (*types.FullCommitQC, []*types.Block) {
	rng := g.rng
	committee := g.committee
	keys := g.keys
	prev := g.prev

	blocks := map[types.LaneID][]*types.Block{}
	makeBlock := func(producer types.LaneID) *types.Block {
		if bs := blocks[producer]; len(bs) > 0 {
			parent := bs[len(bs)-1]
			return types.NewBlock(producer, parent.Header().Next(), parent.Header().Hash(), g.makePayload())
		}
		return types.NewBlock(
			producer,
			types.LaneRangeOpt(prev, producer).Next(),
			types.GenBlockHeaderHash(rng),
			g.makePayload(),
		)
	}
	for range int(g.config.BlocksPerQc) { //nolint:gosec // BlocksPerQc is a small config value
		producer := committee.Lanes().At(rng.Intn(committee.Lanes().Len()))
		blocks[producer] = append(blocks[producer], makeBlock(producer))
	}

	laneQCs := map[types.LaneID]*types.LaneQC{}
	var headers []*types.BlockHeader
	var blockList []*types.Block
	for lane := range committee.Lanes().All() {
		if bs := blocks[lane]; len(bs) > 0 {
			laneQCs[lane] = testLaneQC(keys, bs[len(bs)-1].Header(), 0)
			for _, b := range bs {
				headers = append(headers, b.Header())
				blockList = append(blockList, b)
			}
		}
	}

	viewSpec := types.ViewSpec{CommitQC: prev, Epoch: types.NewEpoch(0, types.OpenRoadRange(), genesisTime, committee, 0)}
	leader := committee.Leader(viewSpec.View())
	var leaderKey types.SecretKey
	for _, k := range keys {
		if k.Public() == leader {
			leaderKey = k
			break
		}
	}
	proposal := utils.OrPanic1(types.NewProposal(
		leaderKey,
		viewSpec,
		time.Now(),
		laneQCs,
		func() utils.Option[*types.AppQC] {
			if cqc, ok := prev.Get(); ok {
				n := cqc.GlobalRange().Next
				p := types.NewAppProposal(n-1, viewSpec.View().Index, types.GenAppHash(rng), viewSpec.Epoch.EpochIndex())
				return utils.Some(testAppQC(keys, p))
			}
			return utils.None[*types.AppQC]()
		}(),
	))
	votes := make([]*types.Signed[*types.CommitVote], 0, len(keys))
	for _, k := range keys {
		votes = append(votes, types.Sign(k, types.NewCommitVote(proposal.Proposal().Msg())))
	}
	return types.NewFullCommitQC(types.NewCommitQC(votes), headers), blockList
}

func testLaneQC(keys []types.SecretKey, header *types.BlockHeader, epochIndex uint64) *types.LaneQC {
	vote := types.NewLaneVote(header)
	votes := make([]*types.Signed[*types.LaneVote], 0, len(keys))
	for _, k := range keys {
		votes = append(votes, types.Sign(k, vote))
	}
	return types.NewLaneQC(votes, epochIndex)
}

func testAppQC(keys []types.SecretKey, proposal *types.AppProposal) *types.AppQC {
	vote := types.NewAppVote(proposal)
	votes := make([]*types.Signed[*types.AppVote], 0, len(keys))
	for _, k := range keys {
		votes = append(votes, types.Sign(k, vote))
	}
	return types.NewAppQC(votes)
}
