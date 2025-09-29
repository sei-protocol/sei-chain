package keeper

import (
	"context"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/sei-protocol/sei-chain/x/kinvault/types"
)

type msgServer struct {
	Keeper
}

var _ types.MsgServer = msgServer{}

func NewMsgServerImpl(k Keeper) types.MsgServer {
	return msgServer{Keeper: k}
}

func (m msgServer) WithdrawWithSigil(goCtx context.Context, msg *types.MsgWithdrawWithSigil) (*types.MsgWithdrawWithSigilResponse, error) {
	if err := msg.ValidateBasic(); err != nil {
		return nil, err
	}

	ctx := sdk.UnwrapSDKContext(goCtx)

	if _, err := m.WithdrawWithSigilLegacy(ctx, msg); err != nil {
		return nil, err
	}

	return &types.MsgWithdrawWithSigilResponse{}, nil
}

func (m msgServer) WithdrawWithSigilLegacy(ctx sdk.Context, msg *types.MsgWithdrawWithSigil) (*sdk.Result, error) {
	return m.Keeper.WithdrawWithSigil(ctx, msg)
}
