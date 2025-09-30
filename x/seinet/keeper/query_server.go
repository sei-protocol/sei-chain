package keeper

import (
	"context"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/sei-protocol/sei-chain/x/seinet/types"
)

var _ types.QueryServer = Keeper{}

func (k Keeper) VaultBalance(goCtx context.Context, req *types.QueryVaultBalanceRequest) (*types.QueryVaultBalanceResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "empty request")
	}

	ctx := sdk.UnwrapSDKContext(goCtx)

	moduleAddr := k.accountKeeper.GetModuleAddress(types.SeinetVaultAccount)
	if moduleAddr == nil {
		return nil, status.Errorf(codes.Internal, "module account %s not configured", types.SeinetVaultAccount)
	}

	balance := k.bankKeeper.GetAllBalances(ctx, moduleAddr)

	return &types.QueryVaultBalanceResponse{Balance: balance.String()}, nil
}
