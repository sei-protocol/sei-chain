package query

import (
	"context"
	"fmt"
	"math/big"
	"strconv"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	gogogrpc "github.com/gogo/protobuf/grpc"
	grpctypes "github.com/sei-protocol/sei-chain/sei-cosmos/types/grpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

var _ gogogrpc.ClientConn = (*Conn)(nil)

// EVMCaller is the ethclient.Client subset needed to execute precompile queries.
type EVMCaller interface {
	CallContract(ctx context.Context, msg ethereum.CallMsg, blockNumber *big.Int) ([]byte, error)
}

type Conn struct {
	caller             EVMCaller
	registry           Registry
	defaultBlockNumber *big.Int
	defaultFrom        common.Address
}

type Option func(*Conn)

func WithDefaultBlockNumber(height int64) Option {
	return func(c *Conn) {
		if height > 0 {
			c.defaultBlockNumber = big.NewInt(height)
		}
	}
}

func WithDefaultFrom(addr common.Address) Option {
	return func(c *Conn) {
		c.defaultFrom = addr
	}
}

func NewConn(caller EVMCaller, registry Registry, opts ...Option) *Conn {
	c := &Conn{
		caller:   caller,
		registry: registry,
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

func (c *Conn) Invoke(ctx context.Context, method string, req, reply interface{}, opts ...grpc.CallOption) error {
	if req == nil {
		return status.Error(codes.InvalidArgument, "request cannot be nil")
	}
	if reply == nil {
		return status.Error(codes.InvalidArgument, "reply cannot be nil")
	}
	if c.caller == nil {
		return status.Error(codes.FailedPrecondition, "EVM caller is not configured")
	}
	binding, ok := c.registry[method]
	if !ok {
		return status.Errorf(codes.Unimplemented, "precompile query binding for %s is not implemented", method)
	}

	env := &Env{
		caller:      c.caller,
		blockNumber: c.blockNumber(ctx),
		defaultFrom: c.defaultFrom,
	}
	return binding.Invoke(ctx, env, req, reply)
}

func (c *Conn) NewStream(context.Context, *grpc.StreamDesc, string, ...grpc.CallOption) (grpc.ClientStream, error) {
	return nil, status.Error(codes.Unimplemented, "streaming precompile queries are not supported")
}

func (c *Conn) blockNumber(ctx context.Context) *big.Int {
	if md, ok := metadata.FromOutgoingContext(ctx); ok {
		heights := md.Get(grpctypes.GRPCBlockHeightHeader)
		if len(heights) > 0 {
			height, err := strconv.ParseInt(heights[0], 10, 64)
			if err == nil && height > 0 {
				return big.NewInt(height)
			}
		}
	}
	if c.defaultBlockNumber == nil {
		return nil
	}
	return new(big.Int).Set(c.defaultBlockNumber)
}

type Env struct {
	caller      EVMCaller
	blockNumber *big.Int
	defaultFrom common.Address
}

func (e *Env) BlockNumber() *big.Int {
	if e.blockNumber == nil {
		return nil
	}
	return new(big.Int).Set(e.blockNumber)
}

func (e *Env) EthCall(ctx context.Context, to common.Address, input []byte, opts ...CallOption) ([]byte, error) {
	call := callOptions{from: e.defaultFrom}
	for _, opt := range opts {
		opt(&call)
	}
	msg := ethereum.CallMsg{
		From: call.from,
		To:   &to,
		Data: input,
	}
	return e.caller.CallContract(ctx, msg, e.BlockNumber())
}

type callOptions struct {
	from common.Address
}

type CallOption func(*callOptions)

func WithFrom(from common.Address) CallOption {
	return func(o *callOptions) {
		o.from = from
	}
}

func typeMismatch(expected interface{}, got interface{}) error {
	return fmt.Errorf("expected %T, got %T", expected, got)
}
