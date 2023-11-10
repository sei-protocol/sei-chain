package dex

import (
	"fmt"
	"sync"
	"time"

	"github.com/cosmos/cosmos-sdk/store/prefix"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/sei-protocol/sei-chain/utils/datastructures"
	"github.com/sei-protocol/sei-chain/x/dex/types"
)

const SynchronizationTimeoutInSeconds = 5

type MemState struct {
	storeKey sdk.StoreKey

	// TODO: all of these need to be in the memstore
	contractsToProcessMtx *sync.RWMutex
	contractsToDepsMtx    *sync.Mutex
}

func NewMemState(storeKey sdk.StoreKey) *MemState {
	return &MemState{
		storeKey:              storeKey,
		contractsToDepsMtx:    &sync.Mutex{},
		contractsToProcessMtx: &sync.RWMutex{},
	}
}

func (s *MemState) GetAllBlockOrders(ctx sdk.Context, contractAddr types.ContractAddress) (list []*types.Order) {
	s.SynchronizeAccess(ctx, contractAddr)
	store := prefix.NewStore(
		ctx.KVStore(s.storeKey),
		types.MemOrderPrefix(
			string(contractAddr),
		),
	)
	iterator := sdk.KVStorePrefixIterator(store, []byte{})

	defer iterator.Close()

	for ; iterator.Valid(); iterator.Next() {
		var val types.Order
		if err := val.Unmarshal(iterator.Value()); err != nil {
			panic(err)
		}
		list = append(list, &val)
	}
	return
}

func (s *MemState) GetBlockOrders(ctx sdk.Context, contractAddr types.ContractAddress, pair types.Pair) *BlockOrders {
	s.SynchronizeAccess(ctx, contractAddr)
	return NewOrders(
		prefix.NewStore(
			ctx.KVStore(s.storeKey),
			types.MemOrderPrefixForPair(
				string(contractAddr), pair.PriceDenom, pair.AssetDenom,
			),
		),
	)
}

func (s *MemState) GetBlockCancels(ctx sdk.Context, contractAddr types.ContractAddress, pair types.Pair) *BlockCancellations {
	s.SynchronizeAccess(ctx, contractAddr)
	return NewCancels(
		prefix.NewStore(
			ctx.KVStore(s.storeKey),
			types.MemCancelPrefixForPair(
				string(contractAddr), pair.PriceDenom, pair.AssetDenom,
			),
		),
	)
}

func (s *MemState) GetDepositInfo(ctx sdk.Context, contractAddr types.ContractAddress) *DepositInfo {
	s.SynchronizeAccess(ctx, contractAddr)
	return NewDepositInfo(
		prefix.NewStore(
			ctx.KVStore(s.storeKey),
			types.MemDepositPrefix(string(contractAddr)),
		),
	)
}

func (s *MemState) GetContractToDependencies(ctx sdk.Context, contractAddress string, loader func(sdk.Context, string) (types.ContractInfoV2, error)) []string {
	s.contractsToDepsMtx.Lock()
	defer s.contractsToDepsMtx.Unlock()

	store := ctx.KVStore(s.storeKey)

	// GET from memstate
	bz := store.Get(types.MemDownstreamContractsKey(contractAddress))
	if bz != nil {
		var contracts types.DownsteamContracts
		if err := contracts.Unmarshal(bz); err != nil {
			panic(err)
		}
		return contracts.ContractAddrs
	}
	loadedDownstreams := GetAllDownstreamContracts(ctx, contractAddress, loader)
	// if we have to get all downstreams, save them to memstate
	downstreamContracts := types.DownsteamContracts{
		ContractAddrs: loadedDownstreams,
	}
	contractsBz, err := downstreamContracts.Marshal()
	if err != nil {
		panic(err)
	}
	store.Set(types.MemDownstreamContractsKey(contractAddress), contractsBz)
	return loadedDownstreams
}

func (s *MemState) ClearContractToDependencies(ctx sdk.Context) {
	s.contractsToDepsMtx.Lock()
	defer s.contractsToDepsMtx.Unlock()
	DeepDelete(ctx.KVStore(s.storeKey), types.KeyPrefix(types.MemDownstreamContracts), func(_ []byte) bool { return true })
}

func (s *MemState) SetDownstreamsToProcess(ctx sdk.Context, contractAddress string, loader func(sdk.Context, string) (types.ContractInfoV2, error)) {
	s.contractsToProcessMtx.Lock()
	defer s.contractsToProcessMtx.Unlock()
	contracts := s.GetContractToDependencies(ctx, contractAddress, loader)
	store := ctx.KVStore(s.storeKey)
	for _, contract := range contracts {
		// Add each to memstate instead - simply set to 1 for some value indicating presence
		store.Set(types.MemContractsToProcessKey(contract), []byte{1})
	}
}

func (s *MemState) ContractsToProcessContains(ctx sdk.Context, contractAddress string) bool {
	s.contractsToProcessMtx.RLock()
	defer s.contractsToProcessMtx.RUnlock()
	store := ctx.KVStore(s.storeKey)
	return store.Has(types.MemContractsToProcessKey(contractAddress))
}

func (s *MemState) GetContractToProcessOrderedSlice(ctx sdk.Context) []string {
	s.contractsToProcessMtx.RLock()
	defer s.contractsToProcessMtx.RUnlock()
	orderedContracts := []string{}
	store := ctx.KVStore(s.storeKey)
	iter := sdk.KVStorePrefixIterator(store, types.KeyPrefix(types.MemContractsToProcess))
	for ; iter.Valid(); iter.Next() {
		// get key
		key := iter.Key()
		// because we know length prefix is 1 byte, and the rest of the key is ONLY the contract addr, this lets us get the contract address bytes
		contractAddrBytes := key[len(types.KeyPrefix(types.MemContractsToProcess))+1:]
		// parse contract address from key
		contractAddr := sdk.AccAddress(contractAddrBytes).String()
		orderedContracts = append(orderedContracts, contractAddr)
	}
	return orderedContracts
}

func (s *MemState) Clear(ctx sdk.Context) {
	s.contractsToProcessMtx.Lock()
	defer s.contractsToProcessMtx.Unlock()
	DeepDelete(ctx.KVStore(s.storeKey), types.KeyPrefix(types.MemOrderKey), func(_ []byte) bool { return true })
	DeepDelete(ctx.KVStore(s.storeKey), types.KeyPrefix(types.MemCancelKey), func(_ []byte) bool { return true })
	DeepDelete(ctx.KVStore(s.storeKey), types.KeyPrefix(types.MemDepositKey), func(_ []byte) bool { return true })
	DeepDelete(ctx.KVStore(s.storeKey), types.KeyPrefix(types.MemContractsToProcess), func(_ []byte) bool { return true })
}

func (s *MemState) ClearCancellationForPair(ctx sdk.Context, contractAddr types.ContractAddress, pair types.Pair) {
	s.SynchronizeAccess(ctx, contractAddr)
	DeepDelete(ctx.KVStore(s.storeKey), types.KeyPrefix(types.MemCancelKey), func(v []byte) bool {
		var c types.Cancellation
		if err := c.Unmarshal(v); err != nil {
			panic(err)
		}
		return c.ContractAddr == string(contractAddr) && c.PriceDenom == pair.PriceDenom && c.AssetDenom == pair.AssetDenom
	})
}

func (s *MemState) DeepCopy() *MemState {
	return &MemState{
		storeKey:              s.storeKey,
		contractsToDepsMtx:    s.contractsToDepsMtx,    // passing by pointer
		contractsToProcessMtx: s.contractsToProcessMtx, // passing by pointer
	}
}

func (s *MemState) DeepFilterAccount(ctx sdk.Context, account string) {
	DeepDelete(ctx.KVStore(s.storeKey), types.KeyPrefix(types.MemOrderKey), func(v []byte) bool {
		var o types.Order
		if err := o.Unmarshal(v); err != nil {
			panic(err)
		}
		return o.Account == account
	})
	DeepDelete(ctx.KVStore(s.storeKey), types.KeyPrefix(types.MemCancelKey), func(v []byte) bool {
		var c types.Cancellation
		if err := c.Unmarshal(v); err != nil {
			panic(err)
		}
		return c.Creator == account
	})
	DeepDelete(ctx.KVStore(s.storeKey), types.KeyPrefix(types.MemDepositKey), func(v []byte) bool {
		var d types.DepositInfoEntry
		if err := d.Unmarshal(v); err != nil {
			panic(err)
		}
		return d.Creator == account
	})
}

func (s *MemState) SynchronizeAccess(ctx sdk.Context, contractAddr types.ContractAddress) {
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

func DeepDelete(kvStore sdk.KVStore, storePrefix []byte, matcher func([]byte) bool) {
	store := prefix.NewStore(
		kvStore,
		storePrefix,
	)
	// Getting all KVs first before applying `matcher` in case `matcher` contains
	// store read/write logics.
	// Wrapping getter into its own function to make sure iterator is always closed
	// before `matcher` logic runs.
	keyValuesGetter := func() ([][]byte, [][]byte) {
		iterator := sdk.KVStorePrefixIterator(store, []byte{})
		defer iterator.Close()
		keys, values := [][]byte{}, [][]byte{}
		for ; iterator.Valid(); iterator.Next() {
			keys = append(keys, iterator.Key())
			values = append(values, iterator.Value())
		}
		return keys, values
	}
	keys, values := keyValuesGetter()
	for i, key := range keys {
		if matcher(values[i]) {
			store.Delete(key)
		}
	}
}

// BFS traversal over a acyclic graph
// Includes the root contract itself.
func GetAllDownstreamContracts(ctx sdk.Context, contractAddress string, loader func(sdk.Context, string) (types.ContractInfoV2, error)) []string {
	res := []string{contractAddress}
	seen := datastructures.NewSyncSet(res)
	downstreams := []*types.ContractInfoV2{}
	populater := func(target *types.ContractInfoV2) {
		for _, dep := range target.Dependencies {
			if downstream, err := loader(ctx, dep.Dependency); err == nil && !seen.Contains(downstream.ContractAddr) {
				if !downstream.Suspended {
					downstreams = append(downstreams, &downstream)
					seen.Add(downstream.ContractAddr)
				}
			} else {
				// either getting the dependency returned an error, or there is a cycle in the graph. Either way
				// is bad and should cause the triggering tx to fail
				panic(fmt.Sprintf("getting dependency %s for %s returned an error, or there is a cycle in the dependency graph", dep.Dependency, target.ContractAddr))
			}
		}
	}
	// init first layer downstreams
	if contract, err := loader(ctx, contractAddress); err == nil {
		populater(&contract)
	} else {
		return res
	}

	for len(downstreams) > 0 {
		downstream := downstreams[0]
		res = append(res, downstream.ContractAddr)
		populater(downstream)
		downstreams = downstreams[1:]
	}
	return res
}
