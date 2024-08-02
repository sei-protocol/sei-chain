package query

import (
	"context"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/x/dex/types"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (k KeeperWrapper) GetMatchResult(c context.Context, req *types.QueryGetMatchResultRequest) (*types.QueryGetMatchResultResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}
	ctx := sdk.UnwrapSDKContext(c)

	result, found := k.GetMatchResultState(ctx, req.ContractAddr)
	if !found {
		return nil, status.Error(codes.NotFound, "result not found")
	}

	return &types.QueryGetMatchResultResponse{Result: result}, nil
}
