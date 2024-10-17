package keeper

import (
	"context"
	"fmt"
	"github.com/sei-protocol/sei-chain/x/confidential/types"

	"github.com/tendermint/tendermint/libs/log"

	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"

	paramtypes "github.com/cosmos/cosmos-sdk/x/params/types"
)

// TODO: This is just scaffolding. To be implemented.
type (
	Keeper struct {
		storeKey sdk.StoreKey

		// TODO: Figure out what the paramSpace should look like
		paramSpace paramtypes.Subspace

		// TODO: Add any required keepers here
		// accountKeeper types.AccountKeeper
	}
)

func (k Keeper) TestQuery(ctx context.Context, request *types.TestQueryRequest) (*types.TestQueryResponse, error) {
	//TODO: This is not a real gRPC query. We should remove this and add the real queries once we define query.proto.
	panic("implement me")
}

// NewKeeper returns a new instance of the x/confidential keeper
func NewKeeper(
	_ codec.Codec,
	storeKey sdk.StoreKey,
	paramSpace paramtypes.Subspace,
) Keeper {
	return Keeper{
		storeKey:   storeKey,
		paramSpace: paramSpace,
	}
}

// Logger returns a logger for the x/confidential module
func (k Keeper) Logger(ctx sdk.Context) log.Logger {
	return ctx.Logger().With("module", fmt.Sprintf("x/%s", types.ModuleName))
}
