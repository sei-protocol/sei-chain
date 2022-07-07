package dex

import (
	"sort"

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

// All new orders attempted to be placed in the current block
type BlockOrders []types.Order

func (o *BlockOrders) AddOrder(order types.Order) {
	*o = append(*o, order)
}

func (o *BlockOrders) MarkFailedToPlaceByAccounts(accounts []string) {
	badAccountSet := utils.NewStringSet(accounts)
	for _, order := range *o {
		if badAccountSet.Contains(order.Account) {
			order.Status = types.OrderStatus_FAILED_TO_PLACE
		}
	}
}

func (o *BlockOrders) MarkFailedToPlaceByIds(ids []uint64) {
	badIdSet := utils.NewUInt64Set(ids)
	for _, order := range *o {
		if badIdSet.Contains(order.Id) {
			order.Status = types.OrderStatus_FAILED_TO_PLACE
		}
	}
}

func (o *BlockOrders) GetSortedMarketOrders(direction types.PositionDirection, includeLiquidationOrders bool) []types.Order {
	res := o.getOrdersByCriteria(types.OrderType_MARKET, direction)
	if includeLiquidationOrders {
		res = append(res, o.getOrdersByCriteria(types.OrderType_LIQUIDATION, direction)...)
	}
	sort.Slice(res, func(i, j int) bool {
		switch direction {
		case types.PositionDirection_LONG:
			return res[i].Price.GT(res[j].Price)
		case types.PositionDirection_SHORT:
			return res[i].Price.LT(res[j].Price)
		default:
			panic("Unknown direction")
		}
	})
	return res
}

func (o *BlockOrders) GetLimitOrders(direction types.PositionDirection) []types.Order {
	return o.getOrdersByCriteria(types.OrderType_LIMIT, direction)
}

func (o *BlockOrders) getOrdersByCriteria(orderType types.OrderType, direction types.PositionDirection) []types.Order {
	res := []types.Order{}
	for _, order := range *o {
		if order.OrderType != orderType || order.PositionDirection != direction {
			continue
		}
		res = append(res, order)
	}
	return res
}

type DepositInfoEntry struct {
	Creator string
	Denom   string
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
		Denom:   depositInfo.Denom,
		Amount:  depositInfo.Amount,
	}
}

type BlockCancellations []types.Cancellation

func (c *BlockCancellations) AddOrderIdToCancel(id uint64, initiator types.CancellationInitiator) {
	*c = append(*c, types.Cancellation{Id: id, Initiator: initiator})
}

func (c *BlockCancellations) FilterByIds(idsToRemove []uint64) {
	tmp := c
	*c = []types.Cancellation{}
	badIdSet := utils.NewUInt64Set(idsToRemove)
	for _, cancel := range *tmp {
		if !badIdSet.Contains(cancel.Id) {
			*c = append(*c, cancel)
		}
	}
}

func (c *BlockCancellations) GetIdsToCancel() []uint64 {
	res := []uint64{}
	for _, cancel := range *c {
		res = append(res, cancel.Id)
	}
	return res
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
