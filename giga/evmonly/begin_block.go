package evmonly

import (
	"errors"
	"fmt"
	"slices"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/params"
	"github.com/sei-protocol/seilog"

	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles"
)

var beginBlockLogger = seilog.NewLogger("giga", "evmonly", "beginblock")

// beginBlocker is a registered custom precompile that runs at the start of every block.
type beginBlocker struct {
	address  common.Address
	contract precompiles.BeginBlocker
}

// customBeginBlockers resolves the registry's BeginBlockers in run order. It
// panics when a listed address is not a registered BeginBlocker, or a
// registered BeginBlocker is not listed.
func customBeginBlockers(registry precompiles.Registry) []beginBlocker {
	if registry == nil {
		return nil
	}
	order := registry.BeginBlockers()
	var blockers []beginBlocker
	for _, addr := range order {
		contract, _ := registry.Get(addr)
		blocker, ok := contract.(precompiles.BeginBlocker)
		if !ok {
			panic(fmt.Sprintf("custom precompile %s is listed as a begin blocker but is not one", addr))
		}
		if slices.ContainsFunc(blockers, func(b beginBlocker) bool { return b.address == addr }) {
			panic(fmt.Sprintf("custom precompile %s is listed as a begin blocker twice", addr))
		}
		blockers = append(blockers, beginBlocker{address: addr, contract: blocker})
	}
	for _, addr := range registry.Addresses() {
		contract, _ := registry.Get(addr)
		if _, ok := contract.(precompiles.BeginBlocker); ok && !slices.Contains(order, addr) {
			panic(fmt.Sprintf("custom precompile %s has BeginBlock but is not listed as a begin blocker", addr))
		}
	}
	return blockers
}

// runBeginBlockers credits the genesis alloc at its height, then runs every
// begin blocker against source, the state before the block, and returns the
// changes they made. replay marks a run that only rebuilds the block's state.
func (e *Executor) runBeginBlockers(block BlockContext, source StateReader, replay bool) (StateChangeSet, error) {
	genesis := e.genesis.appliesTo(block.Number)
	if len(e.beginBlockers) == 0 && !genesis {
		return StateChangeSet{}, nil
	}
	stateDB := e.acquireStateDB(source)
	defer e.releaseStateDB(stateDB)
	if genesis {
		e.genesis.credit(stateDB)
	}
	blockCtx := endBlockContext(block, e.chainConfig(block).ChainID)
	blockCtx.Replay = replay
	for _, blocker := range e.beginBlockers {
		state := &precompileState{db: stateDB}
		failures, err := blocker.contract.BeginBlock(blockCtx, state)
		logBeginBlockFailures(block.Number, blocker.address, failures)
		if err != nil {
			return StateChangeSet{}, fmt.Errorf("begin block %d for custom precompile %s: %w", block.Number, blocker.address, err)
		}
		if state.err != nil {
			return StateChangeSet{}, fmt.Errorf("begin block %d for custom precompile %s: %w", block.Number, blocker.address, state.err)
		}
		if err := stateDB.Error(); err != nil {
			return StateChangeSet{}, fmt.Errorf("begin block %d for custom precompile %s: %w", block.Number, blocker.address, err)
		}
		beginBlockLogger.Debug("custom precompile begin block", "height", block.Number, "address", blocker.address, "storage_gas", state.spent)
	}
	stateDB.Finalise(params.Rules{IsEIP158: true})
	var changes StateChangeSet
	stateDB.ChangeSetInto(&changes)
	return changes, nil
}

// logBeginBlockFailures logs each of a begin blocker's failures, its message
// cut to maxFailureLength bytes: a failure over stored data that does not add
// up as an error, any other at info.
func logBeginBlockFailures(height uint64, address common.Address, failures []error) {
	for _, failure := range failures {
		msg := truncateUTF8(failure.Error(), maxFailureLength)
		if errors.Is(failure, precompiles.ErrDegraded) {
			beginBlockLogger.Error("custom precompile begin block failure over stored data", "height", height, "address", address, "error", msg)
			continue
		}
		beginBlockLogger.Info("custom precompile begin block failure", "height", height, "address", address, "error", msg)
	}
}

// mergeEarlierChangeSet applies earlier, a changeset computed before dst, to
// dst. An entry of dst keeps its value but takes earlier's prior for the same
// key, which is the value before the block; an entry only in earlier is
// inserted. Every list stays sorted by key.
func mergeEarlierChangeSet(dst *StateChangeSet, earlier StateChangeSet) {
	for _, change := range earlier.Accounts {
		dst.Accounts = keepEarliestPrior(dst.Accounts, change, compareAddress[AccountChange](func(c AccountChange) common.Address { return c.Address }),
			func(c *AccountChange, earliest AccountChange) { c.Prior = earliest.Prior })
	}
	for _, change := range earlier.Storage {
		dst.Storage = keepEarliestPrior(dst.Storage, change, compareStorageChange,
			func(c *StorageChange, earliest StorageChange) { c.PriorValue = earliest.PriorValue })
	}
}

// keepEarliestPrior gives list's entry that compares equal to earliest
// earliest's prior via setPrior, or inserts earliest in compare order.
func keepEarliestPrior[T any](list []T, earliest T, compare func(T, T) int, setPrior func(*T, T)) []T {
	i, found := slices.BinarySearchFunc(list, earliest, compare)
	if found {
		setPrior(&list[i], earliest)
		return list
	}
	return slices.Insert(list, i, earliest)
}
