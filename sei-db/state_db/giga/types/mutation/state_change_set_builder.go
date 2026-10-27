package mutation

import (
	"fmt"
	"math/big"
	"slices"

	"github.com/sei-protocol/sei-chain/sei-db/common/keys"
	gigatypes "github.com/sei-protocol/sei-chain/sei-db/state_db/giga/types"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/ktype"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/vtype"
)

// stateChangeSetBuilder collects the state changes a list of mutations makes into a StateChangeSet.
type stateChangeSetBuilder struct {
	// The set under construction.
	changes *gigatypes.StateChangeSet
	// The index in changes.Accounts of each address's account change.
	accountIndex map[gigatypes.Address]int
}

// MutationsToStateChangeSet converts mutations into a StateChangeSet object, with one account change per address whose
// balance, nonce or code changes and one storage change per slot whose value changes. An account change's balance and
// nonce come from the account row mutation and its code from the code row mutation.
func MutationsToStateChangeSet(
	// The mutations to convert.
	mutations []Mutation,
	// If set, mutations of keys of a kind a state change set does not support are skipped without being parsed.
	// Otherwise the presence of a state change that doesn't fit into a StateChangeSet results in an error.
	ignoreUnsupportedKeys bool,
) (*gigatypes.StateChangeSet, error) {
	// The slices and the map are sized to the mutation count, an upper bound on each, so none grows.
	b := &stateChangeSetBuilder{
		changes: &gigatypes.StateChangeSet{
			Accounts: make([]gigatypes.AccountChange, 0, len(mutations)),
			Storage:  make([]gigatypes.StorageChange, 0, len(mutations)),
		},
		accountIndex: make(map[gigatypes.Address]int, len(mutations)),
	}
	for i := range mutations {
		supported, err := b.add(&mutations[i])
		if err != nil {
			return nil, fmt.Errorf("failed to convert mutation %d: %w", i, err)
		}
		if !supported && !ignoreUnsupportedKeys {
			return nil, fmt.Errorf("mutation %d writes key %x, which a state change set cannot hold",
				i, mutations[i].Key)
		}
	}
	b.changes.Accounts = slices.DeleteFunc(b.changes.Accounts, func(change gigatypes.AccountChange) bool {
		return !change.BalanceChanged() && !change.NonceChanged() && !change.CodeChanged()
	})
	return b.changes, nil
}

// add records the changes the mutation makes. Returns false, recording nothing, if the mutation's key is of a kind a
// state change set does not hold.
func (b *stateChangeSetBuilder) add(m *Mutation) (bool, error) {
	moduleName, key, err := ktype.StripModulePrefix(m.Key)
	if err != nil {
		return false, fmt.Errorf("failed to parse key %x: %w", m.Key, err)
	}
	if moduleName == "" {
		return false, fmt.Errorf("key %x has an empty module name", m.Key)
	}
	if moduleName != keys.EVMStoreKey {
		return false, nil
	}

	// ParseEVMKey classifies a row key of the wrong length as misc, where it is rejected, so the stripped key of
	// every row kind has exactly the length of an address, or of an address followed by a slot.
	kind, strippedKey := keys.ParseEVMKey(key)
	switch kind {
	case keys.EVMKeyMisc:
		if hasEVMRowPrefix(key) {
			return false, fmt.Errorf("EVM row key %x has the wrong length", key)
		}
		return false, nil
	case keys.EVMKeyEmpty:
		return false, fmt.Errorf("empty EVM key")
	case keys.EVMKeyCodeHash, keys.EVMKeyBalance:
		return false, fmt.Errorf("account field key %x is not an account row key", key)
	case ktype.EVMKeyAccount:
		err = b.setAccountRow(m, gigatypes.Address(strippedKey))
	case keys.EVMKeyCode:
		err = b.setCodeRow(m, gigatypes.Address(strippedKey))
	case keys.EVMKeyStorage:
		address := gigatypes.Address(strippedKey[:keys.AddressLen])
		slot := gigatypes.Hash(strippedKey[keys.AddressLen:])
		err = b.appendStorageChange(m, address, slot)
	default:
		return false, fmt.Errorf("unknown EVM key kind %v", kind)
	}
	if err != nil {
		return false, fmt.Errorf("failed to convert key %x: %w", m.Key, err)
	}
	return true, nil
}

// accountChange returns the account change of address, appending an empty one if there is none. The returned pointer
// is valid until the next append to the account changes.
func (b *stateChangeSetBuilder) accountChange(address gigatypes.Address) *gigatypes.AccountChange {
	i, ok := b.accountIndex[address]
	if !ok {
		i = len(b.changes.Accounts)
		b.accountIndex[address] = i
		b.changes.Accounts = append(b.changes.Accounts, gigatypes.AccountChange{Address: address})
	}
	return &b.changes.Accounts[i]
}

// setAccountRow sets the balance and nonce of an address's account change from an account row mutation.
func (b *stateChangeSetBuilder) setAccountRow(
	// The account row mutation.
	m *Mutation,
	// The account's address.
	address gigatypes.Address,
) error {
	var newRow *vtype.AccountData
	if m.NewValue != nil {
		row, err := vtype.DeserializeAccountData(m.NewValue)
		if err != nil {
			return fmt.Errorf("failed to deserialize new account row: %w", err)
		}
		newRow = row
	}
	var previousRow *vtype.AccountData
	if m.PreviousValue != nil {
		row, err := vtype.DeserializeAccountData(m.PreviousValue)
		if err != nil {
			return fmt.Errorf("failed to deserialize previous account row: %w", err)
		}
		previousRow = row
	}
	balance := newRow.GetBalance()
	priorBalance := previousRow.GetBalance()
	change := b.accountChange(address)
	change.Account.Balance = new(big.Int).SetBytes(balance[:])
	change.Prior.Balance = new(big.Int).SetBytes(priorBalance[:])
	change.Account.Nonce = newRow.GetNonce()
	change.Prior.Nonce = previousRow.GetNonce()
	return nil
}

// setCodeRow sets the code of an address's account change from a code row mutation. A missing row is no code.
func (b *stateChangeSetBuilder) setCodeRow(
	// The code row mutation.
	m *Mutation,
	// The address the code belongs to.
	address gigatypes.Address,
) error {
	var code []byte
	if m.NewValue != nil {
		row, err := vtype.DeserializeCodeData(m.NewValue)
		if err != nil {
			return fmt.Errorf("failed to deserialize new code row: %w", err)
		}
		code = row.GetBytecode()
	}
	var priorCode []byte
	if m.PreviousValue != nil {
		row, err := vtype.DeserializeCodeData(m.PreviousValue)
		if err != nil {
			return fmt.Errorf("failed to deserialize previous code row: %w", err)
		}
		priorCode = row.GetBytecode()
	}
	change := b.accountChange(address)
	change.Account.Code = code
	change.Prior.Code = priorCode
	return nil
}

// appendStorageChange appends the storage change a storage row mutation makes.
func (b *stateChangeSetBuilder) appendStorageChange(
	// The storage row mutation.
	m *Mutation,
	// The address the storage belongs to.
	address gigatypes.Address,
	// The storage slot.
	slot gigatypes.Hash,
) error {
	var newRow *vtype.StorageData
	if m.NewValue != nil {
		row, err := vtype.DeserializeStorageData(m.NewValue)
		if err != nil {
			return fmt.Errorf("failed to deserialize new storage row: %w", err)
		}
		newRow = row
	}
	var previousRow *vtype.StorageData
	if m.PreviousValue != nil {
		row, err := vtype.DeserializeStorageData(m.PreviousValue)
		if err != nil {
			return fmt.Errorf("failed to deserialize previous storage row: %w", err)
		}
		previousRow = row
	}
	// A missing row reads as zero, so deleting an absent slot is unchanged.
	value := *newRow.GetValue()
	priorValue := *previousRow.GetValue()
	if value == priorValue {
		return nil
	}
	b.changes.Storage = append(b.changes.Storage, gigatypes.StorageChange{
		Address:    address,
		Key:        slot,
		Delete:     newRow == nil,
		Value:      value,
		PriorValue: priorValue,
	})
	return nil
}

// hasEVMRowPrefix reports whether key, a non-empty EVM key, starts with the prefix of a nonce, code hash, balance,
// code or storage key.
func hasEVMRowPrefix(key []byte) bool {
	rowKinds := []keys.EVMKeyKind{
		keys.EVMKeyNonce, keys.EVMKeyCodeHash, keys.EVMKeyBalance, keys.EVMKeyCode, keys.EVMKeyStorage,
	}
	for _, kind := range rowKinds {
		prefix, _ := keys.EVMKeyPrefixByte(kind)
		if key[0] == prefix {
			return true
		}
	}
	return false
}
