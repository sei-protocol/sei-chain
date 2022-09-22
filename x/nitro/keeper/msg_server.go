package keeper

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/sei-protocol/sei-chain/x/nitro/types"
)

type msgServer struct {
	Keeper
}

func NewMsgServerImpl(keeper Keeper) types.MsgServer {
	return &msgServer{Keeper: keeper}
}

var _ types.MsgServer = msgServer{}

func (server msgServer) RecordTransactionData(goCtx context.Context, msg *types.MsgRecordTransactionData) (*types.MsgRecordTransactionDataResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)

	if !server.IsTxSenderWhitelisted(ctx, msg.Sender) {
		return nil, errors.New("sender account is not whitelisted to send nitro transaction data")
	}
	if existingSender, exists := server.GetSender(ctx, msg.Slot); exists {
		return nil, fmt.Errorf("slot %d has already been recorded by %s", msg.Slot, existingSender)
	}

	txsBz := [][]byte{}
	for _, tx := range msg.Txs {
		txBz, err := hex.DecodeString(tx)
		if err != nil {
			return nil, err
		}
		txsBz = append(txsBz, txBz)
	}
	server.SetTransactionData(ctx, msg.Slot, txsBz)
	stateRootBz, err := hex.DecodeString(msg.StateRoot)
	if err != nil {
		return nil, err
	}
	server.SetStateRoot(ctx, msg.Slot, stateRootBz)
	server.SetSender(ctx, msg.Slot, msg.Sender)

	return &types.MsgRecordTransactionDataResponse{}, nil
}
