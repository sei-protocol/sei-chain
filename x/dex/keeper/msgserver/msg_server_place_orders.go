package msgserver

import (
	"context"
	"errors"
	"fmt"
	"math/big"

	conversion "github.com/sei-protocol/sei-chain/utils"

	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	dexcache "github.com/sei-protocol/sei-chain/x/dex/cache"
	"github.com/sei-protocol/sei-chain/x/dex/types"
	typesutils "github.com/sei-protocol/sei-chain/x/dex/types/utils"
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
		k.MemState.GetDepositInfo(typesutils.ContractAddress(msg.GetContractAddr())).AddDeposit(dexcache.DepositInfoEntry{
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

	if msg.AutoCalculateDeposit {
		calculatedCollateral := sdk.NewDecFromBigInt(big.NewInt(0))
		for _, order := range msg.Orders {
			calculatedCollateral = calculatedCollateral.Add(order.Price.Mul(order.Quantity))
		}

		// throw error if current funds amount is less than calculatedCollateral
		if calculatedCollateral.GT(sdk.NewDecFromInt(k.Keeper.BankKeeper.GetBalance(ctx, sdk.AccAddress(msg.GetCreator()), msg.Orders[0].PriceDenom).Amount)) {
			return nil, sdkerrors.Wrapf(sdkerrors.ErrInsufficientFunds, "insufficient funds to place order")
		}

		_, validDenom := k.Keeper.GetAssetMetadataByDenom(ctx, msg.Orders[0].PriceDenom)
		if !validDenom {
			return nil, sdkerrors.Wrapf(sdkerrors.ErrInvalidRequest, "invalid price denom")
		}

		newAmount, newDenom, _ := conversion.ConvertWholeToMicroDenom(calculatedCollateral, msg.Orders[0].PriceDenom)
		msg.Funds[0].Amount = newAmount.RoundInt()
		msg.Funds[0].Denom = newDenom
	}

	if err := k.transferFunds(goCtx, msg); err != nil {
		return nil, err
	}

	nextID := k.GetNextOrderID(ctx)
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
		k.MemState.GetBlockOrders(typesutils.ContractAddress(msg.GetContractAddr()), pairStr).AddOrder(*order)
		idsInResp = append(idsInResp, nextID)
		nextID++
	}
	k.SetNextOrderID(ctx, nextID)

	return &types.MsgPlaceOrdersResponse{
		OrderIds: idsInResp,
	}, nil
}

func (k msgServer) validateOrder(order *types.Order) error {
	if order.Quantity.IsNil() || order.Quantity.IsNegative() {
		return errors.New(fmt.Sprintf("invalid order quantity: %s", order.Quantity))
	}
	if order.Price.IsNil() || order.Price.IsNegative() {
		return errors.New(fmt.Sprintf("invalid order price: %s", order.Price))
	}
	if len(order.AssetDenom) == 0 {
		return errors.New("invalid order, asset denom is empty")
	}
	if len(order.PriceDenom) == 0 {
		return errors.New("invalid order, price denom is empty")
	}
	return nil
}
