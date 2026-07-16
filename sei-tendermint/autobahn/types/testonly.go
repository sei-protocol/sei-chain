package types

import (
	"cmp"
	"fmt"
	"slices"
	"time"

	"github.com/sei-protocol/sei-chain/sei-tendermint/crypto/ed25519"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/pb"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/hashable"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
)

// BuildCommitQC builds a valid CommitQC from explicit lane QCs and an optional app QC.
// Use BuildFullCommitQC when you want random blocks generated automatically.
func BuildCommitQC(
	epoch *Epoch,
	keys []SecretKey,
	prev utils.Option[*CommitQC],
	laneQCs map[LaneID]*LaneQC,
	appQC utils.Option[*AppQC],
) *CommitQC {
	vs := ViewSpec{CommitQC: prev, Epoch: epoch}
	leader := epoch.Committee().Leader(vs.View())
	var leaderKey SecretKey
	for _, k := range keys {
		if k.Public() == leader {
			leaderKey = k
			break
		}
	}
	proposal := utils.OrPanic1(NewProposal(leaderKey, vs, time.Now(), laneQCs, appQC))
	votes := make([]*Signed[*CommitVote], 0, len(keys))
	for _, k := range keys {
		votes = append(votes, Sign(k, NewCommitVote(proposal.Proposal().Msg())))
	}
	return NewCommitQC(votes)
}

// GenNodeID generates a random NodeID.
func GenNodeID(rng utils.Rng) NodeID {
	return NodeID(utils.GenString(rng, 10))
}

// GenPublicKey generates a random PublicKey.
func GenPublicKey(rng utils.Rng) PublicKey {
	return GenSecretKey(rng).Public()
}

// GenSecretKey generates a random SecretKey.
func GenSecretKey(rng utils.Rng) SecretKey {
	return SecretKey{key: ed25519.TestSecretKey(utils.GenBytes(rng, 32))}
}

// GenCommittee generates a random Committee of the given size.
// Returns the generated secret keys as well.
func GenCommittee(rng utils.Rng, size int) (*Committee, []SecretKey) {
	sks := utils.GenSliceN(rng, size, GenSecretKey)
	pks := map[PublicKey]uint64{}
	for _, sk := range sks {
		pks[sk.Public()] = 1000 + uint64(rng.Intn(1000)) // nolint: gosec
	}
	slices.SortStableFunc(sks, func(a, b SecretKey) int {
		return -cmp.Compare(pks[a.Public()], pks[b.Public()])
	})
	return utils.OrPanic1(NewCommittee(pks)), sks
}

// TestKeysWithWeight returns a deterministic subset of keys whose committee weight reaches the requested threshold.
func TestKeysWithWeight(c *Committee, keys []SecretKey, minWeight uint64) []SecretKey {
	weight := uint64(0)
	for i, key := range keys {
		if weight >= minWeight {
			return keys[:i]
		}
		weight += c.Weight(key.Public())
	}
	if weight < minWeight {
		panic("not enough weight")
	}
	return keys
}

// TestSecretKey creates a SecretKey for testing purposes.
// It uses NodeID as the seed of the secret key.
func TestSecretKey(nodeID NodeID) SecretKey {
	return SecretKey{key: ed25519.TestSecretKey([]byte(nodeID))}
}

// GenLaneID generates a random LaneID.
func GenLaneID(rng utils.Rng) LaneID {
	return TestSecretKey(GenNodeID(rng)).Public()
}

// GenSignature generates a random Signature.
func GenSignature(rng utils.Rng) *Signature {
	key := GenSecretKey(rng)
	return &Signature{
		key: key.Public(),
		sig: key.key.Sign(utils.GenBytes(rng, 10)),
	}
}

// SignatureForTesting builds a Signature from a public key and raw signature bytes
// WITHOUT performing any real signing. FOR TESTS/BENCHMARKS ONLY: the resulting
// signature is arbitrary bytes and will NOT verify. sigBytes must be exactly
// ed25519.SignatureSize (64) bytes.
func SignatureForTesting(key PublicKey, sigBytes []byte) (*Signature, error) {
	sig, err := ed25519.SignatureFromBytes(sigBytes)
	if err != nil {
		return nil, fmt.Errorf("sig: %w", err)
	}
	return &Signature{key: key, sig: sig}, nil
}

// SignedForTesting attaches a precomputed (typically fake) signature to a message
// WITHOUT signing. FOR TESTS/BENCHMARKS ONLY: the result will NOT verify. The message is
// still hashed (cheap), only the expensive signing operation is skipped.
func SignedForTesting[T Msg](msg T, sig *Signature) *Signed[T] {
	return newSigned(msg, sig)
}

// NewBlockForTesting builds a Block with an injected payload hash instead of computing
// payload.Hash(). FOR TESTS/BENCHMARKS ONLY: the header's payloadHash need not match the
// payload, so Block.Verify will fail. This skips a full marshal + SHA-256 of the payload.
func NewBlockForTesting(
	lane LaneID,
	blockNumber BlockNumber,
	parentHash BlockHeaderHash,
	payload *Payload,
	payloadHash PayloadHash,
) *Block {
	return &Block{
		header: &BlockHeader{
			lane:        lane,
			blockNumber: blockNumber,
			parentHash:  parentHash,
			payloadHash: payloadHash,
		},
		payload: payload,
	}
}

// GenBlockNumber generates a random BlockNumber.
func GenBlockNumber(rng utils.Rng) BlockNumber {
	return BlockNumber(rng.Uint64())
}

// GenLaneRangeFor generates a random LaneRange whose lane ID is drawn from c.
func GenLaneRangeFor(rng utils.Rng, c *Committee) *LaneRange {
	lanes := c.Lanes()
	lane := lanes.At(rng.Intn(lanes.Len()))
	first := GenBlockNumber(rng)
	length := rng.Uint64() % (MaxLaneRangeInProposal + 1)
	if length == 0 {
		return NewLaneRange(lane, first, utils.None[*BlockHeader]())
	}
	header := NewBlock(
		lane,
		first+BlockNumber(length-1),
		GenBlockHeaderHash(rng),
		GenPayload(rng),
	).Header()
	return NewLaneRange(lane, first, utils.Some(header))
}

// GenLaneRange generates a random LaneRange.
func GenLaneRange(rng utils.Rng) *LaneRange {
	lane := GenLaneID(rng)
	first := GenBlockNumber(rng)
	length := rng.Uint64() % (MaxLaneRangeInProposal + 1)
	if length == 0 {
		return NewLaneRange(lane, first, utils.None[*BlockHeader]())
	}
	header := NewBlock(
		lane,
		first+BlockNumber(length-1),
		GenBlockHeaderHash(rng),
		GenPayload(rng),
	).Header()
	return NewLaneRange(lane, first, utils.Some(header))
}

// GenBlockHeaderHash generates a random BlockHeaderHash.
func GenBlockHeaderHash(rng utils.Rng) BlockHeaderHash {
	return BlockHeaderHash(hashable.GenHash[*pb.BlockHeader](rng))
}

// GenPayloadHash generates a random PayloadHash.
func GenPayloadHash(rng utils.Rng) PayloadHash {
	return PayloadHash(hashable.GenHash[*pb.Payload](rng))
}

// GenBlockHeader generates a random BlockHeader.
func GenBlockHeader(rng utils.Rng) *BlockHeader {
	return &BlockHeader{
		lane:        GenLaneID(rng),
		blockNumber: GenBlockNumber(rng),
		payloadHash: GenPayloadHash(rng),
	}
}

// GenPayload generates a random Payload.
func GenPayload(rng utils.Rng) *Payload {
	return utils.OrPanic1(PayloadBuilder{
		CreatedAt:         utils.GenTimestamp(rng),
		TotalGasWanted:    rng.Uint64(),
		TotalGasEstimated: rng.Uint64(),
		Txs:               utils.GenSlice(rng, func(rng utils.Rng) []byte { return utils.GenBytes(rng, 10) }),
	}.Build())
}

// GenBlock generates a random Block.
func GenBlock(rng utils.Rng) *Block {
	return NewBlock(
		GenLaneID(rng),
		GenBlockNumber(rng),
		GenBlockHeaderHash(rng),
		GenPayload(rng),
	)
}

// GenSigned generates a random Signed.
func GenSigned[T Msg](rng utils.Rng, msg T) *Signed[T] {
	return Sign(GenSecretKey(rng), msg)
}

// GenLaneProposal generates a random LaneProposal.
func GenLaneProposal(rng utils.Rng) *LaneProposal {
	return NewLaneProposal(GenBlock(rng))
}

// GenLaneVote generates a random LaneVote.
func GenLaneVote(rng utils.Rng) *LaneVote {
	return NewLaneVote(GenBlockHeader(rng))
}

// GenLaneQC generates a random LaneQC.
func GenLaneQC(rng utils.Rng) *LaneQC {
	vote := GenLaneVote(rng)
	return NewLaneQC(utils.GenSlice(
		rng,
		func(rng utils.Rng) *Signed[*LaneVote] { return GenSigned(rng, vote) },
	))
}

// GenRoadIndex generates a random RoadIndex.
func GenRoadIndex(rng utils.Rng) RoadIndex {
	return RoadIndex(rng.Uint64())
}

// GenViewNumber generates a random ViewNumber.
func GenViewNumber(rng utils.Rng) ViewNumber {
	return ViewNumber(rng.Uint64())
}

// GenView generates a random View.
func GenView(rng utils.Rng) View {
	return View{
		Index:      GenRoadIndex(rng),
		Number:     GenViewNumber(rng),
		EpochIndex: GenEpochIndex(rng),
	}
}

// GenEpochIndex returns a random small EpochIndex for test use.
func GenEpochIndex(rng utils.Rng) EpochIndex {
	return EpochIndex(rng.Uint64() % 100)
}

// GenEpochWithCommittee returns a random Epoch wrapping committee.
// epochIndex, firstBlock, timestamp, and Roads.First are randomized so that tests
// exercise epoch-binding checks rather than silently passing on zero values.
func GenEpochWithCommittee(rng utils.Rng, committee *Committee) *Epoch {
	first := RoadIndex(rng.Uint64() % 1000)
	return NewEpoch(
		GenEpochIndex(rng),
		RoadRange{First: first, Last: first + RoadIndex(rng.Uint64()%10000) + 10},
		utils.GenTimestamp(rng),
		committee,
		GlobalBlockNumber(rng.Uint64()%1000000)+1,
	)
}

// CommitQCAt creates a CommitQC at ep.RoadRange().First, signed by all keys.
func CommitQCAt(ep *Epoch, keys []SecretKey) *CommitQC {
	vote := NewCommitVote(ProposalAt(ep, View{EpochIndex: ep.EpochIndex(), Index: ep.RoadRange().First}))
	votes := make([]*Signed[*CommitVote], len(keys))
	for i, k := range keys {
		votes[i] = Sign(k, vote)
	}
	return NewCommitQC(votes)
}

// GenProposal generates a random Proposal.
func GenProposal(rng utils.Rng) *Proposal {
	return newProposal(GenView(rng), utils.GenTimestamp(rng), utils.GenSlice(rng, GenLaneRange), utils.Some(GenAppProposal(rng)), GlobalBlockNumber(rng.Uint64()))
}

// GenProposalAt generates a Proposal at a specific view.
func GenProposalAt(rng utils.Rng, view View) *Proposal {
	return newProposal(view, utils.GenTimestamp(rng), utils.GenSlice(rng, GenLaneRange), utils.Some(GenAppProposal(rng)), GlobalBlockNumber(rng.Uint64()))
}

// ProposalAt returns a minimal Proposal at view, consistent with ep.
// No lane ranges and no app proposal — only for tests that care about
// signature weight or epoch binding, not lane/app data.
func ProposalAt(ep *Epoch, view View) *Proposal {
	view.EpochIndex = ep.EpochIndex()
	return newProposal(view, time.Time{}, nil, utils.None[*AppProposal](), ep.FirstBlock())
}

// GenProposalForEpoch generates a Proposal at a specific view whose epochIndex,
// firstBlock, and lane IDs are all consistent with ep. Use in tests that verify
// QCs against a known Epoch.
func GenProposalForEpoch(rng utils.Rng, ep *Epoch, view View) *Proposal {
	view.EpochIndex = ep.EpochIndex()
	c := ep.Committee()
	laneRanges := utils.GenSlice(rng, func(rng utils.Rng) *LaneRange { return GenLaneRangeFor(rng, c) })
	return newProposal(view, utils.GenTimestamp(rng), laneRanges, utils.Some(GenAppProposal(rng)), ep.FirstBlock())
}

// GenAppHash generates a random AppHash.
func GenAppHash(rng utils.Rng) AppHash {
	return AppHash(utils.GenBytes(rng, 32))
}

// GenAppProposal generates a random AppProposal.
func GenAppProposal(rng utils.Rng) *AppProposal {
	return NewAppProposal(GenGlobalBlockNumber(rng), GenRoadIndex(rng), GenAppHash(rng), GenEpochIndex(rng))
}

// GenAppVote generates a random AppVote.
func GenAppVote(rng utils.Rng) *AppVote {
	return NewAppVote(GenAppProposal(rng))
}

// GenAppQC generates a random AppQC.
func GenAppQC(rng utils.Rng) *AppQC {
	vote := GenAppVote(rng)
	return NewAppQC(utils.GenSlice(
		rng,
		func(rng utils.Rng) *Signed[*AppVote] { return GenSigned(rng, vote) },
	))
}

// GenFullProposal generates a random FullProposal.
func GenFullProposal(rng utils.Rng) *FullProposal {
	laneQCs := map[LaneID]*LaneQC{}
	for _, qc := range utils.GenSlice(rng, GenLaneQC) {
		laneQCs[qc.Header().Lane()] = qc
	}
	return &FullProposal{
		proposal:  GenSigned(rng, GenProposal(rng)),
		laneQCs:   laneQCs,
		appQC:     utils.Some(GenAppQC(rng)),
		timeoutQC: utils.Some(GenTimeoutQC(rng)),
	}
}

// GenGlobalBlockNumber generates a random GlobalBlockNumber.
func GenGlobalBlockNumber(rng utils.Rng) GlobalBlockNumber {
	return GlobalBlockNumber(rng.Uint64())
}

// GenGlobalBlock generates a random GlobalBlock.
func GenGlobalBlock(rng utils.Rng) *GlobalBlock {
	return &GlobalBlock{
		GlobalNumber:  GenGlobalBlockNumber(rng),
		Payload:       GenPayload(rng),
		FinalAppState: utils.Some(GenAppProposal(rng)),
	}
}

// GenPrepareVote generates a random PrepareVote.
func GenPrepareVote(rng utils.Rng) *PrepareVote {
	return NewPrepareVote(GenProposal(rng))
}

// GenPrepareQC generates a random PrepareQC.
func GenPrepareQC(rng utils.Rng) *PrepareQC {
	vote := GenPrepareVote(rng)
	return NewPrepareQC(utils.GenSlice(
		rng,
		func(rng utils.Rng) *Signed[*PrepareVote] { return GenSigned(rng, vote) },
	))
}

// GenCommitVote generates a random CommitVote.
func GenCommitVote(rng utils.Rng) *CommitVote {
	return NewCommitVote(GenProposal(rng))
}

// GenCommitQC generates a random CommitQC.
func GenCommitQC(rng utils.Rng) *CommitQC {
	committee, keys := GenCommittee(rng, int(rng.Uint64()%5)+1) //nolint:gosec
	return CommitQCAt(GenEpochWithCommittee(rng, committee), keys)
}

// GenFullCommitQC generates a random FullCommitQC.
func GenFullCommitQC(rng utils.Rng) *FullCommitQC {
	return &FullCommitQC{
		qc:      GenCommitQC(rng),
		headers: utils.GenSlice(rng, GenBlockHeader),
	}
}

// GenFullCommitQCN generates a FullCommitQC carrying exactly n headers. Unlike a
// QC built via NewFullCommitQC, its header count is not reconciled against the
// embedded CommitQC's range — it is for tests that only exercise a QC's header
// count (e.g. a store's range accounting), not its signatures or verification.
func GenFullCommitQCN(rng utils.Rng, n int) *FullCommitQC {
	return &FullCommitQC{
		qc:      GenCommitQC(rng),
		headers: utils.GenSliceN(rng, n, GenBlockHeader),
	}
}

// GenTimeoutVote generates a random TimeoutVote.
func GenTimeoutVote(rng utils.Rng) *TimeoutVote {
	return NewTimeoutVote(GenView(rng), utils.Some(GenViewNumber(rng)))
}

// GenFullTimeoutVote generates a random FullTimeoutVote.
func GenFullTimeoutVote(rng utils.Rng) *FullTimeoutVote {
	return NewFullTimeoutVote(GenSecretKey(rng), GenView(rng), utils.Some(GenPrepareQC(rng)))
}

// GenTimeoutQC generates a random TimeoutQC.
func GenTimeoutQC(rng utils.Rng) *TimeoutQC {
	return NewTimeoutQC(utils.GenSlice(rng, GenFullTimeoutVote))
}
