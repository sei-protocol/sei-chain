package evmonly

import (
	"bytes"
	"errors"
	"fmt"
	"math/big"
	"slices"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/vm"

	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles"
)

var errEndBlockClearedStorage = errors.New("end-block state changes may not clear storage")

// blockHook is a registered custom precompile that implements the block hook T.
type blockHook[T any] struct {
	address  common.Address
	contract T
}

// endBlocker is a registered custom precompile that runs at the end of every block.
type endBlocker = blockHook[precompiles.EndBlocker]

// beginBlocker is a registered custom precompile that checks every block before it runs.
type beginBlocker = blockHook[precompiles.BeginBlocker]

// customEndBlockers returns the registry's contracts that implement
// precompiles.EndBlocker, in address order.
func customEndBlockers(registry precompiles.Registry) []endBlocker {
	return customBlockHooks[precompiles.EndBlocker](registry)
}

// customBeginBlockers returns the registry's contracts that implement
// precompiles.BeginBlocker, in address order.
func customBeginBlockers(registry precompiles.Registry) []beginBlocker {
	return customBlockHooks[precompiles.BeginBlocker](registry)
}

// customBlockHooks returns the registry's contracts that implement T, in
// address order. A contract at a go-ethereum builtin precompile address is
// skipped, as it is never reached by a call either.
func customBlockHooks[T any](registry precompiles.Registry) []blockHook[T] {
	if registry == nil {
		return nil
	}
	var hooks []blockHook[T]
	for _, addr := range registry.Addresses() {
		if slices.Contains(vm.PrecompiledAddressesPrague, addr) {
			continue
		}
		contract, ok := registry.Get(addr)
		if !ok || contract == nil {
			continue
		}
		if hook, ok := contract.(T); ok {
			hooks = append(hooks, blockHook[T]{address: addr, contract: hook})
		}
	}
	slices.SortFunc(hooks, func(a, b blockHook[T]) int {
		return bytes.Compare(a.address[:], b.address[:])
	})
	return slices.CompactFunc(hooks, func(a, b blockHook[T]) bool { return a.address == b.address })
}

// runBeginBlockers runs every begin blocker against the state the block starts from.
func (e *Executor) runBeginBlockers(block BlockContext, source StateReader) error {
	if len(e.beginBlockers) == 0 {
		return nil
	}
	blockCtx := hookBlockContext(block, e.chainConfig(block).ChainID)
	for _, blocker := range e.beginBlockers {
		if err := blocker.contract.BeginBlock(blockCtx, source); err != nil {
			return fmt.Errorf("begin block %d for custom precompile %s: %w", block.Number, blocker.address, err)
		}
	}
	return nil
}

// runEndBlockers runs every end blocker against the state the block's
// transactions produced and folds their writes into the block's changeset.
func (e *Executor) runEndBlockers(block BlockContext, source StateReader, result *BlockResult) error {
	if len(e.endBlockers) == 0 {
		return nil
	}
	stateDB := e.acquireStateDB(newPendingOverlay(source, &result.ChangeSet))
	defer e.releaseStateDB(stateDB)
	blockCtx := hookBlockContext(block, e.chainConfig(block).ChainID)
	for _, blocker := range e.endBlockers {
		// A storage-only account would be recreated, dropping its storage, by the next call to it.
		materializeAccount(stateDB, blocker.address)
		state := &precompileState{db: stateDB}
		if err := blocker.contract.EndBlock(blockCtx, state); err != nil {
			return fmt.Errorf("end block %d for custom precompile %s: %w", block.Number, blocker.address, err)
		}
		if state.err != nil {
			return fmt.Errorf("end block %d for custom precompile %s: %w", block.Number, blocker.address, state.err)
		}
		if err := stateDB.Error(); err != nil {
			return fmt.Errorf("end block %d for custom precompile %s: %w", block.Number, blocker.address, err)
		}
	}
	stateDB.Finalise(true)
	var changes StateChangeSet
	stateDB.ChangeSetInto(&changes)
	return mergeChangeSet(&result.ChangeSet, changes)
}

// hookBlockContext is the block a custom precompile hook runs in.
func hookBlockContext(block BlockContext, chainID *big.Int) precompiles.BlockContext {
	return precompiles.BlockContext{
		Number:      block.Number,
		Time:        block.Time,
		ChainID:     cloneOptionalBig(chainID),
		BaseFee:     cloneOptionalBig(block.BaseFee),
		BlobBaseFee: cloneOptionalBig(block.BlobBaseFee),
		Coinbase:    block.Coinbase,
		PrevRandao:  block.PrevRandao,
	}
}

// mergeChangeSet applies later, a changeset computed on top of dst, to dst. Each
// entry of later replaces dst's entry for the same key or is added, and every
// list stays sorted by key.
func mergeChangeSet(dst *StateChangeSet, later StateChangeSet) error {
	if len(later.StorageClears) > 0 {
		return errEndBlockClearedStorage
	}
	for _, change := range later.Balances {
		dst.Balances = upsertSorted(dst.Balances, change, func(c BalanceChange) []byte { return c.Address[:] })
	}
	for _, change := range later.Nonces {
		dst.Nonces = upsertSorted(dst.Nonces, change, func(c NonceChange) []byte { return c.Address[:] })
	}
	for _, change := range later.Code {
		dst.Code = upsertSorted(dst.Code, change, func(c CodeChange) []byte { return c.Address[:] })
	}
	for _, change := range later.Storage {
		dst.Storage = upsertSorted(dst.Storage, change, storageChangeSortKey)
	}
	return nil
}

func storageChangeSortKey(c StorageChange) []byte {
	key := make([]byte, 0, common.AddressLength+common.HashLength)
	key = append(key, c.Address[:]...)
	return append(key, c.Key[:]...)
}

func upsertSorted[T any](list []T, item T, key func(T) []byte) []T {
	want := key(item)
	i, found := slices.BinarySearchFunc(list, want, func(existing T, target []byte) int {
		return bytes.Compare(key(existing), target)
	})
	if found {
		list[i] = item
		return list
	}
	return slices.Insert(list, i, item)
}
