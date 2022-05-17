package keeper

import (
	"context"

	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	dexcache "github.com/sei-protocol/sei-chain/x/dex/cache"
	"github.com/sei-protocol/sei-chain/x/dex/types"
)

func (k msgServer) transferFunds(goCtx context.Context, msg *types.MsgPlaceOrders) error {
	if len(msg.Funds) == 0 {
		return nil
	}
	_, span := (*k.tracingInfo.Tracer).Start(goCtx, "TransferFunds")
	defer span.End()

	ctx := sdk.UnwrapSDKContext(goCtx)
	callerAddr, err := sdk.AccAddressFromBech32(msg.Creator)
	if err != nil {
		return err
	}
	contractAddr, err := sdk.AccAddressFromBech32(msg.ContractAddr)
	if err != nil {
		return err
	}
	if err := k.BankKeeper.IsSendEnabledCoins(ctx, msg.Funds...); err != nil {
		return err
	}
	if k.BankKeeper.BlockedAddr(contractAddr) {
		return sdkerrors.Wrapf(sdkerrors.ErrUnauthorized, "%s is not allowed to receive funds", contractAddr.String())
	}
	if err := k.BankKeeper.SendCoins(ctx, callerAddr, contractAddr, msg.Funds); err != nil {
		return err
	}

	di := k.DepositInfo[msg.GetContractAddr()]
	for _, fund := range msg.Funds {
		di.DepositInfoList = append(di.DepositInfoList, dexcache.DepositInfoEntry{
			Creator: msg.Creator,
			Denom:   fund.Denom,
			Amount:  fund.Amount.Uint64(),
		})
	}
	return nil
}

func (k msgServer) PlaceOrders(goCtx context.Context, msg *types.MsgPlaceOrders) (*types.MsgPlaceOrdersResponse, error) {
	spanCtx, span := (*k.tracingInfo.Tracer).Start(goCtx, "PlaceOrders")
	defer span.End()

	ctx := sdk.UnwrapSDKContext(goCtx)

	if err := k.transferFunds(spanCtx, msg); err != nil {
		return nil, err
	}

	pairToOrderPlacements := k.OrderPlacements[msg.GetContractAddr()]

	nextId := k.GetNextOrderId(ctx)
	idsInResp := []uint64{}
	for _, orderPlacement := range msg.GetOrders() {
		pair := types.Pair{PriceDenom: orderPlacement.PriceDenom, AssetDenom: orderPlacement.AssetDenom}
		(*pairToOrderPlacements[pair.String()]).Orders = append(
			(*pairToOrderPlacements[pair.String()]).Orders,
			dexcache.OrderPlacement{
				Id:          nextId,
				Price:       orderPlacement.Price,
				Quantity:    orderPlacement.Quantity,
				Creator:     msg.Creator,
				PriceDenom:  orderPlacement.PriceDenom,
				AssetDenom:  orderPlacement.AssetDenom,
				Limit:       orderPlacement.Limit,
				Long:        orderPlacement.Long,
				Open:        orderPlacement.Open,
				Leverage:    orderPlacement.Leverage,
				Liquidation: false,
			},
		)
		idsInResp = append(idsInResp, nextId)
		nextId += 1
	}
	k.SetNextOrderId(ctx, nextId)

	return &types.MsgPlaceOrdersResponse{
		OrderIds: idsInResp,
	}, nil
}
