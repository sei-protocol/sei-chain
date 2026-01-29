package giga

import (
	"context"
	"fmt"
	"errors"
	"time"

	"github.com/tendermint/tendermint/libs/utils/scope"
	"github.com/tendermint/tendermint/libs/utils"
	"github.com/tendermint/tendermint/internal/autobahn/data"
	"github.com/tendermint/tendermint/internal/autobahn/types"
	"github.com/tendermint/tendermint/internal/p2p/rpc"
	"github.com/tendermint/tendermint/internal/p2p/giga/pb"
	apb "github.com/tendermint/tendermint/internal/autobahn/pb"
)

func (s *Service) clientStreamFullCommitQCs(ctx context.Context, client rpc.Client[API]) error {
	stream, err := StreamFullCommitQCs.Call(ctx, client)
	if err != nil {
		return fmt.Errorf("client.StreamFullCommitQCs(): %w", err)
	}
	defer stream.Close()
	if err:=stream.Send(ctx,&pb.StreamFullCommitQCsReq{NextBlock: uint64(s.data.NextBlock())}); err!=nil {
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
		if err := s.data.PushQC(ctx, qc, nil); err != nil {
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

func (s *Service) clientGetBlock(ctx context.Context, client rpc.Client[API]) error {
	return scope.Run(ctx, func(ctx context.Context, scope scope.Scope) error {	
		for ctx.Err()==nil {
			stream, err := GetBlock.Call(ctx, client)
			if err!=nil { return fmt.Errorf("GetBlock.Call(): %w",err) }
			req,err := utils.Recv(ctx,s.getBlockReqs)
			if err!=nil {
				stream.Close()
				return err
			}
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
			if _, err := x.data.QC(ctx, n); err != nil { return err }
			release, err := sem.Acquire(ctx)
			if err != nil { return err }
			scope.Spawn(func() error {
				defer release()
				for {
					if _, err := x.data.TryBlock(n); !errors.Is(err,data.ErrNotFound) {
						return nil	
					}
					req := req{n:n,done:make(chan struct{})}
					if err:=utils.Send(ctx,x.getBlockReqs,req); err!=nil {
						return err
					}
					<-req.done
				}
			})
		}
	})
}

func (s *Service) serverStreamFullCommitQCs(ctx context.Context, server rpc.Server[API]) error {
	return StreamFullCommitQCs.Serve(ctx, server, func(ctx context.Context, stream rpc.Stream[*apb.FullCommitQC,*pb.StreamFullCommitQCsReq]) error {
		req,err := stream.Recv(ctx)
		if err!=nil { return fmt.Errorf("stream.Recv(): %w",err) }
		prev := utils.None[*types.FullCommitQC]()
		for i := types.GlobalBlockNumber(req.NextBlock); ; i++ {
			qc, err := s.data.QC(ctx, i)
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
	})
}

func (x *Service) serverGetBlock(ctx context.Context, server rpc.Server[API]) error {
	return GetBlock.Serve(ctx, server, func(ctx context.Context, stream rpc.Stream[*pb.GetBlockResp, *pb.GetBlockReq]) error { 
		req,err := stream.Recv(ctx)
		if err!=nil { return fmt.Errorf("stream.Recv(): %w",err) }
		block, err := x.data.TryBlock(types.GlobalBlockNumber(req.GlobalNumber))
		resp := &pb.GetBlockResp{}
		if err == nil {
			resp.Block = types.BlockConv.Encode(block) 
		}
		return stream.Send(ctx,resp)
	})
}
