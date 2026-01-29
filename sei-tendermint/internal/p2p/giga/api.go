package giga

import (
	pb "github.com/tendermint/tendermint/internal/p2p/giga/pb"
	apb "github.com/tendermint/tendermint/internal/autobahn/pb"
	"github.com/tendermint/tendermint/internal/p2p/rpc"
)

const kB rpc.InBytes = 1024
const MB rpc.InBytes = 1024 * kB

type API struct{}

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
	rpc.Msg[*pb.LaneProposal]{MsgSize: 2 * MB, Window: 5},
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
	rpc.Limit{Rate: 1, Concurrent: 10},
	rpc.Msg[*apb.ConsensusReq]{MsgSize: 10 * kB, Window: 1},
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
	rpc.Msg[*pb.GetBlockResp]{MsgSize: 2 * MB, Window: 1},
)
