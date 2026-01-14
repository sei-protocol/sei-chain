package data

import (
	"context"
	"fmt"
	"math/rand"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/sei-protocol/sei-stream/config"
	"github.com/sei-protocol/sei-stream/pkg/grpcutils"
	"github.com/sei-protocol/sei-stream/pkg/service"
	"github.com/sei-protocol/sei-stream/pkg/utils"
	"github.com/sei-protocol/sei-stream/stream/pkg/protocol"
	"github.com/sei-protocol/sei-stream/stream/types"
)

type client struct {
	protocol.DataAPIClient
	cfg *config.PeerConfig
}

func (s *State) runStreamFullCommitQCs(ctx context.Context, c *client) error {
	return c.cfg.Retry(ctx, "StreamFullCommitQCs", func(ctx context.Context) error {
		stream, err := c.StreamFullCommitQCs(ctx, &protocol.StreamFullCommitQCsReq{
			NextBlock: uint64(s.NextBlock()),
		})
		if err != nil {
			return fmt.Errorf("client.StreamFullCommitQCs(): %w", err)
		}
		for {
			rawQC, err := stream.Recv()
			if err != nil {
				return fmt.Errorf("stream.Recv(): %w", err)
			}
			qc, err := types.FullCommitQCConv.Decode(rawQC)
			if err != nil {
				return fmt.Errorf("types.CommitQCConv.Decode(): %w", err)
			}
			// TODO: add DoS protection (i.e. that only useful data has been actually sent).
			if err := s.PushQC(ctx, qc, nil); err != nil {
				return fmt.Errorf("s.PushCommitQC(): %w", err)
			}
		}
	})
}

// MaxConcurrentBlockFetches is the maximum number of blocks that client fetches concurrently.
const MaxConcurrentBlockFetches = 100

// BlockFetchTimeout after which the block fetch RPC is considered failed and needs to be retried.
const BlockFetchTimeout = 2 * time.Second

func (s *State) tryFetchBlock(
	ctx context.Context,
	client protocol.DataAPIClient,
	n types.GlobalBlockNumber,
) error {
	ctx, cancel := context.WithTimeout(ctx, BlockFetchTimeout)
	defer cancel()
	rawBlock, err := client.GetBlock(ctx, &protocol.GetBlockReq{
		GlobalNumber: uint64(n),
	})
	if err != nil {
		return fmt.Errorf("client.GetBlock(): %w", err)
	}
	b, err := types.BlockConv.Decode(rawBlock)
	if err != nil {
		return fmt.Errorf("types.BlockConv.Decode(): %w", err)
	}
	if err := s.PushBlock(ctx, n, b); err != nil {
		return fmt.Errorf("s.PushBlock(): %w", err)
	}
	return nil
}

func (s *State) fetchBlock(
	ctx context.Context,
	clients []*client,
	n types.GlobalBlockNumber,
) error {
	// Wait for the QC.
	if _, err := s.QC(ctx, n); err != nil {
		return err
	}
	// Early exit if the block is already available.
	if _, err := s.TryBlock(n); err == nil {
		return nil
	}
	return utils.IgnoreCancel(service.Run(ctx, func(ctx context.Context, scope service.Scope) error {
		// Try to fetch the block in the background until success.
		scope.SpawnBg(func() error {
			for ctx.Err() == nil {
				// TODO(gprusak): here we try to fetch the block from a random peer - instead we should
				// deduce which peer has the block, by looking at the LaneQCs that we received from them.
				// TODO(gprusak): we need to set rate limiting for both client and server side.
				i := rand.Intn(len(clients))
				if err := s.tryFetchBlock(ctx, clients[i], n); err != nil {
					// Skip logging Canceled error - it is expected to happen often, but the logs confuse people.
					if !grpcutils.IsCanceled(err) {
						log.Debug().Err(err).Int("client", i).Uint64("n", uint64(n)).Msg("tryFetchBlock()")
					}
					continue
				}
				return nil
			}
			return ctx.Err()
		})
		// Wait until the block is available.
		_, err := s.Block(ctx, n)
		return err
	}))
}

// RunClientPool runs a pool of RPC clients which actively fetch any missing blocks
// from the peers.
func (s *State) RunClientPool(ctx context.Context, peerCfgs []*config.PeerConfig) error {
	if len(peerCfgs) == 0 {
		return nil
	}
	return utils.IgnoreCancel(service.Run(ctx, func(ctx context.Context, scope service.Scope) error {
		var clients []*client
		for _, cfg := range peerCfgs {
			conn, err := grpcutils.NewClient(cfg.Address)
			if err != nil {
				return fmt.Errorf("grpc.NewClient(%q): %w", cfg.Address, err)
			}
			c := &client{
				DataAPIClient: protocol.NewDataAPIClient(conn),
				cfg:           cfg,
			}
			scope.SpawnNamed("runStreamCommitQCs", func() error {
				return s.runStreamFullCommitQCs(ctx, c)
			})
			clients = append(clients, c)
		}
		// Block fetching
		scope.Spawn(func() error {
			sem := utils.NewSemaphore(MaxConcurrentBlockFetches)
			for n := s.NextBlock(); ; n += 1 {
				release, err := sem.Acquire(ctx)
				if err != nil {
					return err
				}
				scope.Spawn(func() error {
					defer release()
					return s.fetchBlock(ctx, clients, n)
				})
			}
		})
		return nil
	}))
}
