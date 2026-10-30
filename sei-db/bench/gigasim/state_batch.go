package gigasim

import (
	"fmt"
	"math/big"

	"github.com/sei-protocol/sei-chain/sei-db/common/keys"
	gigatypes "github.com/sei-protocol/sei-chain/sei-db/state_db/giga/types"
)

// maxStagedKeyLen is the longest key the benchmark stages: a storage key, which carries an address and
// a slot after the one-byte EVM prefix.
const maxStagedKeyLen = 1 + storageKeyLen

// stagedKey is an EVM key held by value, so that using it as a map key costs no allocation.
type stagedKey struct {
	length uint8
	data   [maxStagedKeyLen]byte
}

// newStagedKey copies a key into a form that can be hashed and compared without allocating.
func newStagedKey(key []byte) stagedKey {
	if len(key) > maxStagedKeyLen {
		panic(fmt.Sprintf("key of %d bytes exceeds the %d bytes the benchmark stages",
			len(key), maxStagedKeyLen))
	}
	staged := stagedKey{length: uint8(len(key))} //nolint:gosec // guarded against overflow above
	copy(staged.data[:], key)
	return staged
}

// stagedWrite is one pending write. It retains the caller's key rather than copying it, which is safe
// because every key staged is a freshly built EVM key that nothing mutates.
type stagedWrite struct {
	key   []byte
	value []byte
}

// blockWrites is one block's state changes in the form the state DB takes, along with the volume of
// key and value bytes they carry.
type blockWrites struct {
	changes *gigatypes.StateChangeSet

	// The number of keys staged for the block, counters included.
	keys int64

	// The bytes the state WAL, SC and SS each take in, counted while the changes were assembled so
	// that the commit thread does not walk them again to find out.
	bytes int64
}

// stateBatch collects the writes of one block, keyed so that a key written twice in one block commits
// once.
//
// It carries no synchronization, because it never has more than one writer: the generator stages the
// block it is building, and setup stages on the main thread. Nothing writes here during execution — a
// block's writes are known when it is generated, so they are staged then.
type stateBatch struct {
	entries map[stagedKey]stagedWrite
}

// newStateBatch returns an empty batch sized for the number of distinct keys one block writes.
func newStateBatch(expectedWrites int) *stateBatch {
	return &stateBatch{entries: make(map[stagedKey]stagedWrite, expectedWrites)}
}

// Put stages a write, replacing any earlier write to the same key.
func (b *stateBatch) Put(key []byte, value []byte) {
	b.entries[newStagedKey(key)] = stagedWrite{key: key, value: value}
}

// drainToChangeSet empties the batch into a state change set, appending the identifier counters that
// ride along with every block as non-EVM writes.
func (b *stateBatch) drainToChangeSet(counters identifierCounters) blockWrites {
	changes := &gigatypes.StateChangeSet{
		Accounts: make([]gigatypes.AccountChange, 0, b.count()),
		Storage:  make([]gigatypes.StorageChange, 0, b.count()),
	}
	// An address can be staged under more than one account field, which must share one account change.
	accountIndex := make(map[gigatypes.Address]int)
	account := func(address []byte) *gigatypes.AccountState {
		i, ok := accountIndex[gigatypes.Address(address)]
		if !ok {
			i = len(changes.Accounts)
			accountIndex[gigatypes.Address(address)] = i
			changes.Accounts = append(changes.Accounts,
				gigatypes.AccountChange{Address: gigatypes.Address(address)})
		}
		return &changes.Accounts[i].Account
	}

	var staged int64
	for _, write := range b.entries {
		staged += int64(len(write.key) + len(write.value))
		kind, strippedKey := keys.ParseEVMKey(write.key)
		switch kind {
		case keys.EVMKeyBalance:
			account(strippedKey).Balance = new(big.Int).SetBytes(write.value)
		case keys.EVMKeyCode:
			account(strippedKey).Code = write.value
		case keys.EVMKeyStorage:
			changes.Storage = append(changes.Storage, gigatypes.StorageChange{
				Address: gigatypes.Address(strippedKey[:keys.AddressLen]),
				Key:     gigatypes.Hash(strippedKey[keys.AddressLen:]),
				Value:   gigatypes.Hash(write.value),
			})
		default:
			panic(fmt.Sprintf("the benchmark staged key %x of unsupported kind %v", write.key, kind))
		}
	}
	keyCount := int64(len(b.entries) + len(counterKeys))
	clear(b.entries)

	values := [len(counterKeys)][]byte{
		encodeCounter(counters.nextAccountID),
		encodeCounter(counters.nextErc20ContractID),
	}
	for i, key := range counterKeys {
		changes.Misc = append(changes.Misc,
			gigatypes.MiscChange{Module: counterModule, Key: key, Value: values[i]})
		staged += int64(len(key) + len(values[i]))
	}

	return blockWrites{changes: changes, keys: keyCount, bytes: staged}
}

// count returns how many distinct keys the batch holds.
func (b *stateBatch) count() int {
	return len(b.entries)
}
