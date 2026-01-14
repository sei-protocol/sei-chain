package avail

import (
	"errors"
	"fmt"

	"google.golang.org/grpc"

	"github.com/tendermint/tendermint/internal/autobahn/data"
	"github.com/tendermint/tendermint/internal/autobahn/pkg/protocol"
	"github.com/tendermint/tendermint/internal/autobahn/types"
)

type server struct {
	protocol.AvailAPIServer
	state *State
}

// Register registers DataAPIServer with the given grpc server.
func (s *State) Register(grpcServer *grpc.Server) {
	protocol.RegisterAvailAPIServer(grpcServer, &server{
		state: s,
	})
}

// StreamLaneProposals implements protocol.StreamAPIServer.
// Streams local blocks starting from the requested number.
func (s *server) StreamLaneProposals(
	req *protocol.StreamLaneProposalsReq,
	stream grpc.ServerStreamingServer[protocol.LaneProposal],
) error {
	ctx := stream.Context()
	for i := types.BlockNumber(req.FirstBlockNumber); ; i++ {
		b, err := s.state.Block(ctx, s.state.key.Public(), i)
		if err != nil {
			if errors.Is(err, data.ErrPruned) {
				continue
			}
			return fmt.Errorf("s.state.Block(): %w", err)
		}
		proposal := types.Sign(s.state.key, types.NewLaneProposal(b))
		if err := stream.Send(&protocol.LaneProposal{
			LaneProposal: types.SignedMsgConv[*types.LaneProposal]().Encode(proposal),
		}); err != nil {
			return fmt.Errorf("stream.Send(): %w", err)
		}
	}
}

func (s *server) StreamLaneVotes(
	req *protocol.StreamLaneVotesReq,
	stream grpc.ServerStreamingServer[protocol.LaneVote],
) error {
	ctx := stream.Context()
	next := map[types.LaneID]types.BlockNumber{}
	for {
		var batch []*types.BlockHeader
		for inner, ctrl := range s.state.inner.Lock() {
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
			vote := types.Sign(s.state.key, types.NewLaneVote(h))
			signedVote := types.SignedMsgConv[*types.LaneVote]().Encode(vote)
			if err := stream.Send(&protocol.LaneVote{LaneVote: signedVote}); err != nil {
				return fmt.Errorf("stream.Send(): %w", err)
			}
		}
	}
}

func (s *server) StreamAppVotes(
	req *protocol.StreamAppVotesReq,
	stream grpc.ServerStreamingServer[protocol.AppVote],
) error {
	ctx := stream.Context()
	for idx := types.RoadIndex(0); ; idx = max(idx, s.state.firstCommitQC()) + 1 {
		qc, err := s.state.CommitQC(ctx, idx)
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
			p, err := s.state.Data().AppProposal(ctx, n)
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
			vote := types.Sign(s.state.key, types.NewAppVote(p))
			signedVote := types.SignedMsgConv[*types.AppVote]().Encode(vote)
			if err := stream.Send(&protocol.AppVote{AppVote: signedVote}); err != nil {
				return fmt.Errorf("stream.Send(): %w", err)
			}
		}
	}
}

func (s *server) StreamAppQCs(
	req *protocol.StreamAppQCsReq,
	stream grpc.ServerStreamingServer[protocol.StreamAppQCsResp],
) error {
	ctx := stream.Context()
	next := types.RoadIndex(0)
	for {
		appQC, commitQC, err := s.state.WaitForAppQC(ctx, next)
		if err != nil {
			return fmt.Errorf("s.state.WaitForAppQC(): %w", err)
		}
		next = appQC.Next()
		if err := stream.Send(&protocol.StreamAppQCsResp{
			AppQc:    types.AppQCConv.Encode(appQC),
			CommitQc: types.CommitQCConv.Encode(commitQC),
		}); err != nil {
			return fmt.Errorf("stream.Send(): %w", err)
		}
	}
}

func (s *server) StreamCommitQCs(
	req *protocol.StreamCommitQCsReq,
	stream grpc.ServerStreamingServer[protocol.CommitQC],
) error {
	ctx := stream.Context()
	next := types.RoadIndex(0)
	for {
		qc, err := s.state.CommitQC(ctx, next)
		if err != nil {
			if errors.Is(err, data.ErrPruned) {
				next = s.state.firstCommitQC()
				continue
			}
			return fmt.Errorf("s.state.FirstCommitQC(): %w", err)
		}
		next = qc.Index() + 1
		if err := stream.Send(types.CommitQCConv.Encode(qc)); err != nil {
			return fmt.Errorf("stream.Send(): %w", err)
		}
	}
}
