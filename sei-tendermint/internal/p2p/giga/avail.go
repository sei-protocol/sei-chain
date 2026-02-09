package giga

import (
	"context"
	"errors"
	"fmt"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/data"
	apb "github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/pb"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/p2p/giga/pb"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/p2p/rpc"
)

func (x *Service) serverStreamLaneProposals(ctx context.Context, server rpc.Server[API]) error {
	return StreamLaneProposals.Serve(ctx, server, func(ctx context.Context, stream rpc.Stream[*pb.LaneProposal, *pb.StreamLaneProposalsReq]) error {
		req, err := stream.Recv(ctx)
		if err != nil {
			return err
		}
		sub := x.state.Avail().SubscribeLaneProposals(types.BlockNumber(req.FirstBlockNumber))
		for {
			p, err := sub.Recv(ctx)
			if err != nil {
				return err
			}
			if err := stream.Send(ctx, &pb.LaneProposal{
				LaneProposal: types.SignedMsgConv[*types.LaneProposal]().Encode(p),
			}); err != nil {
				return fmt.Errorf("stream.Send(): %w", err)
			}
		}
	})
}

func (x *Service) serverStreamLaneVotes(ctx context.Context, server rpc.Server[API]) error {
	return StreamLaneVotes.Serve(ctx, server, func(ctx context.Context, stream rpc.Stream[*pb.LaneVote, *pb.StreamLaneVotesReq]) error {
		req, err := stream.Recv(ctx)
		if err != nil {
			return err
		}
		_ = req
		sub := x.state.Avail().SubscribeLaneVotes()
		for {
			batch, err := sub.RecvBatch(ctx)
			if err != nil {
				return err
			}
			for _, vote := range batch {
				signedVote := types.SignedMsgConv[*types.LaneVote]().Encode(vote)
				if err := stream.Send(ctx, &pb.LaneVote{LaneVote: signedVote}); err != nil {
					return fmt.Errorf("stream.Send(): %w", err)
				}
			}
		}
	})
}

func (x *Service) serverStreamAppVotes(ctx context.Context, server rpc.Server[API]) error {
	return StreamAppVotes.Serve(ctx, server, func(ctx context.Context, stream rpc.Stream[*pb.AppVote, *pb.StreamAppVotesReq]) error {
		req, err := stream.Recv(ctx)
		if err != nil {
			return err
		}
		_ = req
		sub := x.state.Avail().SubscribeAppVotes()
		for {
			vote, err := sub.Recv(ctx)
			if err != nil {
				return err
			}
			signedVote := types.SignedMsgConv[*types.AppVote]().Encode(vote)
			if err := stream.Send(ctx, &pb.AppVote{AppVote: signedVote}); err != nil {
				return fmt.Errorf("stream.Send(): %w", err)
			}
		}
	})
}

func (x *Service) serverStreamAppQCs(ctx context.Context, server rpc.Server[API]) error {
	return StreamAppQCs.Serve(ctx, server, func(ctx context.Context, stream rpc.Stream[*pb.StreamAppQCsResp, *pb.StreamAppQCsReq]) error {
		req, err := stream.Recv(ctx)
		if err != nil {
			return err
		}
		_ = req
		next := types.RoadIndex(0)
		for {
			appQC, commitQC, err := x.state.Avail().WaitForAppQC(ctx, next)
			if err != nil {
				return fmt.Errorf("x.state.Avail().WaitForAppQC(): %w", err)
			}
			next = appQC.Next()
			if err := stream.Send(ctx, &pb.StreamAppQCsResp{
				AppQc:    types.AppQCConv.Encode(appQC),
				CommitQc: types.CommitQCConv.Encode(commitQC),
			}); err != nil {
				return fmt.Errorf("stream.Send(): %w", err)
			}
		}
	})
}

func (x *Service) serverStreamCommitQCs(ctx context.Context, server rpc.Server[API]) error {
	return StreamCommitQCs.Serve(ctx, server, func(ctx context.Context, stream rpc.Stream[*apb.CommitQC, *pb.StreamCommitQCsReq]) error {
		next := types.RoadIndex(0)
		for {
			qc, err := x.state.Avail().CommitQC(ctx, next)
			if err != nil {
				if errors.Is(err, data.ErrPruned) {
					next = x.state.Avail().FirstCommitQC()
					continue
				}
				return fmt.Errorf("x.state.Avail().FirstCommitQC(): %w", err)
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
	req := &pb.StreamLaneProposalsReq{}
	// TODO(gprusak): dissemination of LaneProposals is the main source of bandwidth consumption.
	// * to keep low latency, we need to push the lane proposals (streaming is required)
	// * to avoid wasting bandwidth, we should set req.FirstBlockNumber (for that we need to authenticate validator in handshake)
	// * the current implementation assumes a fully connected network - with a different topology we will need to be smarter.
	if err := stream.Send(ctx, req); err != nil {
		return fmt.Errorf("client.StreamLaneProposals(): %w", err)
	}
	for {
		rawProposal, err := stream.Recv(ctx)
		if err != nil {
			return fmt.Errorf("stream.Recv(): %w", err)
		}
		proposal, err := types.SignedMsgConv[*types.LaneProposal]().Decode(rawProposal.LaneProposal)
		if err != nil {
			return fmt.Errorf("types.LaneProposalConv.Decode(): %w", err)
		}
		// Sanity check, checking that the producer only sends their own proposals.
		// TODO(gprusak): authenticate the peer to be able to do this check.
		/*if got, want := proposal.Msg().Block().Header().Lane(), c.cfg.GetKey(); got != want {
			return fmt.Errorf("producer = %q, want %q", got, want)
		}*/
		if err := x.state.Avail().PushBlock(ctx, proposal); err != nil {
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
		vote, err := types.SignedMsgConv[*types.LaneVote]().Decode(rawVote.LaneVote)
		if err != nil {
			return fmt.Errorf("types.LaneVoteConv.Decode(): %w", err)
		}
		if err := x.state.Avail().PushVote(ctx, vote); err != nil {
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
		if err := x.state.Avail().PushCommitQC(ctx, qc); err != nil {
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
		vote, err := types.SignedMsgConv[*types.AppVote]().Decode(rawVote.AppVote)
		if err != nil {
			return fmt.Errorf("types.LaneVoteConv.Decode(): %w", err)
		}
		if err := x.state.Avail().PushAppVote(ctx, vote); err != nil {
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
		appQC, err := types.AppQCConv.Decode(resp.AppQc)
		if err != nil {
			return fmt.Errorf("types.AppQCConv.Decode(): %w", err)
		}
		commitQC, err := types.CommitQCConv.Decode(resp.CommitQc)
		if err != nil {
			return fmt.Errorf("types.CommitQCConv.Decode(): %w", err)
		}
		if err := x.state.Avail().PushAppQC(appQC, commitQC); err != nil {
			return fmt.Errorf("s.PushFirstCommitQC(): %w", err)
		}
	}
}
