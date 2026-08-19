package giga

import (
	"context"
	"errors"
	"fmt"

	"github.com/sei-protocol/seilog"

	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/avail"
	apb "github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/pb"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/p2p/giga/pb"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/p2p/mux"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/p2p/rpc"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
)

var logger = seilog.NewLogger("tendermint", "internal", "p2p", "giga")

func (x *validatorService) serverStreamLaneProposals(ctx context.Context, server rpc.Server[API]) error {
	return StreamLaneProposals.Serve(ctx, server, func(ctx context.Context, stream rpc.Stream[*pb.LaneProposal, *pb.StreamLaneProposalsReq]) error {
		reqRaw, err := stream.Recv(ctx)
		if err != nil {
			return err
		}
		req, err := StreamLaneProposalsReqConv.Decode(reqRaw)
		if err != nil {
			return fmt.Errorf("StreamLaneProposalsReqConv.Decode(): %w", err)
		}
		local := x.state.Avail().PublicKey()
		if req.LaneID.Validator != local {
			logger.Warn("StreamLaneProposals: lane validator mismatch; ending stream",
				"requested", req.LaneID.Validator, "local", local)
			return nil
		}
		sub := x.state.Avail().SubscribeLaneProposals(req.LaneID, req.FirstBlockNumber)
		for {
			p, err := sub.Recv(ctx)
			if err != nil {
				if errors.Is(err, avail.ErrLaneClosed) {
					return nil
				}
				return err
			}
			if err := stream.Send(ctx, LaneProposalConv.Encode(p)); err != nil {
				return fmt.Errorf("stream.Send(): %w", err)
			}
		}
	})
}

func (x *validatorService) serverStreamLaneVotes(ctx context.Context, server rpc.Server[API]) error {
	return StreamLaneVotes.Serve(ctx, server, func(ctx context.Context, stream rpc.Stream[*pb.LaneVote, *pb.StreamLaneVotesReq]) error {
		reqRaw, err := stream.Recv(ctx)
		if err != nil {
			return err
		}
		_ = reqRaw
		sub := x.state.Avail().SubscribeLaneVotes()
		for {
			batch, err := sub.RecvBatch(ctx)
			if err != nil {
				return err
			}
			for _, vote := range batch {
				if err := stream.Send(ctx, LaneVoteConv.Encode(vote)); err != nil {
					return fmt.Errorf("stream.Send(): %w", err)
				}
			}
		}
	})
}

func (x *validatorService) serverStreamAppVotes(ctx context.Context, server rpc.Server[API]) error {
	return StreamAppVotes.Serve(ctx, server, func(ctx context.Context, stream rpc.Stream[*pb.AppVote, *pb.StreamAppVotesReq]) error {
		reqRaw, err := stream.Recv(ctx)
		if err != nil {
			return err
		}
		_ = reqRaw
		sub := x.state.Avail().SubscribeAppVotes()
		for {
			vote, err := sub.Recv(ctx)
			if err != nil {
				return err
			}
			if err := stream.Send(ctx, AppVoteConv.Encode(vote)); err != nil {
				return fmt.Errorf("stream.Send(): %w", err)
			}
		}
	})
}

func (x *validatorService) serverStreamCommitQCs(ctx context.Context, server rpc.Server[API]) error {
	return StreamCommitQCs.Serve(ctx, server, func(ctx context.Context, stream rpc.Stream[*apb.CommitQC, *pb.StreamCommitQCsReq]) error {
		next := types.RoadIndex(0)
		for {
			qc, err := x.state.Avail().CommitQC(ctx, next)
			if err != nil {
				if errors.Is(err, types.ErrPruned) {
					next = x.state.Avail().First()
					continue
				}
				return fmt.Errorf("x.state.Avail().CommitQC(): %w", err)
			}
			next = qc.Index() + 1
			if err := stream.Send(ctx, types.CommitQCConv.Encode(qc)); err != nil {
				return fmt.Errorf("stream.Send(): %w", err)
			}
		}
	})
}

func (x *validatorService) clientStreamLaneProposals(ctx context.Context, c rpc.Client[API], peer types.PublicKey) error {
	a := x.state.Avail()
	for ctx.Err() == nil {
		// Wait on the peer's current committee LaneID: returns immediately for a
		// stay/transient redial, blocks across leave until rejoin.
		lane, err := a.WaitForNextLane(ctx, peer, utils.None[types.LaneID]())
		if err != nil {
			return err
		}
		if err := x.streamLaneProposalsOnce(ctx, c, lane, a.NextBlock(lane)); err != nil {
			return err
		}
	}
	return ctx.Err()
}

func (x *validatorService) streamLaneProposalsOnce(ctx context.Context, c rpc.Client[API], lane types.LaneID, first types.BlockNumber) error {
	stream, err := StreamLaneProposals.Call(ctx, c)
	if err != nil {
		return err
	}
	defer stream.Close()
	// TODO(gprusak): dissemination of LaneProposals is the main source of bandwidth consumption.
	// To keep low latency, we need to push the lane proposals (streaming is required).
	req := &StreamLaneProposalsReq{LaneID: lane, FirstBlockNumber: first}
	if err := stream.Send(ctx, StreamLaneProposalsReqConv.Encode(req)); err != nil {
		return fmt.Errorf("client.StreamLaneProposals(): %w", err)
	}
	for {
		rawProposal, err := stream.Recv(ctx)
		if err != nil {
			// Server closed after the lane closed (handler returns nil, mux CLOSE).
			if errors.Is(err, mux.ErrRemoteClosed) {
				return nil
			}
			return fmt.Errorf("stream.Recv(): %w", err)
		}
		proposal, err := LaneProposalConv.Decode(rawProposal)
		if err != nil {
			return fmt.Errorf("LaneProposalConv.Decode(): %w", err)
		}
		if proposal.Msg().Block().Header().Lane() != lane {
			return fmt.Errorf("producer lane = %v, want %v", proposal.Msg().Block().Header().Lane(), lane)
		}
		if err := x.state.Avail().PushBlock(ctx, proposal); err != nil {
			return fmt.Errorf("s.PushLaneProposal(): %w", err)
		}
	}
}

func (x *validatorService) clientStreamLaneVotes(ctx context.Context, c rpc.Client[API]) error {
	stream, err := StreamLaneVotes.Call(ctx, c)
	if err != nil {
		return fmt.Errorf("client.StreamLaneVotes(): %w", err)
	}
	defer stream.Close()
	if err := stream.Send(ctx, &pb.StreamLaneVotesReq{}); err != nil {
		return err
	}
	for {
		rawVote, err := stream.Recv(ctx)
		if err != nil {
			return fmt.Errorf("stream.Recv(): %w", err)
		}
		vote, err := LaneVoteConv.Decode(rawVote)
		if err != nil {
			return fmt.Errorf("LaneVoteConv.Decode(): %w", err)
		}
		if err := x.state.Avail().PushVote(ctx, vote); err != nil {
			return fmt.Errorf("s.PushLaneVote(): %w", err)
		}
	}
}

func (x *validatorService) clientStreamCommitQCs(ctx context.Context, c rpc.Client[API]) error {
	stream, err := StreamCommitQCs.Call(ctx, c)
	if err != nil {
		return fmt.Errorf("client.StreamCommitQCs(): %w", err)
	}
	defer stream.Close()
	if err := stream.Send(ctx, &pb.StreamCommitQCsReq{}); err != nil {
		return err
	}
	for {
		resp, err := stream.Recv(ctx)
		if err != nil {
			return fmt.Errorf("stream.Recv(): %w", err)
		}
		qc, err := types.CommitQCConv.Decode(resp)
		if err != nil {
			return fmt.Errorf("types.CommitQCConv.Decode(): %w", err)
		}
		if err := x.state.Avail().PushCommitQC(ctx, qc); err != nil {
			return fmt.Errorf("s.PushCommitQC(): %w", err)
		}
	}
}

func (x *validatorService) clientStreamAppVotes(ctx context.Context, c rpc.Client[API]) error {
	stream, err := StreamAppVotes.Call(ctx, c)
	if err != nil {
		return fmt.Errorf("client.StreamAppVotes(): %w", err)
	}
	defer stream.Close()
	if err := stream.Send(ctx, &pb.StreamAppVotesReq{}); err != nil {
		return err
	}
	for {
		rawVote, err := stream.Recv(ctx)
		if err != nil {
			return fmt.Errorf("stream.Recv(): %w", err)
		}
		vote, err := AppVoteConv.Decode(rawVote)
		if err != nil {
			return fmt.Errorf("AppVoteConv.Decode(): %w", err)
		}
		if err := x.state.Avail().PushAppVote(ctx, vote); err != nil {
			return fmt.Errorf("s.PushLaneVote(): %w", err)
		}
	}
}
