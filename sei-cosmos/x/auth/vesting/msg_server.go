package vesting

import (
	"context"

	"github.com/sei-protocol/sei-chain/sei-cosmos/x/auth/vesting/types"
)

type msgServer struct{}

// NewMsgServerImpl returns an implementation of the vesting MsgServer interface.
//
// The vesting module is deprecated: every handler rejects its message with
// types.ErrVestingDeprecated, so the server needs no keepers. It is kept
// registered so that deprecated messages fail with a clear error instead of an
// unroutable-message error, while existing vesting accounts in state remain
// fully supported.
func NewMsgServerImpl() types.MsgServer {
	return msgServer{}
}

var _ types.MsgServer = msgServer{}

// CreateVestingAccount is deprecated and always returns
// types.ErrVestingDeprecated. Existing vesting accounts remain in state and
// continue to vest according to their schedules; only the creation of new
// vesting accounts is disabled.
func (s msgServer) CreateVestingAccount(context.Context, *types.MsgCreateVestingAccount) (*types.MsgCreateVestingAccountResponse, error) {
	return nil, types.ErrVestingDeprecated
}
