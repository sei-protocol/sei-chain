package dex

import (
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/utils"
	"github.com/sei-protocol/sei-chain/x/dex/types"
)

const (
	LIMIT_BUY_EVENT_TYPE   = "dex_lb"
	LIMIT_SELL_EVENT_TYPE  = "dex_ls"
	MARKET_BUY_EVENT_TYPE  = "dex_mb"
	MARKET_SELL_EVENT_TYPE = "dex_ms"
	CREATOR_ATTR           = "creator"
	PRICE_ATTR             = "price"
	QUANTITY_ATTR          = "quantity"
)

type LimitOrder struct {
	Price     sdk.Dec
	Quantity  sdk.Dec
	Creator   string
	Direction types.PositionDirection
	Effect    types.PositionEffect
	Leverage  sdk.Dec
}

func (o *LimitOrder) FormattedCreatorWithSuffix() string {
	suffix := types.POSITION_EFFECT_TO_SUFFIX[o.Effect]
	return fmt.Sprintf("%s%s%s%s%s", o.Creator, types.FORMATTED_ACCOUNT_DELIMITER, suffix, types.FORMATTED_ACCOUNT_DELIMITER, o.Leverage)
}

type MarketOrder struct {
	Quantity      sdk.Dec
	Creator       string
	Direction     types.PositionDirection
	WorstPrice    sdk.Dec
	IsLiquidation bool
	Effect        types.PositionEffect
	Leverage      sdk.Dec
}

func (o *MarketOrder) FormattedCreatorWithSuffix() string {
	suffix := types.POSITION_EFFECT_TO_SUFFIX[o.Effect]
	return fmt.Sprintf("%s%s%s%s%s", o.Creator, types.FORMATTED_ACCOUNT_DELIMITER, suffix, types.FORMATTED_ACCOUNT_DELIMITER, o.Leverage)
}

type CancelOrder struct {
	Price     sdk.Dec
	Creator   string
	Direction types.PositionDirection
	Quantity  sdk.Dec
	Effect    types.PositionEffect
	Leverage  sdk.Dec
}

func (o *CancelOrder) FormattedCreatorWithSuffix() string {
	suffix := types.POSITION_EFFECT_TO_SUFFIX[o.Effect]
	return fmt.Sprintf("%s%s%s%s%s", o.Creator, types.FORMATTED_ACCOUNT_DELIMITER, suffix, types.FORMATTED_ACCOUNT_DELIMITER, o.Leverage)
}

type CancelAll struct {
	Creator string
}

type Orders struct {
	LimitBuys   []LimitOrder
	LimitSells  []LimitOrder
	MarketBuys  []MarketOrder
	MarketSells []MarketOrder
	CancelBuys  []CancelOrder
	CancelSells []CancelOrder
	CancelAlls  []CancelAll
}

func (o *Orders) AddMarketOrder(order MarketOrder) {
	idx := -1
	switch order.Direction {
	case types.PositionDirection_LONG:
		for i, existingOrder := range o.MarketBuys {
			if existingOrder.WorstPrice.LT(order.WorstPrice) {
				idx = i
				break
			}
		}
		newMarketBuys := append(o.MarketBuys, order)
		if idx != -1 {
			copy(newMarketBuys[idx+1:], newMarketBuys[idx:])
			newMarketBuys[idx] = order
		}
		o.MarketBuys = newMarketBuys
	case types.PositionDirection_SHORT:
		for i, existingOrder := range o.MarketSells {
			if existingOrder.WorstPrice.GT(order.WorstPrice) {
				idx = i
				break
			}
		}
		newMarketSells := append(o.MarketSells, order)
		if idx != -1 {
			copy(newMarketSells[idx+1:], newMarketSells[idx:])
			newMarketSells[idx] = order
		}
		o.MarketSells = newMarketSells
	default:
		panic("Unknown direction")
	}
}

func (o *Orders) AddLimitOrder(order LimitOrder) {
	switch order.Direction {
	case types.PositionDirection_LONG:
		o.LimitBuys = append(o.LimitBuys, order)
	case types.PositionDirection_SHORT:
		o.LimitSells = append(o.LimitSells, order)
	default:
		panic("Unknown direction")
	}
}

func (o *Orders) AddCancelOrder(order CancelOrder) {
	switch order.Direction {
	case types.PositionDirection_LONG:
		o.CancelBuys = append(o.CancelBuys, order)
	case types.PositionDirection_SHORT:
		o.CancelSells = append(o.CancelSells, order)
	default:
		panic("Unknown direction")
	}
}

func (o Orders) String() string {
	return fmt.Sprintf(
		"Limit Buys: %d, Limit Sells: %d, Market Buys: %d, Market Sells: %d, Cancel Buys: %d, Cancel Sells: %d, Cancel Alls: %d",
		len(o.LimitBuys), len(o.LimitSells),
		len(o.MarketBuys), len(o.MarketSells),
		len(o.CancelBuys), len(o.CancelSells),
		len(o.CancelAlls),
	)
}

func NewOrders() *Orders {
	return &Orders{
		LimitBuys:   []LimitOrder{},
		LimitSells:  []LimitOrder{},
		MarketBuys:  []MarketOrder{},
		MarketSells: []MarketOrder{},
		CancelBuys:  []CancelOrder{},
		CancelSells: []CancelOrder{},
		CancelAlls:  []CancelAll{},
	}
}

type OrderPlacement struct {
	Id          uint64
	Price       sdk.Dec
	Quantity    sdk.Dec
	Creator     string
	OrderType   types.OrderType
	Direction   types.PositionDirection
	Effect      types.PositionEffect
	PriceDenom  types.Denom
	AssetDenom  types.Denom
	Leverage    sdk.Dec
	Liquidation bool
}

type OrderPlacements struct {
	Orders []OrderPlacement
}

func NewOrderPlacements() *OrderPlacements {
	return &OrderPlacements{
		Orders: []OrderPlacement{},
	}
}

func ToContractOrderPlacement(orderPlacement OrderPlacement) types.ContractOrderPlacement {
	return types.ContractOrderPlacement{
		Id:                orderPlacement.Id,
		Account:           orderPlacement.Creator,
		PriceDenom:        types.GetContractDenomName(orderPlacement.PriceDenom),
		AssetDenom:        types.GetContractDenomName(orderPlacement.AssetDenom),
		Price:             orderPlacement.Price,
		Quantity:          orderPlacement.Quantity,
		OrderType:         types.GetContractOrderType(orderPlacement.OrderType),
		PositionDirection: types.GetContractPositionDirection(orderPlacement.Direction),
		PositionEffect:    types.GetContractPositionEffect(orderPlacement.Effect),
		Leverage:          orderPlacement.Leverage,
	}
}

func FromLiquidationOrder(liquidationOrder types.LiquidationOrder, orderId uint64) OrderPlacement {
	var price sdk.Dec
	var direction types.PositionDirection
	if liquidationOrder.Long {
		price = sdk.MaxSortableDec
		direction = types.PositionDirection_LONG
	} else {
		price = sdk.ZeroDec()
		direction = types.PositionDirection_SHORT
	}
	priceDenom, priceUnit, err := types.GetDenomFromStr(liquidationOrder.PriceDenom)
	if err != nil {
		panic(err)
	}
	assetDenom, assetUnit, err := types.GetDenomFromStr(liquidationOrder.AssetDenom)
	if err != nil {
		panic(err)
	}
	return OrderPlacement{
		Id:          orderId,
		Price:       types.ConvertDecToStandard(priceUnit, price),
		Quantity:    types.ConvertDecToStandard(assetUnit, liquidationOrder.Quantity),
		Creator:     liquidationOrder.Account,
		OrderType:   types.OrderType_MARKET,
		Direction:   direction,
		Effect:      types.PositionEffect_CLOSE,
		PriceDenom:  priceDenom,
		AssetDenom:  assetDenom,
		Leverage:    liquidationOrder.Leverage,
		Liquidation: true,
	}
}

func (o *OrderPlacements) FilterOutAccounts(badAccounts []string) {
	badAccountsSet := utils.NewStringSet(badAccounts)
	newOrders := []OrderPlacement{}
	for _, order := range o.Orders {
		if !badAccountsSet.Contains(order.Creator) {
			newOrders = append(newOrders, order)
		}
	}
	o.Orders = newOrders
}

func (o *OrderPlacements) FilterOutIds(badIds []uint64) {
	badIdsSet := utils.NewUInt64Set(badIds)
	newOrders := []OrderPlacement{}
	for _, order := range o.Orders {
		if !badIdsSet.Contains(order.Id) {
			newOrders = append(newOrders, order)
		}
	}
	o.Orders = newOrders
}

type DepositInfoEntry struct {
	Creator string
	Denom   types.Denom
	Amount  sdk.Dec
}

type DepositInfo struct {
	DepositInfoList []DepositInfoEntry
}

func NewDepositInfo() *DepositInfo {
	return &DepositInfo{
		DepositInfoList: []DepositInfoEntry{},
	}
}

func ToContractDepositInfo(depositInfo DepositInfoEntry) types.ContractDepositInfo {
	return types.ContractDepositInfo{
		Account: depositInfo.Creator,
		Denom:   types.GetContractDenomName(depositInfo.Denom),
		Amount:  depositInfo.Amount,
	}
}

type OrderCancellation struct {
	Price      sdk.Dec
	Quantity   sdk.Dec
	Creator    string
	Direction  types.PositionDirection
	Effect     types.PositionEffect
	PriceDenom types.Denom
	AssetDenom types.Denom
	Leverage   sdk.Dec
}

type CancellationFromLiquidation struct {
	Creator string
}

type OrderCancellations struct {
	OrderCancellations       []OrderCancellation
	LiquidationCancellations []CancellationFromLiquidation
}

func NewOrderCancellations() *OrderCancellations {
	return &OrderCancellations{
		OrderCancellations:       []OrderCancellation{},
		LiquidationCancellations: []CancellationFromLiquidation{},
	}
}

func ToContractOrderCancellation(orderCancellation OrderCancellation) types.ContractOrderCancellation {
	return types.ContractOrderCancellation{
		Account:           orderCancellation.Creator,
		PriceDenom:        types.GetContractDenomName(orderCancellation.PriceDenom),
		AssetDenom:        types.GetContractDenomName(orderCancellation.AssetDenom),
		Price:             orderCancellation.Price,
		Quantity:          orderCancellation.Quantity,
		PositionDirection: types.GetContractPositionDirection(orderCancellation.Direction),
		PositionEffect:    types.GetContractPositionEffect(orderCancellation.Effect),
		Leverage:          orderCancellation.Leverage,
	}
}

func (o *OrderCancellations) UpdateForLiquidation(liquidatedAccounts []string) {
	badAccountsSet := utils.NewStringSet(liquidatedAccounts)
	newOrderCancellations := []OrderCancellation{}
	for _, order := range o.OrderCancellations {
		if !badAccountsSet.Contains(order.Creator) {
			newOrderCancellations = append(newOrderCancellations, order)
		}
	}
	o.OrderCancellations = newOrderCancellations
	for _, account := range liquidatedAccounts {
		o.LiquidationCancellations = append(o.LiquidationCancellations, CancellationFromLiquidation{
			Creator: account,
		})
	}
}

type LiquidationRequest struct {
	Requestor          string
	AccountToLiquidate string
}

type LiquidationRequests []LiquidationRequest

func (lrs *LiquidationRequests) IsAccountLiquidating(accountToLiquidate string) bool {
	for _, lr := range *lrs {
		if lr.AccountToLiquidate == accountToLiquidate {
			return true
		}
	}
	return false
}

func (lrs *LiquidationRequests) AddNewLiquidationRequest(requestor string, accountToLiquidate string) {
	if lrs.IsAccountLiquidating(accountToLiquidate) {
		return
	}
	*lrs = append(*lrs, LiquidationRequest{
		Requestor:          requestor,
		AccountToLiquidate: accountToLiquidate,
	})
}
