package avail

import (
	"context"
	"fmt"

	"github.com/tendermint/tendermint/internal/autobahn/pb"
	"github.com/tendermint/tendermint/internal/autobahn/types"
	"github.com/tendermint/tendermint/libs/utils"
	"github.com/tendermint/tendermint/libs/utils/scope"
)

// Client is a StreamAPIClient wrapper capable of sending consensus state updates.
type client struct {
	pb.AvailAPIClient
	cfg   *config.PeerConfig
	state *State
}

// RunClient runs an RPC client which actively pulls the peer's availability state.
// If peer is a lane producer, then it also fetches the blocks producer by the peer.
func (s *State) RunClient(ctx context.Context, cfg *config.PeerConfig) error {
	return utils.IgnoreCancel(scope.Run(ctx, func(ctx context.Context, scope scope.Scope) error {
		conn, err := grpcutils.NewClient(cfg.Address)
		if err != nil {
			return fmt.Errorf("grpc.NewClient(%q): %w", cfg.Address, err)
		}
		c := &client{
			AvailAPIClient: pb.NewAvailAPIClient(conn),
			cfg:            cfg,
			state:          s,
		}
		scope.SpawnNamed("runStreamLaneProposals", func() error {
			return c.runStreamLaneProposals(ctx)
		})
		scope.SpawnNamed("runStreamLaneVotes", func() error {
			return c.runStreamLaneVotes(ctx)
		})
		scope.SpawnNamed("runStreamCommitQCs", func() error {
			return c.runStreamCommitQCs(ctx)
		})
		scope.SpawnNamed("runStreamAppVotes", func() error {
			return c.runStreamAppVotes(ctx)
		})
		scope.SpawnNamed("runStreamAppQCs", func() error {
			return c.runStreamAppQCs(ctx)
		})
		return nil
	}))
}

func (c *client) runStreamLaneProposals(ctx context.Context) error {
	return c.cfg.Retry(ctx, "StreamLaneProposals", func(ctx context.Context) error {
		stream, err := c.StreamLaneProposals(ctx, &pb.StreamLaneProposalsReq{
			FirstBlockNumber: uint64(c.state.NextBlock(c.cfg.GetKey())),
		})
		if err != nil {
			return fmt.Errorf("client.StreamLaneProposals(): %w", err)
		}
		for {
			rawProposal, err := stream.Recv()
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
			if err := c.state.PushBlock(ctx, proposal); err != nil {
				return fmt.Errorf("s.PushLaneProposal(): %w", err)
			}
		}
	})
}

func (c *client) runStreamLaneVotes(ctx context.Context) error {
	return c.cfg.Retry(ctx, "StreamLaneVotes", func(ctx context.Context) error {
		stream, err := c.StreamLaneVotes(ctx, &pb.StreamLaneVotesReq{})
		if err != nil {
			return fmt.Errorf("client.StreamLaneVotes(): %w", err)
		}
		for {
			rawVote, err := stream.Recv()
			if err != nil {
				return fmt.Errorf("stream.Recv(): %w", err)
			}
			vote, err := types.SignedMsgConv[*types.LaneVote]().Decode(rawVote.LaneVote)
			if err != nil {
				return fmt.Errorf("types.LaneVoteConv.Decode(): %w", err)
			}
			if err := c.state.PushVote(ctx, vote); err != nil {
				return fmt.Errorf("s.PushLaneVote(): %w", err)
			}
		}
	})
}

func (c *client) runStreamCommitQCs(ctx context.Context) error {
	return c.cfg.Retry(ctx, "StreamCommitQCs", func(ctx context.Context) error {
		stream, err := c.StreamCommitQCs(ctx, &pb.StreamCommitQCsReq{})
		if err != nil {
			return fmt.Errorf("client.StreamCommitQCs(): %w", err)
		}
		for {
			resp, err := stream.Recv()
			if err != nil {
				return fmt.Errorf("stream.Recv(): %w", err)
			}
			qc, err := types.CommitQCConv.Decode(resp)
			if err != nil {
				return fmt.Errorf("types.CommitQCConv.Decode(): %w", err)
			}
			if err := c.state.PushCommitQC(ctx, qc); err != nil {
				return fmt.Errorf("s.PushFirstCommitQC(): %w", err)
			}
		}
	})
}

func (c *client) runStreamAppVotes(ctx context.Context) error {
	return c.cfg.Retry(ctx, "StreamAppVotes", func(ctx context.Context) error {
		ctx, cancel := context.WithCancel(ctx)
		defer cancel()
		stream, err := c.StreamAppVotes(ctx, &pb.StreamAppVotesReq{})
		if err != nil {
			return fmt.Errorf("client.StreamAppVotes(): %w", err)
		}
		for {
			rawVote, err := stream.Recv()
			if err != nil {
				return fmt.Errorf("stream.Recv(): %w", err)
			}
			vote, err := types.SignedMsgConv[*types.AppVote]().Decode(rawVote.AppVote)
			if err != nil {
				return fmt.Errorf("types.LaneVoteConv.Decode(): %w", err)
			}
			if err := c.state.PushAppVote(ctx, vote); err != nil {
				return fmt.Errorf("s.PushLaneVote(): %w", err)
			}
		}
	})
}

func (c *client) runStreamAppQCs(ctx context.Context) error {
	return c.cfg.Retry(ctx, "StreamAppQCs", func(ctx context.Context) error {
		stream, err := c.StreamAppQCs(ctx, &pb.StreamAppQCsReq{})
		if err != nil {
			return fmt.Errorf("client.StreamAppQCs(): %w", err)
		}
		for {
			resp, err := stream.Recv()
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
			if err := c.state.PushAppQC(appQC, commitQC); err != nil {
				return fmt.Errorf("s.PushFirstCommitQC(): %w", err)
			}
		}
	})
}
