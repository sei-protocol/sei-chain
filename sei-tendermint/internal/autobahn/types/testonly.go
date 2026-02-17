package types

import (
	"time"

	"github.com/sei-protocol/sei-chain/sei-tendermint/crypto/ed25519"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/pb"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/hashable"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
)

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
	pks := make([]PublicKey, size)
	for i, sk := range sks {
		pks[i] = sk.Public()
	}
	c, err := NewRoundRobinElection(pks)
	if err != nil {
		panic(err)
	}
	return c, sks
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

// GenBlockNumber generates a random BlockNumber.
func GenBlockNumber(rng utils.Rng) BlockNumber {
	return BlockNumber(rng.Uint64())
}

// GenLaneRange generates a random LaneRange.
func GenLaneRange(rng utils.Rng) *LaneRange {
	return NewLaneRange(GenLaneID(rng), GenBlockNumber(rng), utils.Some(GenBlockHeader(rng)))
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
	return PayloadBuilder{
		CreatedAt: utils.GenTimestamp(rng),
		TotalGas:  rng.Uint64(),
		EdgeCount: rng.Int63(),
		Coinbase:  utils.GenBytes(rng, 10),
		Basefee:   rng.Int63(),
		Txs:       utils.GenSlice(rng, func(rng utils.Rng) []byte { return utils.GenBytes(rng, 10) }),
	}.Build()
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
		Index:  GenRoadIndex(rng),
		Number: GenViewNumber(rng),
	}
}

// GenProposal generates a random Proposal.
func GenProposal(rng utils.Rng) *Proposal {
	return newProposal(GenView(rng), time.Now(), utils.GenSlice(rng, GenLaneRange), utils.Some(GenAppProposal(rng)))
}

// GenProposalAt generates a Proposal at a specific view.
func GenProposalAt(rng utils.Rng, view View) *Proposal {
	return newProposal(view, time.Now(), utils.GenSlice(rng, GenLaneRange), utils.Some(GenAppProposal(rng)))
}

// GenAppHash generates a random AppHash.
func GenAppHash(rng utils.Rng) AppHash {
	return AppHash(utils.GenBytes(rng, 32))
}

// GenAppProposal generates a random AppProposal.
func GenAppProposal(rng utils.Rng) *AppProposal {
	return NewAppProposal(GenGlobalBlockNumber(rng), GenRoadIndex(rng), GenAppHash(rng))
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
	vote := GenCommitVote(rng)
	return NewCommitQC(utils.GenSlice(
		rng,
		func(rng utils.Rng) *Signed[*CommitVote] { return GenSigned(rng, vote) },
	))
}

// GenFullCommitQC generates a random FullCommitQC.
func GenFullCommitQC(rng utils.Rng) *FullCommitQC {
	return &FullCommitQC{
		qc:      GenCommitQC(rng),
		headers: utils.GenSlice(rng, GenBlockHeader),
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
