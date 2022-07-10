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

	for _, fund := range msg.Funds {
		k.MemState.GetDepositInfo(types.ContractAddress(msg.GetContractAddr())).AddDeposit(dexcache.DepositInfoEntry{
			Creator: msg.Creator,
			Denom:   fund.Denom,
			Amount:  sdk.NewDec(fund.Amount.Int64()),
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

	nextId := k.GetNextOrderId(ctx)
	idsInResp := []uint64{}
	for _, order := range msg.GetOrders() {
		ticksize, found := k.Keeper.GetTickSizeForPair(ctx, msg.GetContractAddr(), types.Pair{PriceDenom: order.PriceDenom, AssetDenom: order.AssetDenom})
		if !found {
			return nil, sdkerrors.Wrapf(sdkerrors.ErrKeyNotFound, "the pair {price:%s,asset:%s} has no ticksize configured", order.PriceDenom, order.AssetDenom)
		}
		pair := types.Pair{PriceDenom: order.PriceDenom, AssetDenom: order.AssetDenom, Ticksize: &ticksize}
		pairStr := types.GetPairString(&pair)
		order.Id = nextId
		order.Account = msg.Creator
		order.ContractAddr = msg.GetContractAddr()
		k.MemState.GetBlockOrders(types.ContractAddress(msg.GetContractAddr()), pairStr).AddOrder(*order)
		idsInResp = append(idsInResp, nextId)
		nextId += 1
	}
	k.SetNextOrderId(ctx, nextId)

	return &types.MsgPlaceOrdersResponse{
		OrderIds: idsInResp,
	}, nil
}
