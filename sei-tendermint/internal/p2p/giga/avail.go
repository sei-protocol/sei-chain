package giga

import (
	"context"
	"errors"
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	apb "github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/pb"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/p2p/giga/pb"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/p2p/rpc"
)

func (x *Service) serverStreamLaneProposals(ctx context.Context, server rpc.Server[API]) error {
	return StreamLaneProposals.Serve(ctx, server, func(ctx context.Context, stream rpc.Stream[*pb.LaneProposal, *pb.StreamLaneProposalsReq]) error {
		reqRaw, err := stream.Recv(ctx)
		if err != nil {
			return err
		}
		req, err := StreamLaneProposalsReqConv.Decode(reqRaw)
		if err != nil {
			return fmt.Errorf("StreamLaneProposalsReqConv.Decode(): %w", err)
		}
		sub := x.validatorState().Avail().SubscribeLaneProposals(req.FirstBlockNumber)
		for {
			p, err := sub.Recv(ctx)
			if err != nil {
				return err
			}
			if err := stream.Send(ctx, LaneProposalConv.Encode(p)); err != nil {
				return fmt.Errorf("stream.Send(): %w", err)
			}
		}
	})
}

func (x *Service) serverStreamLaneVotes(ctx context.Context, server rpc.Server[API]) error {
	return StreamLaneVotes.Serve(ctx, server, func(ctx context.Context, stream rpc.Stream[*pb.LaneVote, *pb.StreamLaneVotesReq]) error {
		reqRaw, err := stream.Recv(ctx)
		if err != nil {
			return err
		}
		_ = reqRaw
		sub := x.validatorState().Avail().SubscribeLaneVotes()
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

func (x *Service) serverStreamAppVotes(ctx context.Context, server rpc.Server[API]) error {
	return StreamAppVotes.Serve(ctx, server, func(ctx context.Context, stream rpc.Stream[*pb.AppVote, *pb.StreamAppVotesReq]) error {
		reqRaw, err := stream.Recv(ctx)
		if err != nil {
			return err
		}
		_ = reqRaw
		sub := x.validatorState().Avail().SubscribeAppVotes()
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

func (x *Service) serverStreamAppQCs(ctx context.Context, server rpc.Server[API]) error {
	return StreamAppQCs.Serve(ctx, server, func(ctx context.Context, stream rpc.Stream[*pb.StreamAppQCsResp, *pb.StreamAppQCsReq]) error {
		reqRaw, err := stream.Recv(ctx)
		if err != nil {
			return err
		}
		_ = reqRaw
		next := types.RoadIndex(0)
		for {
			appQC, commitQC, err := x.validatorState().Avail().WaitForAppQC(ctx, next)
			if err != nil {
				return fmt.Errorf("x.validatorState().Avail().WaitForAppQC(): %w", err)
			}
			next = appQC.Next()
			if err := stream.Send(ctx, StreamAppQCsRespConv.Encode(&StreamAppQCsResp{
				AppQC:    appQC,
				CommitQC: commitQC,
			})); err != nil {
				return fmt.Errorf("stream.Send(): %w", err)
			}
		}
	})
}

func (x *Service) serverStreamCommitQCs(ctx context.Context, server rpc.Server[API]) error {
	return StreamCommitQCs.Serve(ctx, server, func(ctx context.Context, stream rpc.Stream[*apb.CommitQC, *pb.StreamCommitQCsReq]) error {
		next := types.RoadIndex(0)
		for {
			qc, err := x.validatorState().Avail().CommitQC(ctx, next)
			if err != nil {
				if errors.Is(err, types.ErrPruned) {
					next = x.validatorState().Avail().FirstCommitQC()
					continue
				}
				return fmt.Errorf("x.validatorState().Avail().FirstCommitQC(): %w", err)
			}
			next = qc.Index() + 1
			if err := stream.Send(ctx, types.CommitQCConv.Encode(qc)); err != nil {
				return fmt.Errorf("stream.Send(): %w", err)
			}
		}
	})
}

func (x *Service) clientStreamLaneProposals(ctx context.Context, c rpc.Client[API]) error {
	stream, err := StreamLaneProposals.Call(ctx, c)
	if err != nil {
		return err
	}
	defer stream.Close()
	req := &StreamLaneProposalsReq{}
	// TODO(gprusak): dissemination of LaneProposals is the main source of bandwidth consumption.
	// * to keep low latency, we need to push the lane proposals (streaming is required)
	// * to avoid wasting bandwidth, we should set req.FirstBlockNumber (for that we need to authenticate validator in handshake)
	// * the current implementation assumes a fully connected network - with a different topology we will need to be smarter.
	if err := stream.Send(ctx, StreamLaneProposalsReqConv.Encode(req)); err != nil {
		return fmt.Errorf("client.StreamLaneProposals(): %w", err)
	}
	for {
		rawProposal, err := stream.Recv(ctx)
		if err != nil {
			return fmt.Errorf("stream.Recv(): %w", err)
		}
		proposal, err := LaneProposalConv.Decode(rawProposal)
		if err != nil {
			return fmt.Errorf("LaneProposalConv.Decode(): %w", err)
		}
		// Sanity check, checking that the producer only sends their own proposals.
		// TODO(gprusak): authenticate the peer to be able to do this check.
		/*if got, want := proposal.Msg().Block().Header().Lane(), c.cfg.GetKey(); got != want {
			return fmt.Errorf("producer = %q, want %q", got, want)
		}*/
		if err := x.validatorState().Avail().PushBlock(ctx, proposal); err != nil {
			return fmt.Errorf("s.PushLaneProposal(): %w", err)
		}
	}
}

func (x *Service) clientStreamLaneVotes(ctx context.Context, c rpc.Client[API]) error {
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
		if err := x.validatorState().Avail().PushVote(ctx, vote); err != nil {
			return fmt.Errorf("s.PushLaneVote(): %w", err)
		}
	}
}

func (x *Service) clientStreamCommitQCs(ctx context.Context, c rpc.Client[API]) error {
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
		if err := x.validatorState().Avail().PushCommitQC(ctx, qc); err != nil {
			return fmt.Errorf("s.PushFirstCommitQC(): %w", err)
		}
	}
}

func (x *Service) clientStreamAppVotes(ctx context.Context, c rpc.Client[API]) error {
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
		if err := x.validatorState().Avail().PushAppVote(ctx, vote); err != nil {
			return fmt.Errorf("s.PushLaneVote(): %w", err)
		}
	}
}

func (x *Service) clientStreamAppQCs(ctx context.Context, c rpc.Client[API]) error {
	stream, err := StreamAppQCs.Call(ctx, c)
	if err != nil {
		return fmt.Errorf("client.StreamAppQCs(): %w", err)
	}
	defer stream.Close()
	if err := stream.Send(ctx, &pb.StreamAppQCsReq{}); err != nil {
		return err
	}
	for {
		resp, err := stream.Recv(ctx)
		if err != nil {
			return fmt.Errorf("stream.Recv(): %w", err)
		}
		msg, err := StreamAppQCsRespConv.Decode(resp)
		if err != nil {
			return fmt.Errorf("StreamAppQCsRespConv.Decode(): %w", err)
		}
		if err := x.validatorState().Avail().PushAppQC(ctx, msg.AppQC, msg.CommitQC); err != nil {
			return fmt.Errorf("s.PushFirstCommitQC(): %w", err)
		}
	}
}
