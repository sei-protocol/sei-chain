package giga

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	apb "github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/pb"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/p2p/giga/pb"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/p2p/rpc"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/scope"
)

func (s *Service) clientStreamFullCommitQCs(ctx context.Context, client rpc.Client[API]) error {
	stream, err := StreamFullCommitQCs.Call(ctx, client)
	if err != nil {
		return fmt.Errorf("client.StreamFullCommitQCs(): %w", err)
	}
	defer stream.Close()
	if err := stream.Send(ctx, StreamFullCommitQCsReqConv.Encode(&StreamFullCommitQCsReq{
		NextBlock: s.data.NextBlock(),
	})); err != nil {
		return fmt.Errorf("stream.Send(): %w", err)
	}
	for ctx.Err() == nil {
		rawQC, err := stream.Recv(ctx)
		if err != nil {
			return fmt.Errorf("stream.Recv(): %w", err)
		}
		qc, err := types.FullCommitQCConv.Decode(rawQC)
		if err != nil {
			return fmt.Errorf("types.CommitQCConv.Decode(): %w", err)
		}
		// TODO: add DoS protection (i.e. that only useful state.Data() has been actually sent).
		if err := s.data.PushQC(ctx, qc, nil); err != nil {
			return fmt.Errorf("s.PushCommitQC(): %w", err)
		}
	}
	return ctx.Err()
}

func (x *Service) clientStreamAppQCs(ctx context.Context, c rpc.Client[API]) error {
	stream, err := StreamAppQCs.Call(ctx, c)
	if err != nil {
		return fmt.Errorf("client.StreamAppQCs(): %w", err)
	}
	defer stream.Close()
	if err := stream.Send(ctx, StreamAppQCsReqConv.Encode(&StreamAppQCsReq{
		NextBlock: x.data.NextAppQC(),
	})); err != nil {
		return err
	}
	for {
		resp, err := stream.Recv(ctx)
		if err != nil {
			return fmt.Errorf("stream.Recv(): %w", err)
		}
		appQC, err := types.AppQCConv.Decode(resp)
		if err != nil {
			return fmt.Errorf("StreamAppQCsRespConv.Decode(): %w", err)
		}
		if err := x.data.PushAppQC(ctx, appQC); err != nil {
			return fmt.Errorf("s.PushFirstCommitQC(): %w", err)
		}
	}
}

// MaxConcurrentBlockFetches is the maximum number of blocks that client fetches concurrently.
const MaxConcurrentBlockFetches = 100

// BlockFetchTimeout after which the block fetch RPC is considered failed and needs to be retried.
const BlockFetchTimeout = 2 * time.Second

// BlockFetchRetryInterval bounds how often runBlockFetcher resends a
// GetBlock for the same height when the chosen peer doesn't have it yet
// (empty Option response). Prevents a tight retry loop when our peer
// happens to be a fullnode that's also catching up.
const BlockFetchRetryInterval = 1 * time.Second

type req struct {
	n    types.GlobalBlockNumber
	done chan struct{}
}

func (s *Service) clientGetBlock(ctx context.Context, client rpc.Client[API]) error {
	return scope.Run(ctx, func(ctx context.Context, scope scope.Scope) error {
		for ctx.Err() == nil {
			stream, err := GetBlock.Call(ctx, client)
			if err != nil {
				return fmt.Errorf("GetBlock.Call(): %w", err)
			}
			req, err := utils.Recv(ctx, s.getBlockReqs)
			if err != nil {
				stream.Close()
				return err
			}
			scope.Spawn(func() error {
				defer stream.Close()
				defer close(req.done)
				resp, err := utils.WithTimeout1(ctx, BlockFetchTimeout, func(ctx context.Context) (*pb.GetBlockResp, error) {
					if err := stream.Send(ctx, GetBlockReqConv.Encode(&GetBlockReq{GlobalNumber: req.n})); err != nil {
						return nil, fmt.Errorf("stream.Send(): %w", err)
					}
					return stream.Recv(ctx)
				})
				if err != nil {
					return err
				}
				block, err := GetBlockRespConv.Decode(resp)
				if err != nil {
					return fmt.Errorf("GetBlockRespConv.Decode(): %w", err)
				}
				b, ok := block.Get()
				if !ok {
					// Peer doesn't have block n yet (e.g. they're a fullnode
					// catching up too). runBlockFetcher's outer loop will
					// retry after BlockFetchRetryInterval.
					return nil
				}
				if err := s.data.PushBlock(ctx, req.n, b); err != nil {
					return fmt.Errorf("s.PushBlock(): %w", err)
				}
				return nil
			})
		}
		return ctx.Err()
	})
}

func (x *Service) runBlockFetcher(ctx context.Context) error {
	sem := utils.NewSemaphore(MaxConcurrentBlockFetches)
	return scope.Run(ctx, func(ctx context.Context, scope scope.Scope) error {
		for n := x.data.NextBlock(); ; n += 1 {
			// Wait for the QC.
			if _, err := x.data.QC(ctx, n); err != nil {
				return err
			}
			release, err := sem.Acquire(ctx)
			if err != nil {
				return err
			}
			scope.Spawn(func() error {
				defer release()
				for first := true; ; first = false {
					// NeedBlock (not TryBlock): gap-fills above nextBlock already
					// satisfy this height. Re-fetching them while a lower hole is
					// empty wastes the shared per-peer GetBlock queue and can
					// starve the contiguous prefix.
					if !x.data.NeedBlock(n) {
						return nil
					}
					// Back off between repeated requests for the same block —
					// avoids hammering a peer that responded with an empty
					// block (doesn't have it yet).
					if !first {
						if err := utils.Sleep(ctx, BlockFetchRetryInterval); err != nil {
							return err
						}
					}
					req := req{n: n, done: make(chan struct{})}
					if err := utils.Send(ctx, x.getBlockReqs, req); err != nil {
						return err
					}
					if _, _, err := utils.RecvOrClosed(ctx, req.done); err != nil {
						return err
					}
				}
			})
		}
	})
}

func (s *Service) serverStreamFullCommitQCs(ctx context.Context, server rpc.Server[API]) error {
	return StreamFullCommitQCs.Serve(ctx, server, func(ctx context.Context, stream rpc.Stream[*apb.FullCommitQC, *pb.StreamFullCommitQCsReq]) error {
		reqRaw, err := stream.Recv(ctx)
		if err != nil {
			return fmt.Errorf("stream.Recv(): %w", err)
		}
		req, err := StreamFullCommitQCsReqConv.Decode(reqRaw)
		if err != nil {
			return fmt.Errorf("StreamFullCommitQCsReqConv.Decode(): %w", err)
		}
		for next := req.NextBlock; ; {
			qc, err := s.data.QC(ctx, next)
			if err != nil {
				if errors.Is(err, types.ErrPruned) {
					next = s.data.First()
				}
				return fmt.Errorf("s.data.QC(): %w", err)
			}
			// Don't send the same QC twice.
			next = qc.QC().GlobalRange().Next
			if err := stream.Send(ctx, types.FullCommitQCConv.Encode(qc)); err != nil {
				return fmt.Errorf("stream.Send(): %w", err)
			}
		}
	})
}

func (s *Service) serverStreamAppQCs(ctx context.Context, server rpc.Server[API]) error {
	return StreamAppQCs.Serve(ctx, server, func(ctx context.Context, stream rpc.Stream[*apb.AppQC, *pb.StreamAppQCsReq]) error {
		reqRaw, err := stream.Recv(ctx)
		if err != nil {
			return err
		}
		req, err := StreamAppQCsReqConv.Decode(reqRaw)
		if err != nil {
			return fmt.Errorf("StreamFullCommitQCsReqConv.Decode(): %w", err)
		}
		for next := req.NextBlock; ; {
			appQC, err := s.data.AppQC(ctx, next)
			if err != nil {
				if errors.Is(err, types.ErrPruned) {
					next = s.data.First()
				}
				return fmt.Errorf("x.validatorState().Data().AppQC(): %w", err)
			}
			next = appQC.Proposal().GlobalRange().Next
			if err := stream.Send(ctx, types.AppQCConv.Encode(appQC)); err != nil {
				return fmt.Errorf("stream.Send(): %w", err)
			}
		}
	})
}

func (x *Service) serverGetBlock(ctx context.Context, server rpc.Server[API]) error {
	return GetBlock.Serve(ctx, server, func(ctx context.Context, stream rpc.Stream[*pb.GetBlockResp, *pb.GetBlockReq]) error {
		reqRaw, err := stream.Recv(ctx)
		if err != nil {
			return fmt.Errorf("stream.Recv(): %w", err)
		}
		req, err := GetBlockReqConv.Decode(reqRaw)
		if err != nil {
			return fmt.Errorf("GetBlockReqConv.Decode(): %w", err)
		}
		block, err := x.data.TryBlock(req.GlobalNumber)
		if err != nil {
			// Absent (not yet / pruned) is a successful empty response so the
			// peer can retry. Any other error is a store failure — do not
			// disguise it as "not available".
			if errors.Is(err, types.ErrPruned) || errors.Is(err, types.ErrNotFound) {
				return stream.Send(ctx, GetBlockRespConv.Encode(utils.None[*types.Block]()))
			}
			return fmt.Errorf("TryBlock(%d): %w", req.GlobalNumber, err)
		}
		return stream.Send(ctx, GetBlockRespConv.Encode(utils.Some(block)))
	})
}
