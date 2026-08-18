package keeper

import (
	"context"

	"github.com/sei-protocol/sei-chain/sei-ibc-go/modules/apps/transfer/types"
)

var _ types.QueryServer = Keeper{}

// DenomTrace implements the Query/DenomTrace gRPC method.
func (Keeper) DenomTrace(context.Context, *types.QueryDenomTraceRequest) (*types.QueryDenomTraceResponse, error) {
	return nil, types.ErrTransferDeprecated
}

// DenomTraces implements the Query/DenomTraces gRPC method.
func (Keeper) DenomTraces(context.Context, *types.QueryDenomTracesRequest) (*types.QueryDenomTracesResponse, error) {
	return nil, types.ErrTransferDeprecated
}

// Params implements the Query/Params gRPC method.
func (Keeper) Params(context.Context, *types.QueryParamsRequest) (*types.QueryParamsResponse, error) {
	return nil, types.ErrTransferDeprecated
}

// DenomHash implements the Query/DenomHash gRPC method.
func (Keeper) DenomHash(context.Context, *types.QueryDenomHashRequest) (*types.QueryDenomHashResponse, error) {
	return nil, types.ErrTransferDeprecated
}

// EscrowAddress implements the EscrowAddress gRPC method.
func (Keeper) EscrowAddress(context.Context, *types.QueryEscrowAddressRequest) (*types.QueryEscrowAddressResponse, error) {
	return nil, types.ErrTransferDeprecated
}
