package blocksim

import (
	"context"
	"fmt"
	"time"

	crand "github.com/sei-protocol/sei-chain/sei-db/common/rand"
	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
)

// Ed25519 signatures are 64 bytes. Fake signatures are filled with this many canned
// random bytes; types.SignatureForTesting validates the length.
const signatureSizeBytes = 64

// SHA-256 digests (parent hash, payload hash, app hash) are 32 bytes.
const hashSizeBytes = 32

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
//
// This is a DB stress benchmark: the block store persists blocks/QCs verbatim and never
// verifies signatures or content. The generator therefore avoids the two dominant costs
// of realistic block production — real Ed25519 signing and per-transaction random-byte
// generation — so that the benchmark measures the database rather than block production.
// All randomness comes from a CannedRandom (a pre-generated, immutable buffer served as
// zero-copy sub-slices), and every signature is a fake (never-signed) blob. See
// types.SignatureForTesting / SignedForTesting / NewBlockForTesting / NewProposalForTesting.
type BlockGenerator struct {
	ctx       context.Context
	config    *BlocksimConfig
	rand      *crand.CannedRandom
	committee *types.Committee
	pubKeys   []types.PublicKey

	// The QC finalized in the previous batch; chains successive batches into
	// contiguous global ranges.
	prev utils.Option[*types.CommitQC]

	batchChan chan *generatedBatch
}

// NewBlockGenerator creates a BlockGenerator and immediately starts its
// background goroutine. prev seeds the chain: pass the last persisted QC to
// resume after existing on-disk history, or utils.None to start from genesis.
// prev is set on the struct before the goroutine is launched, so the goroutine
// observes it without a data race. rand must not be shared with any other
// goroutine (a single generator goroutine owns it).
func NewBlockGenerator(
	ctx context.Context,
	config *BlocksimConfig,
	rand *crand.CannedRandom,
	committee *types.Committee,
	keys []types.SecretKey,
	prev utils.Option[*types.CommitQC],
) *BlockGenerator {
	pubKeys := make([]types.PublicKey, len(keys))
	for i, k := range keys {
		pubKeys[i] = k.Public()
	}
	g := &BlockGenerator{
		ctx:       ctx,
		config:    config,
		rand:      rand,
		committee: committee,
		pubKeys:   pubKeys,
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

// makePayload builds a Payload of the configured size: TransactionsPerBlock transactions
// of BytesPerTransaction bytes each. The transaction bytes are zero-copy sub-slices of
// the CannedRandom buffer, so no random generation or allocation happens per transaction.
func (g *BlockGenerator) makePayload() *types.Payload {
	txs := make([][]byte, g.config.TransactionsPerBlock)
	for i := range txs {
		txs[i] = g.rand.Bytes(int(g.config.BytesPerTransaction)) //nolint:gosec // payload sizes are bounded by config validation
	}
	return utils.OrPanic1(types.PayloadBuilder{
		CreatedAt: time.Now(),
		Txs:       txs,
	}.Build())
}

// fakeSig builds a signature carrying the given public key but random (never-signed)
// bytes. The block store never verifies signatures, so this avoids real Ed25519 signing.
func (g *BlockGenerator) fakeSig(key types.PublicKey) *types.Signature {
	sig, err := types.SignatureForTesting(key, g.rand.Bytes(signatureSizeBytes))
	if err != nil {
		panic(fmt.Sprintf("failed to build fake signature: %v", err))
	}
	return sig
}

// buildFullCommitQC mirrors the construction in data.TestCommitQC, but with a
// configurable block count and configurable payload size, and without real crypto or
// math/rand. Blocks are chained off the previous QC so successive QCs cover contiguous
// global ranges.
func (g *BlockGenerator) buildFullCommitQC() (*types.FullCommitQC, []*types.Block) {
	committee := g.committee
	prev := g.prev

	blocks := map[types.LaneID][]*types.Block{}
	makeBlock := func(producer types.LaneID) *types.Block {
		payload := g.makePayload()
		payloadHash := utils.OrPanic1(types.ParsePayloadHash(g.rand.Bytes(hashSizeBytes)))
		if bs := blocks[producer]; len(bs) > 0 {
			parent := bs[len(bs)-1]
			return types.NewBlockForTesting(producer, parent.Header().Next(), parent.Header().Hash(), payload, payloadHash)
		}
		genesisParent := utils.OrPanic1(types.ParseBlockHeaderHash(g.rand.Bytes(hashSizeBytes)))
		return types.NewBlockForTesting(
			producer,
			types.LaneRangeOpt(prev, producer).Next(),
			genesisParent,
			payload,
			payloadHash,
		)
	}
	for range int(g.config.BlocksPerQc) { //nolint:gosec // BlocksPerQc is a small config value
		producer := committee.Lanes().At(int(g.rand.Int64Range(0, int64(committee.Lanes().Len()))))
		blocks[producer] = append(blocks[producer], makeBlock(producer))
	}

	laneQCs := map[types.LaneID]*types.LaneQC{}
	var headers []*types.BlockHeader
	var blockList []*types.Block
	for lane := range committee.Lanes().All() {
		if bs := blocks[lane]; len(bs) > 0 {
			laneQCs[lane] = g.fakeLaneQC(bs[len(bs)-1].Header())
			for _, b := range bs {
				headers = append(headers, b.Header())
				blockList = append(blockList, b)
			}
		}
	}

	viewSpec := types.ViewSpec{CommitQC: prev, Epoch: types.NewEpoch(0, types.OpenRoadRange(), genesisTime, committee, 0)}
	leader := committee.Leader(viewSpec.View())
	appQC := func() utils.Option[*types.AppQC] {
		if n := viewSpec.NextGlobalBlock(); n > 0 {
			p := types.NewAppProposal(n-1, viewSpec.View().Index, types.AppHash(g.rand.Bytes(hashSizeBytes)), viewSpec.Epoch.EpochIndex())
			return utils.Some(g.fakeAppQC(p))
		}
		return utils.None[*types.AppQC]()
	}()
	proposal := utils.OrPanic1(types.NewProposalForTesting(
		committee,
		viewSpec,
		time.Now(),
		laneQCs,
		appQC,
		g.fakeSig(leader),
	))
	commitVote := types.NewCommitVote(proposal.Proposal().Msg())
	votes := make([]*types.Signed[*types.CommitVote], 0, len(g.pubKeys))
	for _, pk := range g.pubKeys {
		votes = append(votes, types.SignedForTesting(commitVote, g.fakeSig(pk)))
	}
	return types.NewFullCommitQC(types.NewCommitQC(votes), headers), blockList
}

func (g *BlockGenerator) fakeLaneQC(header *types.BlockHeader) *types.LaneQC {
	vote := types.NewLaneVote(header)
	votes := make([]*types.Signed[*types.LaneVote], 0, len(g.pubKeys))
	for _, pk := range g.pubKeys {
		votes = append(votes, types.SignedForTesting(vote, g.fakeSig(pk)))
	}
	return types.NewLaneQC(votes)
}

func (g *BlockGenerator) fakeAppQC(proposal *types.AppProposal) *types.AppQC {
	vote := types.NewAppVote(proposal)
	votes := make([]*types.Signed[*types.AppVote], 0, len(g.pubKeys))
	for _, pk := range g.pubKeys {
		votes = append(votes, types.SignedForTesting(vote, g.fakeSig(pk)))
	}
	return types.NewAppQC(votes)
}
