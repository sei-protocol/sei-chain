package epoch

import (
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	seitypes "github.com/sei-protocol/sei-chain/types"
	"github.com/sei-protocol/sei-chain/x/epoch/keeper"
	"github.com/sei-protocol/sei-chain/x/epoch/types"
)

func NewHandler(_ keeper.Keeper) sdk.Handler {
	return func(ctx sdk.Context, msg seitypes.Msg) (*sdk.Result, error) {
		_ = ctx.WithEventManager(sdk.NewEventManager())
		errMsg := fmt.Sprintf("unrecognized %s message type: %T", types.ModuleName, msg)
		return nil, sdkerrors.Wrap(sdkerrors.ErrUnknownRequest, errMsg)
	}
}
