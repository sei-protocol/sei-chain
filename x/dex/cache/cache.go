package dex

import (
	"fmt"
	"sync"
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/sei-protocol/sei-chain/utils/datastructures"
	typesutils "github.com/sei-protocol/sei-chain/x/dex/types/utils"
)

const SynchronizationTimeoutInSeconds = 5

type memStateItem interface {
	GetAccount() string
}

type memStateItems[T memStateItem] struct {
	internal []T
	copier   func(T) T

	mu *sync.Mutex
}

func NewItems[T memStateItem](copier func(T) T) memStateItems[T] {
	return memStateItems[T]{internal: []T{}, copier: copier, mu: &sync.Mutex{}}
}

func (i *memStateItems[T]) Get() []T {
	i.mu.Lock()
	defer i.mu.Unlock()
	return i.internal
}

func (i *memStateItems[T]) Add(newItem T) {
	i.mu.Lock()
	defer i.mu.Unlock()
	i.internal = append(i.internal, newItem)
}

func (i *memStateItems[T]) FilterByAccount(account string) {
	i.mu.Lock()
	defer i.mu.Unlock()
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
	i.mu.Lock()
	defer i.mu.Unlock()
	copy := NewItems(i.copier)
	for _, item := range i.internal {
		copy.Add(i.copier(item))
	}
	return &copy
}

type MemState struct {
	blockOrders *datastructures.TypedNestedSyncMap[
		typesutils.ContractAddress,
		typesutils.PairString,
		*BlockOrders,
	]
	blockCancels *datastructures.TypedNestedSyncMap[
		typesutils.ContractAddress,
		typesutils.PairString,
		*BlockCancellations,
	]
	depositInfo *datastructures.TypedSyncMap[typesutils.ContractAddress, *DepositInfo]
}

func NewMemState() *MemState {
	return &MemState{
		blockOrders: datastructures.NewTypedNestedSyncMap[
			typesutils.ContractAddress,
			typesutils.PairString,
			*BlockOrders,
		](),
		blockCancels: datastructures.NewTypedNestedSyncMap[
			typesutils.ContractAddress,
			typesutils.PairString,
			*BlockCancellations,
		](),
		depositInfo: datastructures.NewTypedSyncMap[typesutils.ContractAddress, *DepositInfo](),
	}
}

func (s *MemState) GetAllBlockOrders(ctx sdk.Context, contractAddr typesutils.ContractAddress) *datastructures.TypedSyncMap[typesutils.PairString, *BlockOrders] {
	s.SynchronizeAccess(ctx, contractAddr)
	ordersMap, _ := s.blockOrders.LoadOrStore(contractAddr, datastructures.NewTypedSyncMap[typesutils.PairString, *BlockOrders]())
	return ordersMap
}

func (s *MemState) GetBlockOrders(ctx sdk.Context, contractAddr typesutils.ContractAddress, pair typesutils.PairString) *BlockOrders {
	s.SynchronizeAccess(ctx, contractAddr)
	ordersForPair, _ := s.blockOrders.LoadOrStoreNested(contractAddr, pair, NewOrders())
	return ordersForPair
}

func (s *MemState) GetBlockCancels(ctx sdk.Context, contractAddr typesutils.ContractAddress, pair typesutils.PairString) *BlockCancellations {
	s.SynchronizeAccess(ctx, contractAddr)
	cancelsForPair, _ := s.blockCancels.LoadOrStoreNested(contractAddr, pair, NewCancels())
	return cancelsForPair
}

func (s *MemState) GetDepositInfo(ctx sdk.Context, contractAddr typesutils.ContractAddress) *DepositInfo {
	s.SynchronizeAccess(ctx, contractAddr)
	depositsForContract, _ := s.depositInfo.LoadOrStore(contractAddr, NewDepositInfo())
	return depositsForContract
}

func (s *MemState) Clear() {
	s.blockOrders = datastructures.NewTypedNestedSyncMap[
		typesutils.ContractAddress,
		typesutils.PairString,
		*BlockOrders,
	]()
	s.blockCancels = datastructures.NewTypedNestedSyncMap[
		typesutils.ContractAddress,
		typesutils.PairString,
		*BlockCancellations,
	]()
	s.depositInfo = datastructures.NewTypedSyncMap[typesutils.ContractAddress, *DepositInfo]()
}

func (s *MemState) ClearCancellationForPair(ctx sdk.Context, contractAddr typesutils.ContractAddress, pair typesutils.PairString) {
	s.SynchronizeAccess(ctx, contractAddr)
	s.blockCancels.StoreNested(contractAddr, pair, NewCancels())
}

func (s *MemState) DeepCopy() *MemState {
	copy := NewMemState()
	copy.blockOrders = s.blockOrders.DeepCopy(func(o *BlockOrders) *BlockOrders { return o.Copy() })
	copy.blockCancels = s.blockCancels.DeepCopy(func(o *BlockCancellations) *BlockCancellations { return o.Copy() })
	copy.depositInfo = s.depositInfo.DeepCopy(func(o *DepositInfo) *DepositInfo { return o.Copy() })
	return copy
}

func (s *MemState) DeepFilterAccount(account string) {
	s.blockOrders.DeepApply(func(o *BlockOrders) { o.FilterByAccount(account) })
	s.blockCancels.DeepApply(func(o *BlockCancellations) { o.FilterByAccount(account) })
	s.depositInfo.DeepApply(func(o *DepositInfo) { o.FilterByAccount(account) })
}

func (s *MemState) SynchronizeAccess(ctx sdk.Context, contractAddr typesutils.ContractAddress) {
	executingContract := GetExecutingContract(ctx)
	if executingContract == nil {
		// not accessed by contract. no need to synchronize
		return
	}
	targetContractAddr := string(contractAddr)
	if executingContract.ContractAddr == targetContractAddr {
		// access by the contract itself does not need synchronization
		return
	}
	for _, dependency := range executingContract.Dependencies {
		if dependency.Dependency != targetContractAddr {
			continue
		}
		terminationSignals := GetTerminationSignals(ctx)
		if terminationSignals == nil {
			// synchronization should fail in the case of no termination signal to prevent race conditions.
			panic("no termination signal map found in context")
		}
		targetChannel, ok := terminationSignals.Load(dependency.ImmediateElderSibling)
		if !ok {
			// synchronization should fail in the case of no termination signal to prevent race conditions.
			panic(fmt.Sprintf("no termination signal channel for contract %s in context", dependency.ImmediateElderSibling))
		}

		select {
		case <-targetChannel:
			// since buffered channel can only be consumed once, we need to
			// requeue so that it can unblock other goroutines that waits for
			// the same channel.
			targetChannel <- struct{}{}
		case <-time.After(SynchronizationTimeoutInSeconds * time.Second):
			// synchronization should fail in the case of timeout to prevent race conditions.
			panic(fmt.Sprintf("timing out waiting for termination of %s", dependency.ImmediateElderSibling))
		}

		return
	}

	// fail loudly so that the offending contract can be rolled back.
	// eventually we will automatically de-register contracts that have to be rolled back
	// so that this would not become a point of attack in terms of performance.
	panic(fmt.Sprintf("Contract %s trying to access state of %s which is not a registered dependency", executingContract.ContractAddr, targetContractAddr))
}
