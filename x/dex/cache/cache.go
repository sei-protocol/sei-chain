package dex

import (
	"github.com/sei-protocol/sei-chain/utils/datastructures"
	typesutils "github.com/sei-protocol/sei-chain/x/dex/types/utils"
)

type memStateItem interface {
	GetAccount() string
}

type memStateItems[T memStateItem] struct {
	internal []T
	copier   func(T) T
}

func NewItems[T memStateItem](copier func(T) T) memStateItems[T] {
	return memStateItems[T]{internal: []T{}, copier: copier}
}

func (i *memStateItems[T]) Get() []T {
	return i.internal
}

func (i *memStateItems[T]) Add(newItem T) {
	i.internal = append(i.internal, newItem)
}

func (i *memStateItems[T]) FilterByAccount(account string) {
	newItems := []T{}
	for _, item := range i.internal {
		if item.GetAccount() == account {
			continue
		}
		newItems = append(newItems, item)
	}
	i.internal = newItems
}

func (i *memStateItems[T]) Copy() *memStateItems[T] {
	copy := NewItems(i.copier)
	for _, item := range i.internal {
		copy.Add(i.copier(item))
	}
	return &copy
}

type MemState struct {
	BlockOrders *datastructures.TypedNestedSyncMap[
		typesutils.ContractAddress,
		typesutils.PairString,
		*BlockOrders,
	]
	BlockCancels *datastructures.TypedNestedSyncMap[
		typesutils.ContractAddress,
		typesutils.PairString,
		*BlockCancellations,
	]
	DepositInfo         *datastructures.TypedSyncMap[typesutils.ContractAddress, *DepositInfo]
	LiquidationRequests *datastructures.TypedSyncMap[typesutils.ContractAddress, *LiquidationRequests]
}

func NewMemState() *MemState {
	return &MemState{
		BlockOrders: datastructures.NewTypedNestedSyncMap[
			typesutils.ContractAddress,
			typesutils.PairString,
			*BlockOrders,
		](),
		BlockCancels: datastructures.NewTypedNestedSyncMap[
			typesutils.ContractAddress,
			typesutils.PairString,
			*BlockCancellations,
		](),
		DepositInfo:         datastructures.NewTypedSyncMap[typesutils.ContractAddress, *DepositInfo](),
		LiquidationRequests: datastructures.NewTypedSyncMap[typesutils.ContractAddress, *LiquidationRequests](),
	}
}

func (s *MemState) GetBlockOrders(contractAddr typesutils.ContractAddress, pair typesutils.PairString) *BlockOrders {
	ordersForPair, _ := s.BlockOrders.LoadOrStoreNested(contractAddr, pair, NewOrders())
	return ordersForPair
}

func (s *MemState) GetBlockCancels(contractAddr typesutils.ContractAddress, pair typesutils.PairString) *BlockCancellations {
	cancelsForPair, _ := s.BlockCancels.LoadOrStoreNested(contractAddr, pair, NewCancels())
	return cancelsForPair
}

func (s *MemState) GetDepositInfo(contractAddr typesutils.ContractAddress) *DepositInfo {
	depositsForContract, _ := s.DepositInfo.LoadOrStore(contractAddr, NewDepositInfo())
	return depositsForContract
}

func (s *MemState) GetLiquidationRequests(contractAddr typesutils.ContractAddress) *LiquidationRequests {
	liquidationsForContract, _ := s.LiquidationRequests.LoadOrStore(contractAddr, NewLiquidationRequests())
	return liquidationsForContract
}

func (s *MemState) Clear() {
	s.BlockOrders = datastructures.NewTypedNestedSyncMap[
		typesutils.ContractAddress,
		typesutils.PairString,
		*BlockOrders,
	]()
	s.BlockCancels = datastructures.NewTypedNestedSyncMap[
		typesutils.ContractAddress,
		typesutils.PairString,
		*BlockCancellations,
	]()
	s.DepositInfo = datastructures.NewTypedSyncMap[typesutils.ContractAddress, *DepositInfo]()
	s.LiquidationRequests = datastructures.NewTypedSyncMap[typesutils.ContractAddress, *LiquidationRequests]()
}

func (s *MemState) ClearCancellationForPair(contractAddr typesutils.ContractAddress, pair typesutils.PairString) {
	s.BlockCancels.StoreNested(contractAddr, pair, NewCancels())
}

func (s *MemState) DeepCopy() *MemState {
	copy := NewMemState()
	copy.BlockOrders = s.BlockOrders.DeepCopy(func(o *BlockOrders) *BlockOrders { return o.Copy() })
	copy.BlockCancels = s.BlockCancels.DeepCopy(func(o *BlockCancellations) *BlockCancellations { return o.Copy() })
	copy.DepositInfo = s.DepositInfo.DeepCopy(func(o *DepositInfo) *DepositInfo { return o.Copy() })
	copy.LiquidationRequests = s.LiquidationRequests.DeepCopy(func(o *LiquidationRequests) *LiquidationRequests { return o.Copy() })
	return copy
}

func (s *MemState) DeepFilterAccount(account string) {
	s.BlockOrders.DeepApply(func(o *BlockOrders) { o.FilterByAccount(account) })
	s.BlockCancels.DeepApply(func(o *BlockCancellations) { o.FilterByAccount(account) })
	s.DepositInfo.DeepApply(func(o *DepositInfo) { o.FilterByAccount(account) })
	s.LiquidationRequests.DeepApply(func(o *LiquidationRequests) { o.FilterByAccount(account) })
}

func (lrs *LiquidationRequests) Copy() *LiquidationRequests {
	copy := LiquidationRequests([]LiquidationRequest{})
	for _, request := range *lrs {
		copy.AddNewLiquidationRequest(request.Requestor, request.AccountToLiquidate)
	}
	return &copy
}
