package keeper

import (
	"context"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/x/seinet/types"
)

type msgServer struct {
	Keeper
}

// NewMsgServerImpl returns implementation of the MsgServer interface.
func NewMsgServerImpl(k Keeper) types.MsgServer {
	return &msgServer{Keeper: k}
}

// CommitCovenant handles MsgCommitCovenant.
func (m msgServer) CommitCovenant(goCtx context.Context, msg *types.MsgCommitCovenant) (*types.MsgCommitCovenantResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)

	// Commit covenant with validations and royalty enforcement
	if err := m.SeiNetCommitCovenantSync(ctx, msg.Creator, msg.Covenant); err != nil {
		return nil, err
	}

	return &types.MsgCommitCovenantResponse{}, nil
}

// UnlockHardwareKey handles MsgUnlockHardwareKey.
func (m msgServer) UnlockHardwareKey(goCtx context.Context, msg *types.MsgUnlockHardwareKey) (*types.MsgUnlockHardwareKeyResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)

	m.SeiNetSetHardwareKeyApproval(ctx, msg.Creator)

	return &types.MsgUnlockHardwareKeyResponse{}, nil
}