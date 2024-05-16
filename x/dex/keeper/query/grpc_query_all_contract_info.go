package query

import (
	"context"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/x/dex/types"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (k KeeperWrapper) ListAllContractInfo(c context.Context, req *types.QueryListAllContractInfoRequest) (*types.QueryListAllContractInfoResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}
	ctx := sdk.UnwrapSDKContext(c)

	infos := k.GetAllContractInfo(ctx)
	contractInfos := make([]*types.ContractInfoV2, len(infos))

	for i := range infos {
		contractInfos[i] = &infos[i]
	}

	return &types.QueryListAllContractInfoResponse{ContractInfos: contractInfos}, nil
}
