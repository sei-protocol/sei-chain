package keeper

import (
	"context"

	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/sei-protocol/sei-chain/x/seinet/types"
)

var _ types.QueryServer = queryServer{}

// queryServer provides the gRPC query service implementation for the seinet module.
type queryServer struct {
	keeper Keeper
}

// NewQueryServer constructs a new QueryServer implementation backed by the provided keeper.
func NewQueryServer(k Keeper) types.QueryServer {
	return queryServer{keeper: k}
}

// VaultBalance returns the balances held by the seinet vault module account or a custom address if requested.
func (s queryServer) VaultBalance(goCtx context.Context, req *types.QueryVaultBalanceRequest) (*types.QueryVaultBalanceResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	ctx := sdk.UnwrapSDKContext(goCtx)

	targetAddr, err := resolveBalanceAddress(req.Address, types.SeinetVaultAccount)
	if err != nil {
		return nil, err
	}

	balances := s.keeper.bankKeeper.GetAllBalances(ctx, targetAddr)

	return &types.QueryVaultBalanceResponse{Balances: coinsToQueryBalances(balances)}, nil
}

// CovenantBalance returns the balances held by the seinet covenant (royalty) module account or a custom address if requested.
func (s queryServer) CovenantBalance(goCtx context.Context, req *types.QueryCovenantBalanceRequest) (*types.QueryCovenantBalanceResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	ctx := sdk.UnwrapSDKContext(goCtx)

	targetAddr, err := resolveBalanceAddress(req.Address, types.SeinetRoyaltyAccount)
	if err != nil {
		return nil, err
	}

	balances := s.keeper.bankKeeper.GetAllBalances(ctx, targetAddr)

	return &types.QueryCovenantBalanceResponse{Balances: coinsToQueryBalances(balances)}, nil
}

// resolveBalanceAddress resolves to the requested bech32 address if provided,
// otherwise returns the default module account address.
func resolveBalanceAddress(requestedAddress string, moduleAccount string) (sdk.AccAddress, error) {
	if requestedAddress != "" {
		return sdk.AccAddressFromBech32(requestedAddress)
	}
	return authtypes.NewModuleAddress(moduleAccount), nil
}

// coinsToQueryBalances converts sdk.Coins into []*types.QueryBalance.
func coinsToQueryBalances(coins sdk.Coins) []*types.QueryBalance {
	balances := make([]*types.QueryBalance, 0, len(coins))
	for _, coin := range coins {
		balances = append(balances, &types.QueryBalance{
			Denom:  coin.Denom,
			Amount: coin.Amount.String(),
		})
	}
	return balances
}
