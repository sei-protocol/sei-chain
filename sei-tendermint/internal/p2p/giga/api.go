package giga

import (
	apb "github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/pb"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/types"
	pb "github.com/sei-protocol/sei-chain/sei-tendermint/internal/p2p/giga/pb"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/p2p/rpc"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/protoutils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
)

const kB rpc.InBytes = 1024
const MB rpc.InBytes = 1024 * kB

type API struct{}

const maxValidators = 50

var protoBounds = func() protoutils.BoundMap {
	F := protoutils.Field
	type B = protoutils.Bound
	pub := types.PublicKeyConv.Encode(types.PublicKey{})
	sig := types.SignatureConv.Encode(&types.Signature{})
	bh := types.BlockHeaderConv.Encode(&types.BlockHeader{})
	lr := types.LaneRangeConv.Encode(&types.LaneRange{})
	return protoutils.BoundMap{
		F("autobahn.PublicKey.ed25519"):        B{Size: len(pub.Ed25519)},
		F("autobahn.Signature.sig"):            B{Size: len(sig.Sig)},
		F("autobahn.BlockHeader.parent_hash"):  B{Size: len(bh.ParentHash)},
		F("autobahn.BlockHeader.payload_hash"): B{Size: len(bh.PayloadHash)},
		F("autobahn.LaneRange.last_hash"):      B{Size: len(lr.LastHash)},
		F("autobahn.Proposal.lane_ranges"):     B{Count: maxValidators},
		F("autobahn.FullProposal.lane_qcs"):    B{Count: maxValidators},
		F("autobahn.LaneQC.sigs"):              B{Count: maxValidators},
		F("autobahn.AppQC.sigs"):               B{Count: maxValidators},
		F("autobahn.TimeoutQC.votes"):          B{Count: maxValidators},
		F("autobahn.PrepareQC.sigs"):           B{Count: maxValidators},
		F("autobahn.Payload.txs"): B{
			Count: utils.MustCast[int](types.MaxTxsPerBlock),
			Size:  utils.MustCast[int](types.StandardTxBytes),
		},
		F("autobahn.AppProposal.app_hash"): B{Size: 100}, // safe bound on the application-defined app hash.
	}
}()

var consensusReqMaxSize = (300 * kB).MustAtLeast(rpc.InBytes(protoutils.MaxSize[*apb.ConsensusReq](protoBounds)))
var getBlockRespMaxSize = (2 * MB).MustAtLeast(rpc.InBytes(protoutils.MaxSize[*pb.GetBlockResp](protoBounds)))
var laneProposalMaxSize = (2 * MB).MustAtLeast(rpc.InBytes(protoutils.MaxSize[*pb.LaneProposal](protoBounds)))

var Ping = rpc.Register[API](
	0,
	rpc.Limit{Rate: 1, Concurrent: 2},
	rpc.Msg[*pb.PingReq]{MsgSize: kB, Window: 1},
	rpc.Msg[*pb.PingResp]{MsgSize: kB, Window: 1},
)
var StreamLaneProposals = rpc.Register[API](
	1,
	rpc.Limit{Rate: 1, Concurrent: 1},
	rpc.Msg[*pb.StreamLaneProposalsReq]{MsgSize: kB, Window: 1},
	rpc.Msg[*pb.LaneProposal]{MsgSize: laneProposalMaxSize, Window: 5},
)
var StreamLaneVotes = rpc.Register[API](
	2,
	rpc.Limit{Rate: 1, Concurrent: 1},
	rpc.Msg[*pb.StreamLaneVotesReq]{MsgSize: kB, Window: 1},
	rpc.Msg[*pb.LaneVote]{MsgSize: 10 * kB, Window: 100},
)
var StreamCommitQCs = rpc.Register[API](
	3,
	rpc.Limit{Rate: 1, Concurrent: 1},
	rpc.Msg[*pb.StreamCommitQCsReq]{MsgSize: kB, Window: 1},
	rpc.Msg[*apb.CommitQC]{MsgSize: 10 * kB, Window: 20},
)
var StreamAppVotes = rpc.Register[API](
	4,
	rpc.Limit{Rate: 1, Concurrent: 1},
	rpc.Msg[*pb.StreamAppVotesReq]{MsgSize: kB, Window: 1},
	rpc.Msg[*pb.AppVote]{MsgSize: 10 * kB, Window: 100},
)
var StreamAppQCs = rpc.Register[API](5,
	rpc.Limit{Rate: 1, Concurrent: 1},
	rpc.Msg[*pb.StreamAppQCsReq]{MsgSize: kB, Window: 1},
	rpc.Msg[*pb.StreamAppQCsResp]{MsgSize: 10 * kB, Window: 20},
)
var Consensus = rpc.Register[API](6,
	// Consensus streams are special in a sense that
	// * each stream sends just 1 message per view
	// * messages are streamed from client to server
	// * there are many stream (1 per message type)
	// This is an artifact of how Consensus was initially implemented,
	// but it can be made to be consistent with all other streaming RPCs.
	rpc.Limit{Rate: 10, Concurrent: 10},
	rpc.Msg[*apb.ConsensusReq]{MsgSize: consensusReqMaxSize, Window: 1},
	rpc.Msg[*pb.ConsensusResp]{MsgSize: kB, Window: 1},
)
var StreamFullCommitQCs = rpc.Register[API](7,
	rpc.Limit{Rate: 1, Concurrent: 1},
	rpc.Msg[*pb.StreamFullCommitQCsReq]{MsgSize: kB, Window: 1},
	rpc.Msg[*apb.FullCommitQC]{MsgSize: 100 * kB, Window: 20},
)
var GetBlock = rpc.Register[API](8,
	rpc.Limit{Rate: 10, Concurrent: 10},
	rpc.Msg[*pb.GetBlockReq]{MsgSize: 10 * kB, Window: 1},
	rpc.Msg[*pb.GetBlockResp]{
		MsgSize: getBlockRespMaxSize,
		Window:  1,
	},
)
