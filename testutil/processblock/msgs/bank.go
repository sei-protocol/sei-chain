package msgs

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	seitypes "github.com/sei-protocol/sei-chain/types"
)

func Send(from seitypes.AccAddress, to seitypes.AccAddress, amount int64) *banktypes.MsgSend {
	return &banktypes.MsgSend{
		FromAddress: from.String(),
		ToAddress:   to.String(),
		Amount:      sdk.NewCoins(sdk.NewCoin("usei", sdk.NewInt(amount))),
	}
}
