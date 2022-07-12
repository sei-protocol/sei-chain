package dex

import (
	"sort"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/utils"
	"github.com/sei-protocol/sei-chain/x/dex/types"
)

const (
	LimitBuyEventType   = "dex_lb"
	LimitSellEventType  = "dex_ls"
	MarketBuyEventType  = "dex_mb"
	MarketSellEventType = "dex_ms"
	CreatorAttr         = "creator"
	PriceAttr           = "price"
	QuantityAttr        = "quantity"
)

type MemState struct {
	BlockOrders         map[types.ContractAddress]map[types.PairString]*BlockOrders
	BlockCancels        map[types.ContractAddress]map[types.PairString]*BlockCancellations
	DepositInfo         map[types.ContractAddress]*DepositInfo
	LiquidationRequests map[types.ContractAddress]*LiquidationRequests
}

// All new orders attempted to be placed in the current block
type BlockOrders []types.Order

type DepositInfoEntry struct {
	Creator string
	Denom   string
	Amount  sdk.Dec
}

type DepositInfo []DepositInfoEntry

type BlockCancellations []types.Cancellation

type LiquidationRequest struct {
	Requestor          string
	AccountToLiquidate string
}

type LiquidationRequests []LiquidationRequest

func NewMemState() *MemState {
	return &MemState{
		BlockOrders:         map[types.ContractAddress]map[types.PairString]*BlockOrders{},
		BlockCancels:        map[types.ContractAddress]map[types.PairString]*BlockCancellations{},
		DepositInfo:         map[types.ContractAddress]*DepositInfo{},
		LiquidationRequests: map[types.ContractAddress]*LiquidationRequests{},
	}
}

func (s *MemState) GetBlockOrders(contractAddr types.ContractAddress, pair types.PairString) *BlockOrders {
	if _, ok := s.BlockOrders[contractAddr]; !ok {
		s.BlockOrders[contractAddr] = map[types.PairString]*BlockOrders{}
	}
	if _, ok := s.BlockOrders[contractAddr][pair]; !ok {
		emptyBlockOrders := BlockOrders([]types.Order{})
		s.BlockOrders[contractAddr][pair] = &emptyBlockOrders
	}
	return s.BlockOrders[contractAddr][pair]
}

func (s *MemState) GetBlockCancels(contractAddr types.ContractAddress, pair types.PairString) *BlockCancellations {
	if _, ok := s.BlockCancels[contractAddr]; !ok {
		s.BlockCancels[contractAddr] = map[types.PairString]*BlockCancellations{}
	}
	if _, ok := s.BlockCancels[contractAddr][pair]; !ok {
		emptyBlockCancels := BlockCancellations([]types.Cancellation{})
		s.BlockCancels[contractAddr][pair] = &emptyBlockCancels
	}
	return s.BlockCancels[contractAddr][pair]
}

func (s *MemState) GetDepositInfo(contractAddr types.ContractAddress) *DepositInfo {
	if _, ok := s.DepositInfo[contractAddr]; !ok {
		s.DepositInfo[contractAddr] = NewDepositInfo()
	}
	return s.DepositInfo[contractAddr]
}

func (s *MemState) GetLiquidationRequests(contractAddr types.ContractAddress) *LiquidationRequests {
	if _, ok := s.LiquidationRequests[contractAddr]; !ok {
		emptyRequests := LiquidationRequests([]LiquidationRequest{})
		s.LiquidationRequests[contractAddr] = &emptyRequests
	}
	return s.LiquidationRequests[contractAddr]
}

func (s *MemState) Clear() {
	s.BlockOrders = map[types.ContractAddress]map[types.PairString]*BlockOrders{}
	s.BlockCancels = map[types.ContractAddress]map[types.PairString]*BlockCancellations{}
	s.DepositInfo = map[types.ContractAddress]*DepositInfo{}
	s.LiquidationRequests = map[types.ContractAddress]*LiquidationRequests{}
}

func (s *MemState) DeepCopy() *MemState {
	copy := NewMemState()
	for contractAddr, _map := range s.BlockOrders {
		for pair, blockOrders := range _map {
			for _, blockOrder := range *blockOrders {
				copy.GetBlockOrders(contractAddr, pair).AddOrder(blockOrder)
			}
		}
	}
	for contractAddr, _map := range s.BlockCancels {
		for pair, blockCancels := range _map {
			for _, blockCancel := range *blockCancels {
				copy.GetBlockCancels(contractAddr, pair).AddOrderIDToCancel(blockCancel.Id, blockCancel.Initiator)
			}
		}
	}
	for contractAddr, deposits := range s.DepositInfo {
		for _, deposit := range *deposits {
			copy.GetDepositInfo(contractAddr).AddDeposit(deposit)
		}
	}
	for contractAddr, liquidations := range s.LiquidationRequests {
		for _, liquidation := range *liquidations {
			copy.GetLiquidationRequests(contractAddr).AddNewLiquidationRequest(liquidation.Requestor, liquidation.AccountToLiquidate)
		}
	}
	return copy
}

func (o *BlockOrders) AddOrder(order types.Order) {
	*o = append(*o, order)
}

func (o *BlockOrders) MarkFailedToPlaceByAccounts(accounts []string) {
	badAccountSet := utils.NewStringSet(accounts)
	newOrders := []types.Order{}
	for _, order := range *o {
		if badAccountSet.Contains(order.Account) {
			order.Status = types.OrderStatus_FAILED_TO_PLACE
		}
		newOrders = append(newOrders, order)
	}
	*o = newOrders
}

func (o *BlockOrders) MarkFailedToPlaceByIds(ids []uint64) {
	badIDSet := utils.NewUInt64Set(ids)
	newOrders := []types.Order{}
	for _, order := range *o {
		if badIDSet.Contains(order.Id) {
			order.Status = types.OrderStatus_FAILED_TO_PLACE
		}
		newOrders = append(newOrders, order)
	}
	*o = newOrders
}

func (o *BlockOrders) GetSortedMarketOrders(direction types.PositionDirection, includeLiquidationOrders bool) []types.Order {
	res := o.getOrdersByCriteria(types.OrderType_MARKET, direction)
	if includeLiquidationOrders {
		res = append(res, o.getOrdersByCriteria(types.OrderType_LIQUIDATION, direction)...)
	}
	sort.Slice(res, func(i, j int) bool {
		// a price of 0 indicates that there is no worst price for the order, so it should
		// always be ranked at the top.
		if res[i].Price.IsZero() {
			return true
		} else if res[j].Price.IsZero() {
			return false
		}
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

func NewDepositInfo() *DepositInfo {
	emptyDepositInfo := DepositInfo([]DepositInfoEntry{})
	return &emptyDepositInfo
}

func (d *DepositInfo) AddDeposit(deposit DepositInfoEntry) {
	*d = append(*d, deposit)
}

func ToContractDepositInfo(depositInfo DepositInfoEntry) types.ContractDepositInfo {
	return types.ContractDepositInfo{
		Account: depositInfo.Creator,
		Denom:   depositInfo.Denom,
		Amount:  depositInfo.Amount,
	}
}

func (c *BlockCancellations) AddOrderIDToCancel(id uint64, initiator types.CancellationInitiator) {
	*c = append(*c, types.Cancellation{Id: id, Initiator: initiator})
}

func (c *BlockCancellations) FilterByIds(idsToRemove []uint64) {
	tmp := *c
	*c = []types.Cancellation{}
	badIDSet := utils.NewUInt64Set(idsToRemove)
	for _, cancel := range tmp {
		if !badIDSet.Contains(cancel.Id) {
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
