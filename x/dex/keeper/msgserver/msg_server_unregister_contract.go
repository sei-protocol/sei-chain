package msgserver

import (
	"context"

	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/sei-protocol/sei-chain/x/dex/types"
)

func (k msgServer) UnregisterContract(goCtx context.Context, msg *types.MsgUnregisterContract) (*types.MsgUnregisterContractResponse, error) {
	return nil, sdkerrors.Wrapf(sdkerrors.ErrNotSupported, "deprecated")
}
