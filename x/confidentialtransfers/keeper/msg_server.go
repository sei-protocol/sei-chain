package keeper

import (
	"context"
	"github.com/sei-protocol/sei-chain/x/confidentialtransfers/types"
)

type msgServer struct {
	Keeper
}

func (m msgServer) Transfer(ctx context.Context, transfer *types.MsgTransfer) (*types.MsgTransferResponse, error) {
	//TODO implement me
	panic("implement me")
}

func (m msgServer) InitializeAccount(ctx context.Context, account *types.MsgInitializeAccount) (*types.MsgInitializeAccountResponse, error) {
	//TODO implement me
	panic("implement me")
}

func (m msgServer) Deposit(ctx context.Context, deposit *types.MsgDeposit) (*types.MsgDepositResponse, error) {
	//TODO implement me
	panic("implement me")
}

func (m msgServer) Withdraw(ctx context.Context, withdraw *types.MsgWithdraw) (*types.MsgWithdrawResponse, error) {
	//TODO implement me
	panic("implement me")
}

func (m msgServer) ApplyPendingBalance(ctx context.Context, balance *types.MsgApplyPendingBalance) (*types.MsgApplyPendingBalanceResponse, error) {
	//TODO implement me
	panic("implement me")
}

func (m msgServer) CloseAccount(ctx context.Context, account *types.MsgCloseAccount) (*types.MsgCloseAccountResponse, error) {
	//TODO implement me
	panic("implement me")
}

// NewMsgServerImpl returns an implementation of the MsgServer interface
// for the provided Keeper.
func NewMsgServerImpl(keeper Keeper) types.MsgServer {
	return msgServer{keeper}
}

var _ types.MsgServer = msgServer{}
