package types

import (
	"cmp"
	"fmt"
	"math/big"
	"slices"
	"time"

	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/params"

	"github.com/sei-protocol/sei-chain/sei-tendermint/crypto/ed25519"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/pb"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/hashable"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
)

// testViewTimeout is the genesis view timeout. The accepted range is
// [NextTimestamp(), NextTimestamp() + (view number + 1) * testViewTimeout].
const testViewTimeout = 1500 * time.Millisecond

// BuildPrepareQC builds a PrepareQC over proposal signed by keys.
func BuildPrepareQC(keys []SecretKey, proposal *Proposal) *PrepareQC {
	vote := NewPrepareVote(proposal)
	votes := make([]*Signed[*PrepareVote], len(keys))
	for i, key := range keys {
		votes[i] = key.Sign(vote)
	}
	return NewPrepareQC(votes)
}

// BuildCommitQC builds a valid CommitQC from explicit lane QCs and an optional app QC.
// If laneQCs is empty, a single 1-block LaneQC is synthesized so the tipcut is non-empty.
// Use BuildFullCommitQC when you want random blocks generated automatically.
func BuildCommitQC(
	epoch *Epoch,
	keys []SecretKey,
	prev utils.Option[*CommitQC],
	laneQCs map[LaneID]*LaneQC,
) *CommitQC {
	vs := ViewSpec{ConsensusSpec: ConsensusSpec{CommitQC: prev, Epoch: epoch}}
	if len(laneQCs) == 0 {
		laneQCs = oneBlockLaneQCMap(vs, keys)
	}
	leader := epoch.Committee().Leader(vs.View())
	var leaderKey SecretKey
	for _, k := range keys {
		if k.Public() == leader {
			leaderKey = k
			break
		}
	}
	proposal := utils.OrPanic1(NewProposal(leaderKey, vs, vs.ClampTimestamp(time.Now(), testViewTimeout), laneQCs))
	votes := make([]*Signed[*CommitVote], 0, len(keys))
	prepareQC := BuildPrepareQC(keys, proposal.Proposal().Msg())
	for _, k := range keys {
		votes = append(votes, k.Sign(NewCommitVote(prepareQC)))
	}
	return NewCommitQC(votes)
}

// oneBlockLaneQCMap builds a single LaneQC advancing the first committee lane by one block.
func oneBlockLaneQCMap(vs ViewSpec, keys []SecretKey) map[LaneID]*LaneQC {
	c := vs.Epoch.Committee()
	lane := c.Lanes().At(0)
	n := LaneRangeOpt(vs.CommitQC, lane).Next()
	header := NewLaneBlock(lane, n, LaneBlockHeaderHash{}, &Payload{}).Header()
	vote := NewLaneVote(vs.Epoch.EpochIndex(), header)
	votes := make([]*Signed[*LaneVote], 0, len(keys))
	for _, k := range TestKeysWithWeight(c, keys, c.LaneQuorum()) {
		votes = append(votes, k.Sign(vote))
	}
	return map[LaneID]*LaneQC{lane: NewLaneQC(votes)}
}

// GetChallenge generates a random Challenge.
func GetChallenge(rng utils.Rng) Challenge {
	return Challenge{
		SessionKey: SessionKey(utils.GenBytes(rng, len(SessionKey{}))),
		IsServer:   utils.GenBool(rng),
	}
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
	sks := rng.GenSliceN(size, GenSecretKey)
	pks := map[PublicKey]uint64{}
	for _, sk := range sks {
		pks[sk.Public()] = 1000 + uint64(rng.Intn(1000)) // nolint: gosec
	}
	slices.SortStableFunc(sks, func(a, b SecretKey) int {
		return -cmp.Compare(pks[a.Public()], pks[b.Public()])
	})
	prev := make(map[PublicKey]LaneID, len(sks))
	for _, sk := range sks {
		v := sk.Public()
		prev[v] = LaneID{Validator: v, Joined: GenEpochIndex(rng)}
	}
	return utils.OrPanic1(newCommittee(prev, pks, 0)), sks
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

// GenLaneID generates a random LaneID (random validator, random joined).
func GenLaneID(rng utils.Rng) LaneID {
	return LaneID{Validator: TestSecretKey(GenNodeID(rng)).Public(), Joined: GenEpochIndex(rng)}
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

// NewLaneBlockForTesting builds a LaneBlock with an injected payload hash instead of computing
// payload.Hash(). FOR TESTS/BENCHMARKS ONLY: the header's payloadHash need not match the
// payload, so LaneProposal.Verify will fail. This skips a full marshal + SHA-256 of the payload.
func NewLaneBlockForTesting(
	lane LaneID,
	height LaneHeight,
	parentHash LaneBlockHeaderHash,
	payload *Payload,
	payloadHash PayloadHash,
) *LaneBlock {
	return &LaneBlock{
		header: &LaneBlockHeader{
			lane:        lane,
			height:      height,
			parentHash:  parentHash,
			payloadHash: payloadHash,
		},
		payload: payload,
	}
}

// GenLaneHeight generates a random LaneHeight.
func GenLaneHeight(rng utils.Rng) LaneHeight {
	return LaneHeight(rng.Uint64())
}

// GenLaneRangeFor generates a random LaneRange that starts at first and whose lane ID is drawn from c.
func GenLaneRangeFor(rng utils.Rng, c *Committee, first LaneHeight) *LaneRange {
	lanes := c.Lanes()
	lane := lanes.At(rng.Intn(lanes.Len()))
	length := rng.Uint64() % (MaxLaneRangeInProposal + 1)
	if length == 0 {
		return NewLaneRange(lane, first, utils.None[*LaneBlockHeader]())
	}
	header := NewLaneBlock(
		lane,
		first+LaneHeight(length-1),
		GenLaneBlockHeaderHash(rng),
		GenPayload(rng),
	).Header()
	return NewLaneRange(lane, first, utils.Some(header))
}

// GenLaneRange generates a random LaneRange.
func GenLaneRange(rng utils.Rng) *LaneRange {
	lane := GenLaneID(rng)
	first := GenLaneHeight(rng)
	length := rng.Uint64() % (MaxLaneRangeInProposal + 1)
	if length == 0 {
		return NewLaneRange(lane, first, utils.None[*LaneBlockHeader]())
	}
	header := NewLaneBlock(
		lane,
		first+LaneHeight(length-1),
		GenLaneBlockHeaderHash(rng),
		GenPayload(rng),
	).Header()
	return NewLaneRange(lane, first, utils.Some(header))
}

// GenLaneBlockHeaderHash generates a random LaneBlockHeaderHash.
func GenLaneBlockHeaderHash(rng utils.Rng) LaneBlockHeaderHash {
	return LaneBlockHeaderHash(hashable.GenHash[*pb.LaneBlockHeader](rng))
}

// GenPayloadHash generates a random PayloadHash.
func GenPayloadHash(rng utils.Rng) PayloadHash {
	return PayloadHash(hashable.GenHash[*pb.Payload](rng))
}

// GenLaneBlockHeader generates a random LaneBlockHeader.
func GenLaneBlockHeader(rng utils.Rng) *LaneBlockHeader {
	return &LaneBlockHeader{
		lane:        GenLaneID(rng),
		height:      GenLaneHeight(rng),
		payloadHash: GenPayloadHash(rng),
	}
}

// GenPayload generates a random Payload.
func GenPayload(rng utils.Rng) *Payload {
	return utils.OrPanic1(PayloadBuilder{
		CreatedAt:         utils.GenTimestamp(rng),
		TotalGasWanted:    rng.Uint64(),
		TotalGasEstimated: rng.Uint64(),
		Txs:               rng.GenSlice(func(rng utils.Rng) []byte { return utils.GenBytes(rng, 10) }),
	}.Build())
}

// GenEthPayload generates a payload of signed Ethereum transfers on chain ID 1.
func GenEthPayload(rng utils.Rng) *Payload {
	payload := GenPayload(rng).ToBuilder()
	payload.TotalGasWanted = 0
	payload.TotalGasEstimated = 0
	signer := ethtypes.LatestSignerForChainID(big.NewInt(1))
	for i := range payload.Txs {
		key := utils.OrPanic1(crypto.ToECDSA(utils.GenBytes(rng, 32)))
		recipient := common.Address(utils.GenBytes(rng, len(common.Address{})))
		tx := ethtypes.NewTx(&ethtypes.LegacyTx{
			To:       &recipient,
			Gas:      params.TxGas,
			GasPrice: big.NewInt(params.GWei),
		})
		signed := utils.OrPanic1(ethtypes.SignTx(tx, signer, key))
		payload.Txs[i] = utils.OrPanic1(signed.MarshalBinary())
		payload.TotalGasWanted += signed.Gas()
		payload.TotalGasEstimated += signed.Gas()
	}
	return utils.OrPanic1(payload.Build())
}

// GenLaneBlock generates a random LaneBlock.
func GenLaneBlock(rng utils.Rng) *LaneBlock {
	return NewLaneBlock(
		GenLaneID(rng),
		GenLaneHeight(rng),
		GenLaneBlockHeaderHash(rng),
		GenPayload(rng),
	)
}

// GenSigned generates a random Signed.
func GenSigned[T Msg](rng utils.Rng, msg T) *Signed[T] {
	return GenSecretKey(rng).Sign(msg)
}

// GenLaneProposal generates a random LaneProposal.
func GenLaneProposal(rng utils.Rng) *LaneProposal {
	return NewLaneProposal(GenLaneBlock(rng))
}

// GenLaneVote generates a random LaneVote.
func GenLaneVote(rng utils.Rng) *LaneVote {
	return NewLaneVote(GenEpochIndex(rng), GenLaneBlockHeader(rng))
}

// GenLaneQC generates a random LaneQC.
func GenLaneQC(rng utils.Rng) *LaneQC {
	vote := GenLaneVote(rng)
	return NewLaneQC(rng.GenSlice(func(rng utils.Rng) *Signed[*LaneVote] { return GenSigned(rng, vote) }))
}

// GenRoadHeight generates a random RoadHeight.
func GenRoadHeight(rng utils.Rng) RoadHeight {
	return RoadHeight(rng.Uint64())
}

// GenViewNumber generates a random ViewNumber.
func GenViewNumber(rng utils.Rng) ViewNumber {
	return ViewNumber(rng.Uint64())
}

// GenView generates a random View.
func GenView(rng utils.Rng) View {
	return View{
		Height:     GenRoadHeight(rng),
		Number:     GenViewNumber(rng),
		EpochIndex: GenEpochIndex(rng),
	}
}

// GenEpochIndex returns a random small EpochIndex for test use.
func GenEpochIndex(rng utils.Rng) EpochIndex {
	return EpochIndex(rng.Uint64() % 100)
}

// GenEpochWithCommittee returns a random Epoch wrapping committee.
// epochIndex and Roads.First are randomized so that tests
// exercise epoch-binding checks rather than silently passing on zero values.
func GenEpochWithCommittee(rng utils.Rng, committee *Committee) *Epoch {
	first := RoadHeight(rng.Uint64() % 1000)
	return NewEpoch(
		GenEpochIndex(rng),
		RoadRange{First: first, Next: first + RoadHeight(rng.Uint64()%10000) + 10},
		committee)
}

// CommitQCAt creates a CommitQC at ep.RoadRange().First, signed by all keys.
func CommitQCAt(ep *Epoch, keys []SecretKey) *CommitQC {
	voteProposal := ProposalAt(ep, View{EpochIndex: ep.EpochIndex(), Height: ep.RoadRange().First})
	vote := NewCommitVote(BuildPrepareQC(keys, voteProposal))
	votes := make([]*Signed[*CommitVote], len(keys))
	for i, k := range keys {
		votes[i] = k.Sign(vote)
	}
	return NewCommitQC(votes)
}

// GenRoadBlockHeaderHash generates a random RoadBlockHeaderHash.
func GenRoadBlockHeaderHash(rng utils.Rng) RoadBlockHeaderHash {
	return RoadBlockHeaderHash(hashable.GenHash[*pb.RoadBlockHeader](rng))
}

// GenProposal generates a random Proposal.
func GenProposal(rng utils.Rng) *Proposal {
	return GenProposalAt(rng, GenView(rng))
}

// GenProposalAt generates a Proposal at a specific view.
func GenProposalAt(rng utils.Rng, view View) *Proposal {
	return &Proposal{viewNumber: view.Number, header: newRoadBlockHeader(view.EpochIndex, view.Height, utils.Some(GenRoadBlockHeaderHash(rng)), utils.GenTimestamp(rng), rng.GenSlice(GenLaneRange))}
}

// ProposalAt returns a Proposal at view, consistent with ep, with a single 1-block lane range.
func ProposalAt(ep *Epoch, view View) *Proposal {
	p, _ := ProposalAtBlocks(ep, view, 1)
	return p
}

// ProposalWithParent is ProposalAt with a lane range that continues from parent, and the given parent hash.
func ProposalWithParent(ep *Epoch, view View, parent utils.Option[*CommitQC], parentHash utils.Option[RoadBlockHeaderHash]) *Proposal {
	p, _ := proposalAtBlocks(ep, view, 1, parent, parentHash)
	return p
}

// ProposalAtBlocks is ProposalAt with a lane range of n blocks, n >= 1.
func ProposalAtBlocks(ep *Epoch, view View, n int) (*Proposal, []*LaneBlock) {
	return proposalAtBlocks(ep, view, n, utils.None[*CommitQC](), utils.None[RoadBlockHeaderHash]())
}

// proposalAtBlocks returns a Proposal at view with parentHash and a lane range of n blocks, n >= 1,
// together with those blocks. Each lane range continues from parent.
func proposalAtBlocks(ep *Epoch, view View, n int, parent utils.Option[*CommitQC], parentHash utils.Option[RoadBlockHeaderHash]) (*Proposal, []*LaneBlock) {
	view.EpochIndex = ep.EpochIndex()
	lanes := ep.Committee().Lanes()
	lane := lanes.At(0)
	prev := LaneRangeOpt(parent, lane)
	blocks := make([]*LaneBlock, n)
	prevHash := prev.LastHash()
	num := prev.Next()
	for i := 0; i < n; i++ {
		b := NewLaneBlock(lane, num, prevHash, &Payload{})
		blocks[i] = b
		prevHash = b.Header().Hash()
		num = b.Header().Next()
	}
	laneRanges := []*LaneRange{NewLaneRange(lane, prev.Next(), utils.Some(blocks[n-1].Header()))}
	for l := range lanes.All() {
		// A lane that the header omits starts at 0, so only a lane that the parent advanced needs an empty range.
		if next := LaneRangeOpt(parent, l).Next(); l != lane && next > 0 {
			laneRanges = append(laneRanges, NewLaneRange(l, next, utils.None[*LaneBlockHeader]()))
		}
	}
	return &Proposal{viewNumber: view.Number, header: newRoadBlockHeader(view.EpochIndex, view.Height, parentHash, time.Time{}, laneRanges)}, blocks
}

// GenProposalForEpoch generates a Proposal at a specific view whose epochIndex,
// and lane IDs are consistent with ep. Use in tests that verify
// QCs against a known Epoch.
func GenProposalForEpoch(rng utils.Rng, ep *Epoch, view View) *Proposal {
	view.EpochIndex = ep.EpochIndex()
	c := ep.Committee()
	// The lane ranges start at 0, so they continue from an empty parent.
	laneRanges := rng.GenSlice(func(rng utils.Rng) *LaneRange { return GenLaneRangeFor(rng, c, 0) })
	return &Proposal{viewNumber: view.Number, header: newRoadBlockHeader(view.EpochIndex, view.Height, utils.None[RoadBlockHeaderHash](), utils.GenTimestamp(rng), laneRanges)}
}

// GenAppHash generates a random AppHash.
func GenAppHash(rng utils.Rng) AppHash {
	return AppHash(utils.GenBytes(rng, 32))
}

// GenAppProposal generates a random AppProposal.
func GenAppProposal(rng utils.Rng) *AppProposal {
	return NewAppProposal(GenProposal(rng).View(), GenAppHash(rng))
}

// GenAppVote generates a random AppVote.
func GenAppVote(rng utils.Rng) *AppVote {
	return NewAppVote(GenAppProposal(rng))
}

// GenAppQC generates a random AppQC.
func GenAppQC(rng utils.Rng) *AppQC {
	vote := GenAppVote(rng)
	return NewAppQC(rng.GenSlice(func(rng utils.Rng) *Signed[*AppVote] { return GenSigned(rng, vote) }))
}

// GenAppQCFor generates an AppQC over appProposal.
func GenAppQCFor(rng utils.Rng, appProposal *AppProposal) *AppQC {
	vote := NewAppVote(appProposal)
	return NewAppQC(rng.GenSliceN(1, func(rng utils.Rng) *Signed[*AppVote] { return GenSigned(rng, vote) }))
}

// GenFullProposal generates a random FullProposal.
func GenFullProposal(rng utils.Rng) *FullProposal {
	laneQCs := map[LaneID]*LaneQC{}
	for _, qc := range rng.GenSlice(GenLaneQC) {
		laneQCs[qc.Header().Lane()] = qc
	}
	return &FullProposal{
		proposal:  GenSigned(rng, GenProposal(rng)),
		laneQCs:   laneQCs,
		timeoutQC: utils.Some(GenTimeoutQC(rng)),
	}
}

// GenSuperBlock generates a random SuperBlock.
func GenSuperBlock(rng utils.Rng) *SuperBlock {
	block := GenLaneBlock(rng)
	qc := GenFullCommitQCAt(rng, GenRoadHeight(rng), 1)
	qc = NewFullCommitQC(qc.QC(), []*LaneBlockHeader{block.Header()})
	return NewRoadBlock(qc, []*LaneBlock{block}).SuperBlock()
}

// GenPrepareVote generates a random PrepareVote.
func GenPrepareVote(rng utils.Rng) *PrepareVote {
	voteProposal := GenProposal(rng)
	return NewPrepareVote(voteProposal)
}

// GenPrepareQC generates a random PrepareQC.
func GenPrepareQC(rng utils.Rng) *PrepareQC {
	vote := GenPrepareVote(rng)
	return NewPrepareQC(rng.GenSlice(func(rng utils.Rng) *Signed[*PrepareVote] { return GenSigned(rng, vote) }))
}

// GenCommitVote generates a random CommitVote.
func GenCommitVote(rng utils.Rng) *CommitVote {
	return NewCommitVote(GenPrepareQC(rng))
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
		headers: rng.GenSlice(GenLaneBlockHeader),
	}
}

// GenFullCommitQCAt generates a QC at index with n chained headers matching its lane range.
func GenFullCommitQCAt(rng utils.Rng, height RoadHeight, n int) *FullCommitQC {
	headers := make([]*LaneBlockHeader, n)
	var ranges []*LaneRange
	if n > 0 {
		lane := GenLaneID(rng)
		var parent LaneBlockHeaderHash
		for i := range headers {
			headers[i] = NewLaneBlock(lane, LaneHeight(i), parent, GenPayload(rng)).Header() //nolint:gosec // slice indices are nonnegative
			parent = headers[i].Hash()
		}
		last := headers[n-1]
		ranges = append(ranges, NewLaneRange(last.Lane(), 0, utils.Some(last)))
	}
	view := GenView(rng)
	view.Height = height
	proposal := &Proposal{viewNumber: view.Number, header: newRoadBlockHeader(view.EpochIndex, view.Height, utils.None[RoadBlockHeaderHash](), utils.GenTimestamp(rng), ranges)}
	qc := NewCommitQC([]*Signed[*CommitVote]{GenSigned(rng, NewCommitVote(BuildPrepareQC([]SecretKey{GenSecretKey(rng)}, proposal)))})
	return NewFullCommitQC(qc, headers)
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
	return NewTimeoutQC(rng.GenSlice(GenFullTimeoutVote))
}

// GenAppProposalAt generates an AppProposal for road height.
func GenAppProposalAt(rng utils.Rng, height RoadHeight) *AppProposal {
	view := GenView(rng)
	view.Height = height
	return NewAppProposal(GenProposalAt(rng, view).View(), GenAppHash(rng))
}
