package msgserver

import (
	"context"
	"errors"
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	dexcache "github.com/sei-protocol/sei-chain/x/dex/cache"
	"github.com/sei-protocol/sei-chain/x/dex/types"
	typesutils "github.com/sei-protocol/sei-chain/x/dex/types/utils"
	dexutils "github.com/sei-protocol/sei-chain/x/dex/utils"
)

func (k msgServer) transferFunds(goCtx context.Context, msg *types.MsgPlaceOrders) error {
	if len(msg.Funds) == 0 {
		return nil
	}

	ctx := sdk.UnwrapSDKContext(goCtx)
	contractAddr, err := sdk.AccAddressFromBech32(msg.ContractAddr)
	if err != nil {
		return err
	}
	for _, fund := range msg.Funds {
		if fund.Amount.IsNil() {
			return errors.New("deposit amount cannot be nil")
		}
	}
	if err := k.BankKeeper.IsSendEnabledCoins(ctx, msg.Funds...); err != nil {
		return err
	}
	if k.BankKeeper.BlockedAddr(contractAddr) {
		return sdkerrors.Wrapf(sdkerrors.ErrUnauthorized, "%s is not allowed to receive funds", contractAddr.String())
	}

	for _, fund := range msg.Funds {
		if fund.Amount.IsNil() || fund.IsNegative() {
			return errors.New("fund deposits cannot be nil or negative")
		}
		dexutils.GetMemState(ctx.Context()).GetDepositInfo(ctx, typesutils.ContractAddress(msg.GetContractAddr())).Add(&dexcache.DepositInfoEntry{
			Creator: msg.Creator,
			Denom:   fund.Denom,
			Amount:  sdk.NewDec(fund.Amount.Int64()),
		})
	}
	return nil
}

func (k msgServer) PlaceOrders(goCtx context.Context, msg *types.MsgPlaceOrders) (*types.MsgPlaceOrdersResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)

	for _, order := range msg.Orders {
		if err := k.validateOrder(order); err != nil {
			return nil, err
		}
	}

	if err := k.transferFunds(goCtx, msg); err != nil {
		return nil, err
	}

	nextID := k.GetNextOrderID(ctx, msg.ContractAddr)
	idsInResp := []uint64{}
	for _, order := range msg.GetOrders() {
		ticksize, found := k.Keeper.GetTickSizeForPair(ctx, msg.GetContractAddr(), types.Pair{PriceDenom: order.PriceDenom, AssetDenom: order.AssetDenom})
		if !found {
			return nil, sdkerrors.Wrapf(sdkerrors.ErrKeyNotFound, "the pair {price:%s,asset:%s} has no ticksize configured", order.PriceDenom, order.AssetDenom)
		}
		pair := types.Pair{PriceDenom: order.PriceDenom, AssetDenom: order.AssetDenom, Ticksize: &ticksize}
		pairStr := typesutils.GetPairString(&pair)
		order.Id = nextID
		order.Account = msg.Creator
		order.ContractAddr = msg.GetContractAddr()
		dexutils.GetMemState(ctx.Context()).GetBlockOrders(ctx, typesutils.ContractAddress(msg.GetContractAddr()), pairStr).Add(order)
		idsInResp = append(idsInResp, nextID)
		nextID++
	}
	k.SetNextOrderID(ctx, msg.ContractAddr, nextID)

	return &types.MsgPlaceOrdersResponse{
		OrderIds: idsInResp,
	}, nil
}

func (k msgServer) validateOrder(order *types.Order) error {
	if order.Quantity.IsNil() || order.Quantity.IsNegative() {
		return fmt.Errorf("invalid order quantity: %s", order.Quantity)
	}
	if order.Price.IsNil() || order.Price.IsNegative() {
		return fmt.Errorf("invalid order price: %s", order.Price)
	}
	if len(order.AssetDenom) == 0 {
		return fmt.Errorf("invalid order, asset denom is empty")
	}
	if len(order.PriceDenom) == 0 {
		return fmt.Errorf("invalid order, price denom is empty")
	}
	return nil
}
