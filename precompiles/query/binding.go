package query

import (
	"context"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type ResponseShape uint8

const (
	ExactProtobufShape ResponseShape = iota
	DocumentedVariation
)

type Invoker interface {
	Method() string
	Invoke(ctx context.Context, env *Env, req, reply interface{}) error
}

type Registry map[string]Invoker

func NewRegistry(bindings ...Invoker) Registry {
	registry := make(Registry, len(bindings))
	for _, binding := range bindings {
		registry[binding.Method()] = binding
	}
	return registry
}

type Binding[Req any, Resp any] struct {
	FullMethod    string
	Precompile    common.Address
	ABI           abi.ABI
	ABIMethod     string
	Pack          func(context.Context, *Env, *Req) ([]interface{}, error)
	Unpack        func(context.Context, *Env, *Req, []interface{}, *Resp) error
	ABIForHeight  func(height int64) abi.ABI
	ResponseShape ResponseShape
	Variation     string
}

func Bind[Req any, Resp any](binding Binding[Req, Resp]) Invoker {
	return binding
}

func (b Binding[Req, Resp]) Method() string {
	return b.FullMethod
}

func (b Binding[Req, Resp]) Invoke(ctx context.Context, env *Env, req, reply interface{}) error {
	typedReq, ok := req.(*Req)
	if !ok {
		return status.Error(codes.InvalidArgument, typeMismatch((*Req)(nil), req).Error())
	}
	if typedReq == nil {
		return status.Error(codes.InvalidArgument, "request cannot be nil")
	}
	typedReply, ok := reply.(*Resp)
	if !ok {
		return status.Error(codes.InvalidArgument, typeMismatch((*Resp)(nil), reply).Error())
	}
	if typedReply == nil {
		return status.Error(codes.InvalidArgument, "reply cannot be nil")
	}
	if b.Pack == nil || b.Unpack == nil {
		return status.Errorf(codes.FailedPrecondition, "precompile query binding for %s is incomplete", b.FullMethod)
	}

	contractABI := b.ABI
	if b.ABIForHeight != nil {
		height := int64(0)
		if blockNumber := env.BlockNumber(); blockNumber != nil {
			if !blockNumber.IsInt64() {
				return status.Errorf(codes.InvalidArgument, "block height %s overflows int64", blockNumber.String())
			}
			height = blockNumber.Int64()
		}
		contractABI = b.ABIForHeight(height)
	}

	args, err := b.Pack(ctx, env, typedReq)
	if err != nil {
		return err
	}
	input, err := contractABI.Pack(b.ABIMethod, args...)
	if err != nil {
		return err
	}
	output, err := env.EthCall(ctx, b.Precompile, input)
	if err != nil {
		return err
	}
	values, err := contractABI.Unpack(b.ABIMethod, output)
	if err != nil {
		return err
	}
	return b.Unpack(ctx, env, typedReq, values, typedReply)
}
