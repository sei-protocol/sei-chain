package keeper

import (
	"context"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/x/seinet/types"
)

type queryServer struct {
	Keeper
}

// NewQueryServerImpl returns implementation of QueryServer.
func NewQueryServerImpl(k Keeper) types.QueryServer {
	return &queryServer{Keeper: k}
}

// Covenant returns final covenant.
func (q queryServer) Covenant(goCtx context.Context, _ *types.QueryCovenantRequest) (*types.QueryCovenantResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)
	store := ctx.KVStore(q.storeKey)
	bz := store.Get([]byte("final_covenant"))
	return &types.QueryCovenantResponse{Covenant: string(bz)}, nil
}
