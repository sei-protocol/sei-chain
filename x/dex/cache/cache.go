package dex

import (
	"fmt"
	"time"

	"github.com/cosmos/cosmos-sdk/store/prefix"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/sei-protocol/sei-chain/utils/datastructures"
	"github.com/sei-protocol/sei-chain/x/dex/types"
	typesutils "github.com/sei-protocol/sei-chain/x/dex/types/utils"
)

const SynchronizationTimeoutInSeconds = 5

type MemState struct {
	storeKey    sdk.StoreKey
	depositInfo *datastructures.TypedSyncMap[typesutils.ContractAddress, *DepositInfo]
}

func NewMemState(storeKey sdk.StoreKey) *MemState {
	return &MemState{
		storeKey:    storeKey,
		depositInfo: datastructures.NewTypedSyncMap[typesutils.ContractAddress, *DepositInfo](),
	}
}

func (s *MemState) GetAllBlockOrders(ctx sdk.Context, contractAddr typesutils.ContractAddress) (list []*types.Order) {
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

func (s *MemState) GetBlockOrders(ctx sdk.Context, contractAddr typesutils.ContractAddress, pair typesutils.PairString) *BlockOrders {
	s.SynchronizeAccess(ctx, contractAddr)
	return NewOrders(
		prefix.NewStore(
			ctx.KVStore(s.storeKey),
			types.MemOrderPrefixForPair(
				string(contractAddr), string(pair),
			),
		),
	)
}

func (s *MemState) GetBlockCancels(ctx sdk.Context, contractAddr typesutils.ContractAddress, pair typesutils.PairString) *BlockCancellations {
	s.SynchronizeAccess(ctx, contractAddr)
	return NewCancels(
		prefix.NewStore(
			ctx.KVStore(s.storeKey),
			types.MemCancelPrefixForPair(
				string(contractAddr), string(pair),
			),
		),
	)
}

func (s *MemState) GetDepositInfo(ctx sdk.Context, contractAddr typesutils.ContractAddress) *DepositInfo {
	s.SynchronizeAccess(ctx, contractAddr)
	return NewDepositInfo(
		prefix.NewStore(
			ctx.KVStore(s.storeKey),
			types.MemDepositPrefix(string(contractAddr)),
		),
	)
}

func (s *MemState) Clear(ctx sdk.Context) {
	DeepDelete(ctx.KVStore(s.storeKey), types.KeyPrefix(types.MemOrderKey), func(_ []byte) bool { return true })
	DeepDelete(ctx.KVStore(s.storeKey), types.KeyPrefix(types.MemCancelKey), func(_ []byte) bool { return true })
	DeepDelete(ctx.KVStore(s.storeKey), types.KeyPrefix(types.MemDepositKey), func(_ []byte) bool { return true })
}

func (s *MemState) ClearCancellationForPair(ctx sdk.Context, contractAddr typesutils.ContractAddress, pair typesutils.PairString) {
	s.SynchronizeAccess(ctx, contractAddr)
	DeepDelete(ctx.KVStore(s.storeKey), types.KeyPrefix(types.MemCancelKey), func(v []byte) bool {
		var c types.Cancellation
		if err := c.Unmarshal(v); err != nil {
			panic(err)
		}
		return c.ContractAddr == string(contractAddr) && typesutils.GetPairString(&types.Pair{
			AssetDenom: c.AssetDenom,
			PriceDenom: c.PriceDenom,
		}) == pair
	})
}

func (s *MemState) DeepCopy() *MemState {
	copy := NewMemState(s.storeKey)
	return copy
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

func DeepDelete(kvStore sdk.KVStore, storePrefix []byte, matcher func([]byte) bool) {
	store := prefix.NewStore(
		kvStore,
		storePrefix,
	)
	iterator := sdk.KVStorePrefixIterator(store, []byte{})

	defer iterator.Close()

	keysToDelete := [][]byte{}
	for ; iterator.Valid(); iterator.Next() {
		if matcher(iterator.Value()) {
			keysToDelete = append(keysToDelete, iterator.Key())
		}
	}
	for _, keyToDelete := range keysToDelete {
		store.Delete(keyToDelete)
	}
}
