package giga

import (
	"errors"
	"context"
	"fmt"
	"github.com/tendermint/tendermint/internal/p2p/giga/pb"
	"github.com/tendermint/tendermint/internal/p2p/rpc"
	apb "github.com/tendermint/tendermint/internal/autobahn/pb"
	"github.com/tendermint/tendermint/internal/autobahn/types"
	"github.com/tendermint/tendermint/internal/autobahn/data"
)

// StreamLaneProposals implements pb.StreamAPIServer.
// Streams local blocks starting from the requested number.
func (x *Service) serverStreamLaneProposals(ctx context.Context, server rpc.Server[API]) error {
	return StreamLaneProposals.Serve(ctx,server, func(ctx context.Context, stream rpc.Stream[*pb.LaneProposal,*pb.StreamLaneProposalsReq]) error {
		req,err := stream.Recv(ctx)
		if err!=nil { return err }
		for i := types.BlockNumber(req.FirstBlockNumber); ; i++ {
			b, err := x.avail.Block(ctx, x.avail.key.Public(), i)
			if err != nil {
				if errors.Is(err, data.ErrPruned) {
					continue
				}
				return fmt.Errorf("x.avail.Block(): %w", err)
			}
			proposal := types.Sign(x.avail.key, types.NewLaneProposal(b))
			if err := stream.Send(ctx,&pb.LaneProposal{
				LaneProposal: types.SignedMsgConv[*types.LaneProposal]().Encode(proposal),
			}); err != nil {
				return fmt.Errorf("stream.Send(): %w", err)
			}
		}
	})
}

func (x *Service) serverStreamLaneVotes(ctx context.Context, server rpc.Server[API]) error {
	return StreamLaneVotes.Serve(ctx, server, func(ctx context.Context, stream rpc.Stream[*pb.LaneVote,*pb.StreamLaneVotesReq]) error {
		req,err := stream.Recv(ctx)
		if err!=nil { return err }
		_ = req
		next := map[types.LaneID]types.BlockNumber{}
		for {
			var batch []*types.BlockHeader
			for inner, ctrl := range x.avail.inner.Lock() {
				for {
					for lane, bq := range inner.blocks {
						for i := max(bq.first, next[lane]); i < bq.next; i++ {
							batch = append(batch, bq.q[i].Header())
						}
						next[lane] = bq.next
					}
					if len(batch) > 0 {
						break
					}
					if err := ctrl.Wait(ctx); err != nil {
						return err
					}
				}
			}
			for _, h := range batch {
				vote := types.Sign(x.avail.key, types.NewLaneVote(h))
				signedVote := types.SignedMsgConv[*types.LaneVote]().Encode(vote)
				if err := stream.Send(ctx,&pb.LaneVote{LaneVote: signedVote}); err != nil {
					return fmt.Errorf("stream.Send(): %w", err)
				}
			}
		}
	})
}

func (x *Service) serverStreamAppVotes(ctx context.Context, server rpc.Server[API]) error {
	return StreamAppVotes.Serve(ctx, server, func(ctx context.Context, stream rpc.Stream[*pb.AppVote,*pb.StreamAppVotesReq]) error {
		req,err := stream.Recv(ctx)
		if err!=nil { return err }
		_ = req
		for idx := types.RoadIndex(0); ; idx = max(idx, x.avail.firstCommitQC()) + 1 {
			qc, err := x.avail.CommitQC(ctx, idx)
			if err != nil {
				if errors.Is(err, data.ErrPruned) {
					continue
				}
				return err
			}
			// Send votes for global blocks from this commitQC.
			gr := qc.GlobalRange()
			for n := gr.First; ; n += 1 {
				// Fetch the proposal.
				p, err := x.avail.Data().AppProposal(ctx, n)
				if err != nil {
					if errors.Is(err, data.ErrPruned) {
						continue
					}
					return err
				}
				// AppProposal currently might return a proposal with a higher global number than the one we requested.
				// Correct the n in such a case.
				// TODO(gprusak): simplify, as this is overcomplicated.
				n = p.GlobalNumber()
				if n >= gr.Next {
					break
				}
				// Send the vote.
				vote := types.Sign(x.avail.key, types.NewAppVote(p))
				signedVote := types.SignedMsgConv[*types.AppVote]().Encode(vote)
				if err := stream.Send(&pb.AppVote{AppVote: signedVote}); err != nil {
					return fmt.Errorf("stream.Send(): %w", err)
				}
			}
		}
	})
}

func (x *Service) serverStreamAppQCs(ctx context.Context, server rpc.Server[API]) error {
	return StreamAppQCs.Serve(ctx,server, func(ctx context.Context, stream rpc.Stream[*pb.StreamAppQCsResp,*pb.StreamAppQCsReq]) error {
		req,err := stream.Recv(ctx)
		if err!=nil { return err }
		_ = req
		next := types.RoadIndex(0)
		for {
			appQC, commitQC, err := x.avail.WaitForAppQC(ctx, next)
			if err != nil {
				return fmt.Errorf("x.avail.WaitForAppQC(): %w", err)
			}
			next = appQC.Next()
			if err := stream.Send(ctx,&pb.StreamAppQCsResp{
				AppQc:    types.AppQCConv.Encode(appQC),
				CommitQc: types.CommitQCConv.Encode(commitQC),
			}); err != nil {
				return fmt.Errorf("stream.Send(): %w", err)
			}
		}
	})
}

func (x *Service) serverStreamCommitQCs(ctx context.Context, server rpc.Server[API]) error {
	return StreamCommitQCs.Serve(ctx,server,func(ctx context.Context, stream rpc.Stream[*apb.CommitQC,*pb.StreamCommitQCsReq]) error {
		next := types.RoadIndex(0)
		for {
			qc, err := x.avail.CommitQC(ctx, next)
			if err != nil {
				if errors.Is(err, data.ErrPruned) {
					next = x.avail.firstCommitQC()
					continue
				}
				return fmt.Errorf("x.avail.FirstCommitQC(): %w", err)
			}
			next = qc.Index() + 1
			if err := stream.Send(ctx, types.CommitQCConv.Encode(qc)); err != nil {
				return fmt.Errorf("stream.Send(): %w", err)
			}
		}
	})
}

func (x *Service) clientStreamLaneProposals(ctx context.Context, c rpc.Client[API]) error {
	stream, err := StreamLaneProposals.Call(ctx,c)
	if err!=nil { return err }
	defer stream.Close()
	if err := stream.Send(ctx,&pb.StreamLaneProposalsReq{FirstBlockNumber: uint64(x.avail.NextBlock(c.cfg.GetKey()))}); err != nil {
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
		if got, want := proposal.Msg().Block().Header().Lane(), c.cfg.GetKey(); got != want {
			return fmt.Errorf("producer = %q, want %q", got, want)
		}
		if err := x.avail.PushBlock(ctx, proposal); err != nil {
			return fmt.Errorf("s.PushLaneProposal(): %w", err)
		}
	}
}

func (x *Service) clientStreamLaneVotes(ctx context.Context, c rpc.Client[API]) error {
	stream, err := StreamLaneVotes.Call(ctx, c)
	if err != nil { return fmt.Errorf("client.StreamLaneVotes(): %w", err) }
	defer stream.Close()
	if err :=stream.Send(ctx, &pb.StreamLaneVotesReq{}); err!=nil { return err }
	for {
		rawVote, err := stream.Recv(ctx)
		if err != nil {
			return fmt.Errorf("stream.Recv(): %w", err)
		}
		vote, err := types.SignedMsgConv[*types.LaneVote]().Decode(rawVote.LaneVote)
		if err != nil {
			return fmt.Errorf("types.LaneVoteConv.Decode(): %w", err)
		}
		if err := x.avail.PushVote(ctx, vote); err != nil {
			return fmt.Errorf("s.PushLaneVote(): %w", err)
		}
	}
}

func (x *Service) clientStreamCommitQCs(ctx context.Context, c rpc.Client[API]) error {
	stream, err := StreamCommitQCs.Call(ctx,c)
	if err != nil {
		return fmt.Errorf("client.StreamCommitQCs(): %w", err)
	}
	defer stream.Close()
	if err:=stream.Send(ctx,&pb.StreamCommitQCsReq{}); err!=nil { return err  }
	for {
		resp, err := stream.Recv(ctx)
		if err != nil {
			return fmt.Errorf("stream.Recv(): %w", err)
		}
		qc, err := types.CommitQCConv.Decode(resp)
		if err != nil {
			return fmt.Errorf("types.CommitQCConv.Decode(): %w", err)
		}
		if err := x.avail.PushCommitQC(ctx, qc); err != nil {
			return fmt.Errorf("s.PushFirstCommitQC(): %w", err)
		}
	}
}

func (x *Service) clientStreamAppVotes(ctx context.Context, c rpc.Client[API]) error {
	stream, err := StreamAppVotes.Call(ctx,c)
	if err != nil {
		return fmt.Errorf("client.StreamAppVotes(): %w", err)
	}
	defer stream.Close()
	if err:=stream.Send(ctx,&pb.StreamAppVotesReq{}); err!=nil { return err }
	for {
		rawVote, err := stream.Recv(ctx)
		if err != nil {
			return fmt.Errorf("stream.Recv(): %w", err)
		}
		vote, err := types.SignedMsgConv[*types.AppVote]().Decode(rawVote.AppVote)
		if err != nil {
			return fmt.Errorf("types.LaneVoteConv.Decode(): %w", err)
		}
		if err := x.avail.PushAppVote(ctx, vote); err != nil {
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
	if err:=stream.Send(ctx,&pb.StreamAppQCsReq{}); err!=nil { return err }
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
		if err := x.avail.PushAppQC(appQC, commitQC); err != nil {
			return fmt.Errorf("s.PushFirstCommitQC(): %w", err)
		}
	}
}
