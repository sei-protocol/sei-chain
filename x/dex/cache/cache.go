package dex

import (
	"sort"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/utils"
	"github.com/sei-protocol/sei-chain/x/dex/types"
	typesutils "github.com/sei-protocol/sei-chain/x/dex/types/utils"
	"github.com/sei-protocol/sei-chain/x/dex/types/wasm"
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
	BlockOrders         map[typesutils.ContractAddress]map[typesutils.PairString]*BlockOrders
	BlockCancels        map[typesutils.ContractAddress]map[typesutils.PairString]*BlockCancellations
	DepositInfo         map[typesutils.ContractAddress]*DepositInfo
	LiquidationRequests map[typesutils.ContractAddress]*LiquidationRequests
}

// All new orders attempted to be placed in the current block
type BlockOrders []*types.Order

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
		BlockOrders:         map[typesutils.ContractAddress]map[typesutils.PairString]*BlockOrders{},
		BlockCancels:        map[typesutils.ContractAddress]map[typesutils.PairString]*BlockCancellations{},
		DepositInfo:         map[typesutils.ContractAddress]*DepositInfo{},
		LiquidationRequests: map[typesutils.ContractAddress]*LiquidationRequests{},
	}
}

func (s *MemState) GetBlockOrders(contractAddr typesutils.ContractAddress, pair typesutils.PairString) *BlockOrders {
	if _, ok := s.BlockOrders[contractAddr]; !ok {
		s.BlockOrders[contractAddr] = map[typesutils.PairString]*BlockOrders{}
	}
	if _, ok := s.BlockOrders[contractAddr][pair]; !ok {
		emptyBlockOrders := BlockOrders([]*types.Order{})
		s.BlockOrders[contractAddr][pair] = &emptyBlockOrders
	}
	return s.BlockOrders[contractAddr][pair]
}

func (s *MemState) GetBlockCancels(contractAddr typesutils.ContractAddress, pair typesutils.PairString) *BlockCancellations {
	if _, ok := s.BlockCancels[contractAddr]; !ok {
		s.BlockCancels[contractAddr] = map[typesutils.PairString]*BlockCancellations{}
	}
	if _, ok := s.BlockCancels[contractAddr][pair]; !ok {
		emptyBlockCancels := BlockCancellations([]types.Cancellation{})
		s.BlockCancels[contractAddr][pair] = &emptyBlockCancels
	}
	return s.BlockCancels[contractAddr][pair]
}

func (s *MemState) GetDepositInfo(contractAddr typesutils.ContractAddress) *DepositInfo {
	if _, ok := s.DepositInfo[contractAddr]; !ok {
		s.DepositInfo[contractAddr] = NewDepositInfo()
	}
	return s.DepositInfo[contractAddr]
}

func (s *MemState) GetLiquidationRequests(contractAddr typesutils.ContractAddress) *LiquidationRequests {
	if _, ok := s.LiquidationRequests[contractAddr]; !ok {
		emptyRequests := LiquidationRequests([]LiquidationRequest{})
		s.LiquidationRequests[contractAddr] = &emptyRequests
	}
	return s.LiquidationRequests[contractAddr]
}

func (s *MemState) Clear() {
	s.BlockOrders = map[typesutils.ContractAddress]map[typesutils.PairString]*BlockOrders{}
	s.BlockCancels = map[typesutils.ContractAddress]map[typesutils.PairString]*BlockCancellations{}
	s.DepositInfo = map[typesutils.ContractAddress]*DepositInfo{}
	s.LiquidationRequests = map[typesutils.ContractAddress]*LiquidationRequests{}
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
				copy.GetBlockCancels(contractAddr, pair).AddCancel(blockCancel)
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

func (s *MemState) DeepFilterAccount(account string) {
	for _, _map := range s.BlockOrders {
		for _, blockOrders := range _map {
			blockOrders.FilterByAccount(account)
		}
	}
	for _, _map := range s.BlockCancels {
		for _, blockCancels := range _map {
			blockCancels.FilterByAccount(account)
		}
	}
	for _, deposits := range s.DepositInfo {
		deposits.FilterByAccount(account)
	}
	for _, liquidations := range s.LiquidationRequests {
		liquidations.FilterByAccount(account)
	}
}

func (o *BlockOrders) AddOrder(order *types.Order) {
	*o = append(*o, &types.Order{
		Id:                order.Id,
		Status:            order.Status,
		Account:           order.Account,
		ContractAddr:      order.ContractAddr,
		Price:             order.Price,
		Quantity:          order.Quantity,
		PriceDenom:        order.PriceDenom,
		AssetDenom:        order.AssetDenom,
		Data:              order.Data,
		StatusDescription: order.StatusDescription,
		OrderType:         order.OrderType,
		PositionDirection: order.PositionDirection,
	})
}

func (o *BlockOrders) MarkFailedToPlaceByAccounts(accounts []string) {
	badAccountSet := utils.NewStringSet(accounts)
	newOrders := []*types.Order{}
	for _, order := range *o {
		if badAccountSet.Contains(order.Account) {
			order.Status = types.OrderStatus_FAILED_TO_PLACE
			order.StatusDescription = "Failed liquidation"
		}
		newOrders = append(newOrders, order)
	}
	*o = newOrders
}

func (o *BlockOrders) MarkFailedToPlace(failedOrders []wasm.UnsuccessfulOrder) {
	failedOrdersMap := map[uint64]wasm.UnsuccessfulOrder{}
	for _, failedOrder := range failedOrders {
		failedOrdersMap[failedOrder.ID] = failedOrder
	}
	newOrders := []*types.Order{}
	for _, order := range *o {
		if failedOrder, ok := failedOrdersMap[order.Id]; ok {
			order.Status = types.OrderStatus_FAILED_TO_PLACE
			order.StatusDescription = failedOrder.Reason
		}
		newOrders = append(newOrders, order)
	}
	*o = newOrders
}

func (o *BlockOrders) FilterByAccount(account string) {
	newOrders := []*types.Order{}
	for _, order := range *o {
		if order.Account == account {
			continue
		}
		newOrders = append(newOrders, order)
	}
	*o = newOrders
}

func (o *BlockOrders) GetSortedMarketOrders(direction types.PositionDirection, includeLiquidationOrders bool) []*types.Order {
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

func (o *BlockOrders) GetLimitOrders(direction types.PositionDirection) []*types.Order {
	return o.getOrdersByCriteria(types.OrderType_LIMIT, direction)
}

func (o *BlockOrders) getOrdersByCriteria(orderType types.OrderType, direction types.PositionDirection) []*types.Order {
	res := []*types.Order{}
	for _, order := range *o {
		if order.OrderType != orderType || order.PositionDirection != direction {
			continue
		}
		if order.Status == types.OrderStatus_FAILED_TO_PLACE {
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

func (d *DepositInfo) FilterByAccount(account string) {
	newDeposits := []DepositInfoEntry{}
	for _, deposit := range *d {
		if deposit.Creator == account {
			continue
		}
		newDeposits = append(newDeposits, deposit)
	}
	*d = newDeposits
}

func ToContractDepositInfo(depositInfo DepositInfoEntry) wasm.ContractDepositInfo {
	return wasm.ContractDepositInfo{
		Account: depositInfo.Creator,
		Denom:   depositInfo.Denom,
		Amount:  depositInfo.Amount,
	}
}

func (c *BlockCancellations) AddCancel(cancel types.Cancellation) {
	*c = append(*c, cancel)
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

func (c *BlockCancellations) FilterByAccount(account string) {
	newCancels := []types.Cancellation{}
	for _, cancel := range *c {
		if cancel.Creator == account {
			continue
		}
		newCancels = append(newCancels, cancel)
	}
	*c = newCancels
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

func (lrs *LiquidationRequests) FilterByAccount(account string) {
	newRequests := []LiquidationRequest{}
	for _, request := range *lrs {
		if request.Requestor == account {
			continue
		}
		newRequests = append(newRequests, request)
	}
	*lrs = newRequests
}
