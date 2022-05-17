package dex

import (
	"fmt"
	"math"
	"strconv"

	"github.com/sei-protocol/sei-chain/utils"
	"github.com/sei-protocol/sei-chain/x/dex/types"
)

const LIMIT_BUY_EVENT_TYPE = "dex_lb"
const LIMIT_SELL_EVENT_TYPE = "dex_ls"
const MARKET_BUY_EVENT_TYPE = "dex_mb"
const MARKET_SELL_EVENT_TYPE = "dex_ms"
const CREATOR_ATTR = "creator"
const PRICE_ATTR = "price"
const QUANTITY_ATTR = "quantity"

type LimitOrder struct {
	Price    uint64
	Quantity uint64
	Creator  string
	Long     bool
	Open     bool
	Leverage string
}

func (o *LimitOrder) FormattedCreatorWithSuffix() string {
	var suffix string
	if o.Open {
		suffix = types.OPEN_ORDER_CREATOR_SUFFIX
	} else {
		suffix = types.CLOSE_ORDER_CREATOR_SUFFIX
	}
	return fmt.Sprintf("%s%s%s%s%s", o.Creator, types.FORMATTED_ACCOUNT_DELIMITER, suffix, types.FORMATTED_ACCOUNT_DELIMITER, o.Leverage)
}

type MarketOrder struct {
	Quantity      uint64
	Creator       string
	Long          bool
	WorstPrice    uint64
	IsLiquidation bool
	Open          bool
	Leverage      string
}

func (o *MarketOrder) FormattedCreatorWithSuffix() string {
	var suffix string
	if o.Open {
		suffix = "o"
	} else {
		suffix = "c"
	}
	return fmt.Sprintf("%s%s%s%s%s", o.Creator, types.FORMATTED_ACCOUNT_DELIMITER, suffix, types.FORMATTED_ACCOUNT_DELIMITER, o.Leverage)
}

type CancelOrder struct {
	Price    uint64
	Creator  string
	Long     bool
	Quantity uint64
	Open     bool
	Leverage string
}

func (o *CancelOrder) FormattedCreatorWithSuffix() string {
	var suffix string
	if o.Open {
		suffix = "o"
	} else {
		suffix = "c"
	}
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
	if order.Long {
		for i, existingOrder := range o.MarketBuys {
			if existingOrder.WorstPrice < order.WorstPrice {
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
	} else {
		for i, existingOrder := range o.MarketSells {
			if existingOrder.WorstPrice > order.WorstPrice {
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
	}
}

func (o *Orders) AddLimitOrder(order LimitOrder) {
	if order.Long {
		o.LimitBuys = append(o.LimitBuys, order)
	} else {
		o.LimitSells = append(o.LimitSells, order)
	}
}

func (o *Orders) AddCancelOrder(order CancelOrder) {
	if order.Long {
		o.CancelBuys = append(o.CancelBuys, order)
	} else {
		o.CancelSells = append(o.CancelSells, order)
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
	Price       uint64
	Quantity    uint64
	Creator     string
	Limit       bool
	Long        bool
	Open        bool
	PriceDenom  string
	AssetDenom  string
	Leverage    string
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
		PriceDenom:        orderPlacement.PriceDenom,
		AssetDenom:        orderPlacement.AssetDenom,
		Price:             strconv.FormatUint(orderPlacement.Price, 10),
		Quantity:          strconv.FormatUint(orderPlacement.Quantity, 10),
		OrderType:         types.GetOrderType(orderPlacement.Limit),
		PositionDirection: types.GetPositionDirection(orderPlacement.Long),
		PositionEffect:    types.GetPositionEffect(orderPlacement.Open),
		Leverage:          orderPlacement.Leverage,
	}
}

func FromLiquidationOrder(liquidationOrder types.LiquidationOrder, orderId uint64) OrderPlacement {
	quantity, err := strconv.ParseUint(liquidationOrder.Quantity, 10, 64)
	if err != nil {
		panic(err)
	}
	var price uint64
	if liquidationOrder.Long {
		price = math.MaxUint64
	} else {
		price = 0
	}
	return OrderPlacement{
		Id:          orderId,
		Price:       price,
		Quantity:    quantity,
		Creator:     liquidationOrder.Account,
		Limit:       false,
		Long:        liquidationOrder.Long,
		Open:        false,
		PriceDenom:  liquidationOrder.PriceDenom,
		AssetDenom:  liquidationOrder.AssetDenom,
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
	Denom   string
	Amount  uint64
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
		Denom:   depositInfo.Denom,
		Amount:  strconv.FormatUint(depositInfo.Amount, 10),
	}
}

type OrderCancellation struct {
	Price      uint64
	Quantity   uint64
	Creator    string
	Long       bool
	Open       bool
	PriceDenom string
	AssetDenom string
	Leverage   string
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
		PriceDenom:        orderCancellation.PriceDenom,
		AssetDenom:        orderCancellation.AssetDenom,
		Price:             strconv.FormatUint(orderCancellation.Price, 10),
		Quantity:          strconv.FormatUint(orderCancellation.Quantity, 10),
		PositionDirection: types.GetPositionDirection(orderCancellation.Long),
		PositionEffect:    types.GetPositionEffect(orderCancellation.Open),
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
