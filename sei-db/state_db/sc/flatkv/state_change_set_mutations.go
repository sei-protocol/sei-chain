package flatkv

import (
	"errors"
	"fmt"
	"math/big"
	"runtime"

	"github.com/ethereum/go-ethereum/crypto"
	"golang.org/x/sync/errgroup"

	"github.com/sei-protocol/sei-chain/sei-db/common/keys"
	gigatypes "github.com/sei-protocol/sei-chain/sei-db/state_db/giga/types"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/giga/types/mutation"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/config"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/ktype"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/vtype"
)

// The length of the physical key of an account or code row: "evm/", the kind prefix byte, then the address.
const physicalAddressKeyLen = len(keys.EVMStoreKey) + 2 + keys.AddressLen

// The length of the physical key of a storage row: "evm/", the kind prefix byte, the address, then the slot.
const physicalStorageKeyLen = physicalAddressKeyLen + vtype.SlotLen

// stateChangeSetConverter converts one block's state change set into mutations.
type stateChangeSetConverter struct {
	// The block's state changes.
	changes *gigatypes.StateChangeSet
	// The block the changes belong to.
	blockHeight uint64
	// The state as of the block before blockHeight.
	priorState gigatypes.StateView
	// The number of entries one task converts at a time.
	chunkSize int
	// The number of tasks that run at once.
	workers int
	// Index i is the position in mutations of account change i's first mutation. An account change produces a
	// varying number of mutations: none if nothing changed, its account row if its balance or nonce changed, and
	// its account row plus its code row if its code changed.
	accountOffsets []int
	// The number of mutations the account changes produce. The storage mutations start at this position.
	accountMutationCount int
	// The output, one mutation per physical key the block writes.
	mutations []mutation.Mutation
	// The key of each mutation, in the form BatchGetRaw() takes.
	physicalKeys [][]byte
	// The memory every mutation's key is carved from.
	keySlab []byte
}

// MutationsFromStateChangeSet converts one block's state change set into one mutation per physical key the block
// writes. An account change produces an account row mutation, plus a code row mutation when its code changed, and
// produces nothing when nothing changed. Only the account fields that changed are written over the account's prior
// row. The returned mutations must not be modified.
func MutationsFromStateChangeSet(
	// The block's state changes. Must not be modified during the call.
	changes *gigatypes.StateChangeSet,
	// The block the changes belong to. Every new value is stamped with it.
	blockHeight uint64,
	// The state as of the block before blockHeight. Supplies each mutation's previous value.
	priorState gigatypes.StateView,
	// Supplies the chunk size and worker count the conversion runs with.
	cfg *config.Config,
) ([]mutation.Mutation, error) {
	priorHeight := priorState.GetBlockHeight()
	//nolint:gosec // a non-negative height converts exactly
	if priorHeight < 0 || uint64(priorHeight)+1 != blockHeight {
		return nil, fmt.Errorf("prior state is at block %d, but block %d needs the state of the block before",
			priorHeight, blockHeight)
	}

	c := newStateChangeSetConverter(changes, blockHeight, priorState, cfg)
	if err := c.convertChunks(); err != nil {
		return nil, err
	}
	return c.mutations, nil
}

// newStateChangeSetConverter lays out where each entry of changes lands in the mutations and the key slab.
func newStateChangeSetConverter(
	changes *gigatypes.StateChangeSet,
	blockHeight uint64,
	priorState gigatypes.StateView,
	cfg *config.Config,
) *stateChangeSetConverter {
	accountOffsets := make([]int, len(changes.Accounts))
	count := 0
	for i := range changes.Accounts {
		accountOffsets[i] = count
		count += accountChangeMutationCount(&changes.Accounts[i])
	}

	keyBytes := count*physicalAddressKeyLen + len(changes.Storage)*physicalStorageKeyLen
	for i := range changes.Misc {
		keyBytes += len(changes.Misc[i].Module) + 1 + len(changes.Misc[i].Key)
	}
	mutationCount := count + len(changes.Storage) + len(changes.Misc)
	return &stateChangeSetConverter{
		changes:              changes,
		blockHeight:          blockHeight,
		priorState:           priorState,
		chunkSize:            cfg.StateChangeSetChunkSize,
		workers:              stateChangeSetWorkerCount(cfg, runtime.NumCPU()),
		accountOffsets:       accountOffsets,
		accountMutationCount: count,
		mutations:            make([]mutation.Mutation, mutationCount),
		physicalKeys:         make([][]byte, mutationCount),
		keySlab:              make([]byte, keyBytes),
	}
}

// accountChangeMutationCount returns the number of mutations an account change produces.
func accountChangeMutationCount(change *gigatypes.AccountChange) int {
	switch {
	case change.CodeChanged():
		return 2
	case change.BalanceChanged() || change.NonceChanged():
		return 1
	default:
		return 0
	}
}

// convertChunks converts every entry, a chunk of entries per task, running the tasks concurrently.
func (c *stateChangeSetConverter) convertChunks() error {
	var group errgroup.Group
	group.SetLimit(c.workers)
	// The tasks share no state to coordinate: each writes only its own entries' mutations and key slab ranges.
	group.Go(c.convertMisc)
	for start := 0; start < len(c.changes.Accounts); start += c.chunkSize {
		end := min(start+c.chunkSize, len(c.changes.Accounts))
		group.Go(func() error { return c.convertAccounts(start, end) })
	}
	for start := 0; start < len(c.changes.Storage); start += c.chunkSize {
		end := min(start+c.chunkSize, len(c.changes.Storage))
		group.Go(func() error { return c.convertStorage(start, end) })
	}
	if err := group.Wait(); err != nil {
		return fmt.Errorf("convert state changes: %w", err)
	}
	return nil
}

// firstStorageMutation returns the index of the first storage mutation.
func (c *stateChangeSetConverter) firstStorageMutation() int {
	return c.accountMutationCount
}

// firstMiscMutation returns the index of the first misc mutation.
func (c *stateChangeSetConverter) firstMiscMutation() int {
	return c.firstStorageMutation() + len(c.changes.Storage)
}

// readPriorValues returns the prior rows of the mutations in [first, last), whose keys are already written.
func (c *stateChangeSetConverter) readPriorValues(first int, last int) map[string][]byte {
	if first == last {
		return nil
	}
	return c.priorState.BatchGetRaw(c.physicalKeys[first:last])
}

// convertMisc converts every misc change. There are few, so they take one task.
func (c *stateChangeSetConverter) convertMisc() error {
	first := c.firstMiscMutation()
	// Misc keys vary in length, so they follow every fixed-length key in the slab.
	offset := c.firstStorageMutation()*physicalAddressKeyLen + len(c.changes.Storage)*physicalStorageKeyLen
	for i := range c.changes.Misc {
		change := &c.changes.Misc[i]
		if change.Module == keys.EVMStoreKey {
			return fmt.Errorf("misc change %d writes key %x in the EVM module", i, change.Key)
		}
		end := offset + len(change.Module) + 1 + len(change.Key)
		key := ktype.WritePhysicalKeyToBuffer(c.keySlab[offset:offset:end], change.Module, change.Key)
		c.mutations[first+i].Key = key
		c.physicalKeys[first+i] = key
		offset = end
	}

	priorValues := c.readPriorValues(first, first+len(c.changes.Misc))
	height := int64(c.blockHeight) //nolint:gosec // block heights fit in an int64
	for i := range c.changes.Misc {
		change := &c.changes.Misc[i]
		m := &c.mutations[first+i]
		m.PreviousValue = priorValues[string(m.Key)]
		if !change.Delete {
			m.NewValue = vtype.SerializeMisc(height, change.Value)
		}
	}
	return nil
}

// convertAccounts converts the account changes in [start, end).
func (c *stateChangeSetConverter) convertAccounts(start int, end int) error {
	chunkEnd := c.accountOffsets[start]
	for i := start; i < end; i++ {
		address := c.changes.Accounts[i].Address
		first := c.accountOffsets[i]
		accountEnd := first + accountChangeMutationCount(&c.changes.Accounts[i])
		for m := first; m < accountEnd; m++ {
			kind := ktype.EVMKeyAccount
			if m > first {
				kind = keys.EVMKeyCode
			}
			keyStart := m * physicalAddressKeyLen
			buf := c.keySlab[keyStart:keyStart:(keyStart + physicalAddressKeyLen)]
			key := ktype.WritePhysicalEVMKeyToBuffer(buf, kind, address[:])
			c.mutations[m].Key = key
			c.physicalKeys[m] = key
		}
		chunkEnd = accountEnd
	}

	priorValues := c.readPriorValues(c.accountOffsets[start], chunkEnd)
	for i := start; i < end; i++ {
		count := accountChangeMutationCount(&c.changes.Accounts[i])
		if count == 0 {
			continue
		}
		first := c.accountOffsets[i]
		err := c.writeAccountValues(&c.changes.Accounts[i], priorValues, c.mutations[first:first+count])
		if err != nil {
			return fmt.Errorf("account change %d for %s: %w", i, c.changes.Accounts[i].Address, err)
		}
	}
	return nil
}

// writeAccountValues sets the previous and new values of an account change's mutations: its account row, then its
// code row if its code changed.
func (c *stateChangeSetConverter) writeAccountValues(
	change *gigatypes.AccountChange,
	// The prior row under each found key, keyed by string(key).
	priorValues map[string][]byte,
	// The account change's mutations, starting with its account row. Their keys are already set.
	mutations []mutation.Mutation,
) error {
	fields := vtype.NewPendingAccountWrite()
	if change.BalanceChanged() {
		balance, err := balanceField(change.Account.Balance)
		if err != nil {
			return err
		}
		fields.SetBalance(balance)
	}
	if change.NonceChanged() {
		fields.SetNonce(change.Account.Nonce)
	}
	codeChanged := change.CodeChanged()
	if codeChanged {
		var codeHash vtype.CodeHash
		if len(change.Account.Code) != 0 {
			codeHash = vtype.CodeHash(crypto.Keccak256Hash(change.Account.Code))
		}
		fields.SetCodeHash(&codeHash)
	}

	account := &mutations[0]
	account.PreviousValue = priorValues[string(account.Key)]
	newRow, err := mergedAccountRow(fields, account.PreviousValue, c.blockHeight)
	if err != nil {
		return err
	}
	account.NewValue = newRow

	if codeChanged {
		code := &mutations[1]
		code.PreviousValue = priorValues[string(code.Key)]
		if len(change.Account.Code) != 0 {
			//nolint:gosec // block heights fit in an int64
			code.NewValue = vtype.SerializeCode(int64(c.blockHeight), change.Account.Code)
		}
	}
	return nil
}

// convertStorage converts the storage changes in [start, end).
func (c *stateChangeSetConverter) convertStorage(start int, end int) error {
	firstStorage := c.firstStorageMutation()
	storageKeyStart := firstStorage * physicalAddressKeyLen
	for i := start; i < end; i++ {
		change := &c.changes.Storage[i]
		keyStart := storageKeyStart + i*physicalStorageKeyLen
		buf := c.keySlab[keyStart:keyStart:(keyStart + physicalStorageKeyLen)]
		buf = ktype.WritePhysicalEVMKeyToBuffer(buf, keys.EVMKeyStorage, change.Address[:])
		key := append(buf, change.Key[:]...)
		c.mutations[firstStorage+i].Key = key
		c.physicalKeys[firstStorage+i] = key
	}

	priorValues := c.readPriorValues(firstStorage+start, firstStorage+end)
	height := int64(c.blockHeight) //nolint:gosec // block heights fit in an int64
	for i := start; i < end; i++ {
		change := &c.changes.Storage[i]
		m := &c.mutations[firstStorage+i]
		m.PreviousValue = priorValues[string(m.Key)]
		if change.Delete {
			continue
		}
		// An all-zero value is a deletion, for which SerializeStorage returns nil.
		value, err := vtype.SerializeStorage(height, change.Value[:])
		if err != nil {
			return fmt.Errorf("storage change %d: %w", i, err)
		}
		m.NewValue = value
	}
	return nil
}

// balanceField returns balance as an account row's balance field. A nil balance is zero.
func balanceField(balance *big.Int) (*vtype.Balance, error) {
	var field vtype.Balance
	if balance == nil {
		return &field, nil
	}
	if balance.Sign() < 0 || balance.BitLen() > 8*vtype.BalanceLen {
		return nil, errors.New("balance must fit in an unsigned 256-bit integer")
	}
	balance.FillBytes(field[:])
	return &field, nil
}
