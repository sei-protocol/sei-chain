package wasm

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/x/dex/types"
)

type SudoSettlementMsg struct {
	Settlement types.Settlements `json:"settlement"`
}

func NewSettlementEntry(
	ctx sdk.Context,
	orderID uint64,
	account string,
	direction types.PositionDirection,
	priceDenom string,
	assetDenom string,
	quantity sdk.Dec,
	executionCostOrProceed sdk.Dec,
	expectedCostOrProceed sdk.Dec,
	orderType types.OrderType,
) *types.SettlementEntry {
	return &types.SettlementEntry{
		OrderId:                orderID,
		PositionDirection:      GetContractPositionDirection(direction),
		PriceDenom:             priceDenom,
		AssetDenom:             assetDenom,
		Quantity:               quantity,
		ExecutionCostOrProceed: executionCostOrProceed,
		ExpectedCostOrProceed:  expectedCostOrProceed,
		Account:                account,
		OrderType:              GetContractOrderType(orderType),
		Timestamp:              uint64(ctx.BlockTime().Unix()),
		Height:                 uint64(ctx.BlockHeight()),
	}
}
