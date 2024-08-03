package msgs

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/utils"
	dextypes "github.com/sei-protocol/sei-chain/x/dex/types"
)

type Market struct {
	contract   string
	priceDenom string
	assetDenom string
}

func NewMarket(contract string, priceDenom string, assetDenom string) *Market {
	return &Market{
		contract:   contract,
		priceDenom: priceDenom,
		assetDenom: assetDenom,
	}
}

func (m *Market) Register(admin sdk.AccAddress, deps []string, deposit uint64) []sdk.Msg {
	pointOne := sdk.NewDecWithPrec(1, 1)
	return []sdk.Msg{dextypes.NewMsgRegisterContract(admin.String(), 0, m.contract, true, utils.Map(deps, func(dep string) *dextypes.ContractDependencyInfo {
		return &dextypes.ContractDependencyInfo{Dependency: dep}
	}), deposit), dextypes.NewMsgRegisterPairs(admin.String(), []dextypes.BatchContractPair{{
		ContractAddr: m.contract,
		Pairs: []*dextypes.Pair{{
			PriceDenom:       m.priceDenom,
			AssetDenom:       m.assetDenom,
			PriceTicksize:    &pointOne,
			QuantityTicksize: &pointOne,
		}},
	}})}
}

func (m *Market) LongLimitOrder(account sdk.AccAddress, price string, quantity string) *dextypes.MsgPlaceOrders {
	o := m.commonOrder(account, price, quantity)
	o.PositionDirection = dextypes.PositionDirection_LONG
	o.OrderType = dextypes.OrderType_LIMIT
	return dextypes.NewMsgPlaceOrders(account.String(), []*dextypes.Order{o}, m.contract, fundForOrder(o))
}

func (m *Market) ShortLimitOrder(account sdk.AccAddress, price string, quantity string) *dextypes.MsgPlaceOrders {
	o := m.commonOrder(account, price, quantity)
	o.PositionDirection = dextypes.PositionDirection_SHORT
	o.OrderType = dextypes.OrderType_LIMIT
	return dextypes.NewMsgPlaceOrders(account.String(), []*dextypes.Order{o}, m.contract, fundForOrder(o))
}

func (m *Market) LongMarketOrder(account sdk.AccAddress, price string, quantity string) *dextypes.MsgPlaceOrders {
	o := m.commonOrder(account, price, quantity)
	o.PositionDirection = dextypes.PositionDirection_LONG
	o.OrderType = dextypes.OrderType_MARKET
	return dextypes.NewMsgPlaceOrders(account.String(), []*dextypes.Order{o}, m.contract, fundForOrder(o))
}

func (m *Market) ShortMarketOrder(account sdk.AccAddress, price string, quantity string) *dextypes.MsgPlaceOrders {
	o := m.commonOrder(account, price, quantity)
	o.PositionDirection = dextypes.PositionDirection_SHORT
	o.OrderType = dextypes.OrderType_MARKET
	return dextypes.NewMsgPlaceOrders(account.String(), []*dextypes.Order{o}, m.contract, fundForOrder(o))
}

func (m *Market) CancelLongOrder(account sdk.AccAddress, price string, id uint64) *dextypes.MsgCancelOrders {
	c := m.commonCancel(account, price, id)
	c.PositionDirection = dextypes.PositionDirection_LONG
	return dextypes.NewMsgCancelOrders(account.String(), []*dextypes.Cancellation{c}, m.contract)
}

func (m *Market) CancelShortOrder(account sdk.AccAddress, price string, id uint64) *dextypes.MsgCancelOrders {
	c := m.commonCancel(account, price, id)
	c.PositionDirection = dextypes.PositionDirection_SHORT
	return dextypes.NewMsgCancelOrders(account.String(), []*dextypes.Cancellation{c}, m.contract)
}

func (m *Market) commonOrder(account sdk.AccAddress, price string, quantity string) *dextypes.Order {
	return &dextypes.Order{
		Account:    account.String(),
		Price:      sdk.MustNewDecFromStr(price),
		Quantity:   sdk.MustNewDecFromStr(quantity),
		PriceDenom: m.priceDenom,
		AssetDenom: m.assetDenom,
	}
}

func (m *Market) commonCancel(account sdk.AccAddress, price string, id uint64) *dextypes.Cancellation {
	return &dextypes.Cancellation{
		Creator:      account.String(),
		Price:        sdk.MustNewDecFromStr(price),
		Id:           id,
		PriceDenom:   m.priceDenom,
		AssetDenom:   m.assetDenom,
		ContractAddr: m.contract,
	}
}

func fundForOrder(o *dextypes.Order) sdk.Coins {
	return sdk.NewCoins(sdk.NewCoin("usei", o.Price.Mul(o.Quantity).RoundInt()))
}
