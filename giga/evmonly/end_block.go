package evmonly

import (
	"bytes"
	"cmp"
	"fmt"
	"math/big"
	"slices"
	"unicode/utf8"

	"github.com/ethereum/go-ethereum/common"
	"github.com/sei-protocol/seilog"

	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles"
)

var endBlockLogger = seilog.NewLogger("giga", "evmonly", "endblock")

// maxFailureLength is the most bytes of an end block failure's message logged.
const maxFailureLength = 1024

// endBlocker is a registered custom precompile that runs at the end of every block.
type endBlocker struct {
	address  common.Address
	contract precompiles.EndBlocker
}

// customEndBlockers resolves the registry's EndBlockers in run order. It panics
// when a listed address is not a registered EndBlocker, or a registered
// EndBlocker is not listed.
func customEndBlockers(registry precompiles.Registry) []endBlocker {
	if registry == nil {
		return nil
	}
	order := registry.EndBlockers()
	var blockers []endBlocker
	for _, addr := range order {
		contract, _ := registry.Get(addr)
		blocker, ok := contract.(precompiles.EndBlocker)
		if !ok {
			panic(fmt.Sprintf("custom precompile %s is listed as an end blocker but is not one", addr))
		}
		if slices.ContainsFunc(blockers, func(b endBlocker) bool { return b.address == addr }) {
			panic(fmt.Sprintf("custom precompile %s is listed as an end blocker twice", addr))
		}
		blockers = append(blockers, endBlocker{address: addr, contract: blocker})
	}
	for _, addr := range registry.Addresses() {
		contract, _ := registry.Get(addr)
		if _, ok := contract.(precompiles.EndBlocker); ok && !slices.Contains(order, addr) {
			panic(fmt.Sprintf("custom precompile %s has EndBlock but is not listed as an end blocker", addr))
		}
	}
	return blockers
}

// runEndBlockers runs every end blocker against the state the block's
// transactions produced and folds their writes into the block's changeset.
func (e *Executor) runEndBlockers(block BlockContext, source StateReader, result *BlockResult) error {
	if len(e.endBlockers) == 0 {
		return nil
	}
	stateDB := e.acquireStateDB(newPendingOverlay(source, &result.ChangeSet))
	defer e.releaseStateDB(stateDB)
	blockCtx := endBlockContext(block, e.chainConfig(block).ChainID)
	for _, blocker := range e.endBlockers {
		state := &precompileState{db: stateDB}
		failures, err := blocker.contract.EndBlock(blockCtx, state)
		logEndBlockFailures(block.Number, blocker.address, failures)
		if err != nil {
			return fmt.Errorf("end block %d for custom precompile %s: %w", block.Number, blocker.address, err)
		}
		if state.err != nil {
			return fmt.Errorf("end block %d for custom precompile %s: %w", block.Number, blocker.address, state.err)
		}
		if err := stateDB.Error(); err != nil {
			return fmt.Errorf("end block %d for custom precompile %s: %w", block.Number, blocker.address, err)
		}
		endBlockLogger.Debug("custom precompile end block", "height", block.Number, "address", blocker.address, "storage_gas", state.spent)
	}
	stateDB.Finalise(true)
	var changes StateChangeSet
	stateDB.ChangeSetInto(&changes)
	mergeChangeSet(&result.ChangeSet, changes)
	return nil
}

// logEndBlockFailures logs each of an end blocker's failures, its message cut
// to maxFailureLength bytes.
func logEndBlockFailures(height uint64, address common.Address, failures []error) {
	for _, failure := range failures {
		endBlockLogger.Info("custom precompile end block failure", "height", height, "address", address, "error", truncateUTF8(failure.Error(), maxFailureLength))
	}
}

// truncateUTF8 returns at most n bytes of s, cut on a UTF-8 boundary.
func truncateUTF8(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n]
}

func endBlockContext(block BlockContext, chainID *big.Int) precompiles.BlockContext {
	return precompiles.BlockContext{
		Number:      block.Number,
		UnixNano:    unixNano(block.Time),
		ChainID:     cloneOptionalBig(chainID),
		BaseFee:     cloneOptionalBig(block.BaseFee),
		BlobBaseFee: cloneOptionalBig(block.BlobBaseFee),
		Coinbase:    block.Coinbase,
		PrevRandao:  block.PrevRandao,
	}
}

// mergeChangeSet applies later, a changeset computed on top of dst, to dst. An
// entry of later replaces dst's value for the same key but keeps dst's prior,
// which is the value before the block. Every list stays sorted by key.
func mergeChangeSet(dst *StateChangeSet, later StateChangeSet) {
	for _, change := range later.Accounts {
		dst.Accounts = upsertSorted(dst.Accounts, change, compareAddress[AccountChange](func(c AccountChange) common.Address { return c.Address }),
			func(c *AccountChange, prior AccountChange) { c.Prior = prior.Prior })
	}
	for _, change := range later.Storage {
		dst.Storage = upsertSorted(dst.Storage, change, compareStorageChange,
			func(c *StorageChange, prior StorageChange) { c.PriorValue = prior.PriorValue })
	}
}

// compareAddress orders changes by the address addr reads from them.
func compareAddress[T any](addr func(T) common.Address) func(T, T) int {
	return func(a, b T) int {
		x, y := addr(a), addr(b)
		return bytes.Compare(x[:], y[:])
	}
}

// compareStorageChange orders storage changes by address, then slot.
func compareStorageChange(a, b StorageChange) int {
	return cmp.Or(bytes.Compare(a.Address[:], b.Address[:]), bytes.Compare(a.Key[:], b.Key[:]))
}

// upsertSorted replaces list's entry that compares equal to item, keeping that
// entry's prior via keepPrior, or inserts item in compare order.
func upsertSorted[T any](list []T, item T, compare func(T, T) int, keepPrior func(*T, T)) []T {
	i, found := slices.BinarySearchFunc(list, item, compare)
	if found {
		keepPrior(&item, list[i])
		list[i] = item
		return list
	}
	return slices.Insert(list, i, item)
}
