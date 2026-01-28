package data

import (
	"context"
	"fmt"
	"errors"
	"time"

	"github.com/tendermint/tendermint/libs/utils/scope"
	"github.com/tendermint/tendermint/libs/utils"
	"github.com/tendermint/tendermint/internal/autobahn/pb"
	"github.com/tendermint/tendermint/internal/autobahn/types"
	"github.com/tendermint/tendermint/internal/p2p/rpc"
	"github.com/tendermint/tendermint/internal/p2p/giga"
	"github.com/tendermint/tendermint/internal/p2p"
)

func (s *State) runStreamFullCommitQCs(ctx context.Context, client rpc.Client[giga.API]) error {
	stream, err := giga.StreamFullCommitQCs.Call(ctx, client)
	if err != nil {
		return fmt.Errorf("client.StreamFullCommitQCs(): %w", err)
	}
	if err:=stream.Send(ctx,&pb.StreamFullCommitQCsReq{NextBlock: uint64(s.NextBlock())}); err!=nil {
		return fmt.Errorf("stream.Send(): %w",err)
	}
	for ctx.Err()==nil {
		rawQC, err := stream.Recv(ctx)
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
	return ctx.Err()
}

// MaxConcurrentBlockFetches is the maximum number of blocks that client fetches concurrently.
const MaxConcurrentBlockFetches = 100

// BlockFetchTimeout after which the block fetch RPC is considered failed and needs to be retried.
const BlockFetchTimeout = 2 * time.Second

type req struct {
	n types.GlobalBlockNumber
	done chan struct{}
}

func (s *State) runGetBlock(
	ctx context.Context,
	client rpc.Client[giga.API],
	reqs chan req,
) error {
	return utils.IgnoreCancel(scope.Run(ctx, func(ctx context.Context, scope scope.Scope) error {	
		for ctx.Err()==nil {
			stream, err := giga.GetBlock.Call(ctx, client)
			if err!=nil { return fmt.Errorf("giga.GetBlock.Call(): %w",err) }
			req,err := utils.Recv(ctx,reqs)
			if err!=nil { return err }
			scope.Spawn(func() error {
				defer stream.Close()
				defer close(req.done)
				resp,err := utils.WithTimeout1(ctx, BlockFetchTimeout, func(ctx context.Context) (*pb.GetBlockResp,error) {
					if err:=stream.Send(ctx, &pb.GetBlockReq{GlobalNumber: uint64(req.n)}); err != nil {
						return nil, fmt.Errorf("stream.Send(): %w", err)
					}
					return stream.Recv(ctx)
				})
				if err!=nil {
					return err
				}
				if resp.Block==nil {
					return nil
				}
				b,err := types.BlockConv.Decode(resp.Block)
				if err!=nil {
					return fmt.Errorf("BlockConv.Decode(): %w",err)
				}
				if err := s.PushBlock(ctx, req.n, b); err != nil {
					return fmt.Errorf("s.PushBlock(): %w", err)
				}
				return nil
			})
		}
		return ctx.Err()
	}))
}

func runBlockFetcher(ctx context.Context, state *State, getBlockReqs chan req) error {
	sem := utils.NewSemaphore(MaxConcurrentBlockFetches)
	return scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		for n := state.NextBlock(); ; n += 1 {	
			// Wait for the QC.
			if _, err := state.QC(ctx, n); err != nil { return err }
			release, err := sem.Acquire(ctx)
			if err != nil { return err }
			s.Spawn(func() error {
				defer release()
				for {
					if _, err := state.TryBlock(n); err == nil || errors.Is(err,ErrPruned) {
						return nil	
					}
					req := req{n:n,done:make(chan struct{})}
					if err:=utils.Send(ctx,getBlockReqs,req); err!=nil {
						return err
					}
					<-req.done
				}
			})
		}
	})
}

func (s *State) streamFullCommitQCs(ctx context.Context, stream rpc.Stream[*pb.FullCommitQC,*pb.StreamFullCommitQCsReq]) error {
	req,err := stream.Recv(ctx)
	if err!=nil { return fmt.Errorf("stream.Recv(): %w",err) }
	prev := utils.None[*types.FullCommitQC]()
	for i := types.GlobalBlockNumber(req.NextBlock); ; i++ {
		qc, err := s.QC(ctx, i)
		if err != nil {
			return fmt.Errorf("s.state.QC(): %w", err)
		}
		// Don't send the same QC twice.
		if types.NextIndexOpt(prev) > qc.Index() {
			continue
		}
		prev = utils.Some(qc)
		if err := stream.Send(ctx,types.FullCommitQCConv.Encode(qc)); err != nil {
			return fmt.Errorf("stream.Send(): %w", err)
		}
	} 
}

func (s *State) getBlock(ctx context.Context, stream rpc.Stream[*pb.GetBlockResp, *pb.GetBlockReq]) error {
	req,err := stream.Recv(ctx)
	if err!=nil { return fmt.Errorf("stream.Recv(): %w",err) }
	block, err := s.TryBlock(types.GlobalBlockNumber(req.GlobalNumber))
	resp := &pb.GetBlockResp{}
	if err == nil {
		resp.Block = types.BlockConv.Encode(block) 
	}
	return stream.Send(ctx,resp)
}

func RunServer(ctx context.Context, state *State, router *p2p.GigaRouter) error {
	getBlockReqs := make(chan req)
	return scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		s.Spawn(func() error { return runBlockFetcher(ctx,state,getBlockReqs) })
		s.Spawn(func() error {
			return router.RunServers(ctx, func(ctx context.Context, server rpc.Server[giga.API]) error {
				return scope.Run(ctx, func(ctx context.Context, scope scope.Scope) error {
					s.Spawn(func() error { return giga.StreamFullCommitQCs.Serve(ctx, server, state.streamFullCommitQCs) })
					s.Spawn(func() error { return giga.GetBlock.Serve(ctx, server, state.getBlock) })
					return nil
				})
			})
		})
		s.Spawn(func() error {
			return router.RunClients(ctx, func(ctx context.Context, client rpc.Client[giga.API]) error {
				return scope.Run(ctx, func(ctx context.Context, scope scope.Scope) error {
					s.Spawn(func() error { return state.runStreamFullCommitQCs(ctx,client) })
					s.Spawn(func() error { return state.runGetBlock(ctx,client,getBlockReqs) })
					return nil
				})
			})
		})
		return nil
	})
}
