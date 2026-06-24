package giga

import (
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/p2p/giga/pb"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/protoutils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
)

type StreamLaneProposalsReq struct {
	FirstBlockNumber types.BlockNumber
}

type StreamAppQCsResp struct {
	AppQC    *types.AppQC
	CommitQC *types.CommitQC
}

type GetBlockReq struct {
	GlobalNumber types.GlobalBlockNumber
}

type StreamFullCommitQCsReq struct {
	NextBlock types.GlobalBlockNumber
}

var LaneVoteConv = protoutils.Conv[*types.Signed[*types.LaneVote], *pb.LaneVote]{
	Encode: func(m *types.Signed[*types.LaneVote]) *pb.LaneVote {
		return &pb.LaneVote{
			LaneVoteV2: types.SignedLaneVoteConv.Encode(m),
		}
	},
	Decode: func(m *pb.LaneVote) (*types.Signed[*types.LaneVote], error) {
		laneVote, err := types.SignedLaneVoteConv.DecodeReq(m.LaneVoteV2)
		if err != nil {
			return nil, fmt.Errorf("laneVote: %w", err)
		}
		return laneVote, nil
	},
}

var LaneProposalConv = protoutils.Conv[*types.Signed[*types.LaneProposal], *pb.LaneProposal]{
	Encode: func(m *types.Signed[*types.LaneProposal]) *pb.LaneProposal {
		return &pb.LaneProposal{
			LaneProposalV2: types.SignedLaneProposalConv.Encode(m),
		}
	},
	Decode: func(m *pb.LaneProposal) (*types.Signed[*types.LaneProposal], error) {
		laneProposal, err := types.SignedLaneProposalConv.DecodeReq(m.LaneProposalV2)
		if err != nil {
			return nil, fmt.Errorf("laneProposal: %w", err)
		}
		return laneProposal, nil
	},
}

var AppVoteConv = protoutils.Conv[*types.Signed[*types.AppVote], *pb.AppVote]{
	Encode: func(m *types.Signed[*types.AppVote]) *pb.AppVote {
		return &pb.AppVote{
			AppVoteV2: types.SignedAppVoteConv.Encode(m),
		}
	},
	Decode: func(m *pb.AppVote) (*types.Signed[*types.AppVote], error) {
		appVote, err := types.SignedAppVoteConv.DecodeReq(m.AppVoteV2)
		if err != nil {
			return nil, fmt.Errorf("appVote: %w", err)
		}
		return appVote, nil
	},
}

var StreamLaneProposalsReqConv = protoutils.Conv[*StreamLaneProposalsReq, *pb.StreamLaneProposalsReq]{
	Encode: func(m *StreamLaneProposalsReq) *pb.StreamLaneProposalsReq {
		return &pb.StreamLaneProposalsReq{FirstBlockNumber: uint64(m.FirstBlockNumber)}
	},
	Decode: func(m *pb.StreamLaneProposalsReq) (*StreamLaneProposalsReq, error) {
		return &StreamLaneProposalsReq{FirstBlockNumber: types.BlockNumber(m.FirstBlockNumber)}, nil
	},
}

var StreamAppQCsRespConv = protoutils.Conv[*StreamAppQCsResp, *pb.StreamAppQCsResp]{
	Encode: func(m *StreamAppQCsResp) *pb.StreamAppQCsResp {
		return &pb.StreamAppQCsResp{
			AppQc:    types.AppQCConv.Encode(m.AppQC),
			CommitQc: types.CommitQCConv.Encode(m.CommitQC),
		}
	},
	Decode: func(m *pb.StreamAppQCsResp) (*StreamAppQCsResp, error) {
		appQC, err := types.AppQCConv.DecodeReq(m.AppQc)
		if err != nil {
			return nil, fmt.Errorf("appQC: %w", err)
		}
		commitQC, err := types.CommitQCConv.DecodeReq(m.CommitQc)
		if err != nil {
			return nil, fmt.Errorf("commitQC: %w", err)
		}
		return &StreamAppQCsResp{AppQC: appQC, CommitQC: commitQC}, nil
	},
}

var GetBlockReqConv = protoutils.Conv[*GetBlockReq, *pb.GetBlockReq]{
	Encode: func(m *GetBlockReq) *pb.GetBlockReq {
		return &pb.GetBlockReq{GlobalNumber: uint64(m.GlobalNumber)}
	},
	Decode: func(m *pb.GetBlockReq) (*GetBlockReq, error) {
		return &GetBlockReq{GlobalNumber: types.GlobalBlockNumber(m.GlobalNumber)}, nil
	},
}

var GetBlockRespConv = protoutils.Conv[utils.Option[*types.Block], *pb.GetBlockResp]{
	Encode: func(m utils.Option[*types.Block]) *pb.GetBlockResp {
		return &pb.GetBlockResp{Block: types.BlockConv.EncodeOpt(m)}
	},
	Decode: func(m *pb.GetBlockResp) (utils.Option[*types.Block], error) {
		block, err := types.BlockConv.DecodeOpt(m.Block)
		if err != nil {
			return utils.None[*types.Block](), fmt.Errorf("block: %w", err)
		}
		return block, nil
	},
}

var StreamFullCommitQCsReqConv = protoutils.Conv[*StreamFullCommitQCsReq, *pb.StreamFullCommitQCsReq]{
	Encode: func(m *StreamFullCommitQCsReq) *pb.StreamFullCommitQCsReq {
		return &pb.StreamFullCommitQCsReq{NextBlock: uint64(m.NextBlock)}
	},
	Decode: func(m *pb.StreamFullCommitQCsReq) (*StreamFullCommitQCsReq, error) {
		return &StreamFullCommitQCsReq{NextBlock: types.GlobalBlockNumber(m.NextBlock)}, nil
	},
}
