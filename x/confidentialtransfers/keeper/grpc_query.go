package keeper

import (
	"context"

	"github.com/cosmos/cosmos-sdk/types/query"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/x/confidentialtransfers/types"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

var _ types.QueryServer = BaseKeeper{}

func (k BaseKeeper) GetAccount(ctx context.Context, req *types.GetAccountRequest) (*types.GetAccountResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "empty request")
	}

	if req.Address == "" {
		return nil, status.Error(codes.InvalidArgument, "address cannot be empty")
	}

	address, err := sdk.AccAddressFromBech32(req.Address)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid address: %s", err.Error())
	}

	if req.Denom == "" {
		return nil, status.Error(codes.InvalidArgument, "invalid denom")
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)

	ctAccount, found := k.getCtAccount(sdkCtx, address, req.Denom)
	if !found {
		return nil, status.Errorf(codes.NotFound, "account not found for account %s and denom %s",
			req.Address, req.Denom)
	}

	return &types.GetAccountResponse{Account: &ctAccount}, nil
}

func (k BaseKeeper) GetAllAccounts(ctx context.Context, req *types.GetAllAccountsRequest) (*types.GetAllAccountsResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "empty request")
	}

	if req.Address == "" {
		return nil, status.Error(codes.InvalidArgument, "address cannot be empty")
	}

	address, err := sdk.AccAddressFromBech32(req.Address)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid address: %s", err.Error())
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)

	store := k.getAccountStoreForAddress(sdkCtx, address)
	accounts := make([]types.CtAccountWithDenom, 0)
	pageRes, err := query.Paginate(store, req.Pagination, func(denom, value []byte) error {

		var ctAccount types.CtAccount
		err = k.cdc.Unmarshal(value, &ctAccount)
		if err != nil {
			return err
		}
		accounts = append(accounts, types.CtAccountWithDenom{Denom: string(denom), Account: ctAccount})
		return nil
	})

	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "paginate: %v", err)
	}

	return &types.GetAllAccountsResponse{Accounts: accounts, Pagination: pageRes}, nil

}
