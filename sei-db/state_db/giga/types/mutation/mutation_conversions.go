package mutation

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math/big"

	"github.com/sei-protocol/sei-chain/sei-db/common/keys"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	gigatypes "github.com/sei-protocol/sei-chain/sei-db/state_db/giga/types"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/ktype"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/vtype"
)

// This file converts mutations to and from changesets, and into state change sets. A mutation's key is a FlatKV
// physical key (ktype) and its values are FlatKV rows (vtype), the same bytes FlatKV stores and folds into its lattice
// hash. New values are written in the current vtype serialization version.

// MutationsFromChangeSets converts one whole block's changesets into one mutation per physical key the block writes.
// Account fields written separately in the changesets are merged onto the account's prior row into a single mutation.
// The returned mutations must not be modified.
func MutationsFromChangeSets(
	// Every changeset of the block. A key written more than once takes its last write.
	changeSets []*proto.NamedChangeSet,
	// The block the changesets belong to. Every new value is stamped with it.
	blockHeight uint64,
	// The state as of the block before blockHeight. Supplies each mutation's previous value.
	priorState gigatypes.StateView,
) ([]Mutation, error) {
	priorHeight := priorState.GetBlockHeight()
	//nolint:gosec // a non-negative height converts exactly
	if priorHeight < 0 || uint64(priorHeight)+1 != blockHeight {
		return nil, fmt.Errorf("prior state is at block %d, but block %d needs the state of the block before",
			priorHeight, blockHeight)
	}

	// Both maps are sized to the block's entry count, an upper bound on each, so neither grows while collecting.
	entryCount := 0
	for _, changeSet := range changeSets {
		if changeSet != nil {
			entryCount += len(changeSet.Changeset.Pairs)
		}
	}
	// The account fields the block writes to each account row, keyed by string(physical key). Each is merged onto
	// the account's previous row to produce its new value.
	accountFieldWrites := make(map[string]*vtype.PendingAccountWrite, entryCount)
	// The new value of each storage, code or misc key the block writes, or nil if deleted, keyed by
	// string(physical key).
	nonAccountNewValues := make(map[string][]byte, entryCount)
	for _, changeSet := range changeSets {
		if changeSet == nil {
			continue
		}
		for _, pair := range changeSet.Changeset.Pairs {
			kind, strippedKey, err := ktype.ParseLogicalKey(changeSet.Name, pair.Key)
			if err != nil {
				return nil, fmt.Errorf("failed to parse module %q key %x: %w",
					changeSet.Name, pair.Key, err)
			}
			var key []byte
			if kind == keys.EVMKeyMisc {
				key = ktype.BuildPhysicalKey(changeSet.Name, pair.Key)
			} else {
				key = ktype.BuildPhysicalEVMKey(kind, strippedKey)
			}
			if isAccountKind(kind) {
				fields, ok := accountFieldWrites[string(key)]
				if !ok {
					fields = vtype.NewPendingAccountWrite()
					accountFieldWrites[string(key)] = fields
				}
				err = fields.SetField(kind, pair.Value, pair.Delete)
			} else {
				nonAccountNewValues[string(key)], err = rowValue(kind, pair, blockHeight)
			}
			if err != nil {
				return nil, fmt.Errorf("failed to apply the write to module %q key %x: %w",
					changeSet.Name, pair.Key, err)
			}
		}
	}

	physicalKeys := make([][]byte, 0, len(accountFieldWrites)+len(nonAccountNewValues))
	for key := range accountFieldWrites {
		physicalKeys = append(physicalKeys, []byte(key))
	}
	for key := range nonAccountNewValues {
		physicalKeys = append(physicalKeys, []byte(key))
	}
	priorValues := priorState.BatchGetRaw(physicalKeys)

	mutations := make([]Mutation, len(physicalKeys))
	for i, key := range physicalKeys {
		previousValue := priorValues[string(key)]
		var newValue []byte
		if fields, isAccount := accountFieldWrites[string(key)]; isAccount {
			var err error
			newValue, err = mergedAccountRow(fields, previousValue, blockHeight)
			if err != nil {
				return nil, fmt.Errorf("failed to compute the new value of key %x: %w", key, err)
			}
		} else {
			newValue = nonAccountNewValues[string(key)]
		}
		mutations[i] = Mutation{Key: key, PreviousValue: previousValue, NewValue: newValue}
	}
	return mutations, nil
}

// ToChangeSet converts the mutation into a changeset that writes its new value, with one entry per account field for
// an account row. The previous value is not carried over. Neither the mutation nor the returned changeset may be
// modified.
func (m *Mutation) ToChangeSet() (*proto.NamedChangeSet, error) {
	moduleName, key, err := ktype.StripModulePrefix(m.Key)
	if err != nil {
		return nil, fmt.Errorf("failed to parse key %x: %w", m.Key, err)
	}
	pairs, err := m.changeSetPairs(moduleName, key)
	if err != nil {
		return nil, fmt.Errorf("failed to convert key %x: %w", m.Key, err)
	}
	return &proto.NamedChangeSet{Name: moduleName, Changeset: proto.ChangeSet{Pairs: pairs}}, nil
}

// MutationsToChangeSets converts mutations into one changeset per module that they write. Previous values are not
// carried over. Neither the mutations nor the returned changesets may be modified.
func MutationsToChangeSets(mutations []Mutation) ([]*proto.NamedChangeSet, error) {
	var changeSets []*proto.NamedChangeSet
	changeSetByModule := make(map[string]*proto.NamedChangeSet)
	for i := range mutations {
		changeSet, err := mutations[i].ToChangeSet()
		if err != nil {
			return nil, fmt.Errorf("failed to convert mutation %d: %w", i, err)
		}
		existing, ok := changeSetByModule[changeSet.Name]
		if !ok {
			changeSetByModule[changeSet.Name] = changeSet
			changeSets = append(changeSets, changeSet)
			continue
		}
		existing.Changeset.Pairs = append(existing.Changeset.Pairs, changeSet.Changeset.Pairs...)
	}
	return changeSets, nil
}

// isAccountKind reports whether kind is one of the account fields merged into a single account row.
func isAccountKind(kind keys.EVMKeyKind) bool {
	return kind == keys.EVMKeyNonce || kind == keys.EVMKeyCodeHash || kind == keys.EVMKeyBalance
}

// mergedAccountRow returns the account row after the block, or nil if the block leaves it deleted.
func mergedAccountRow(
	// The account fields the block writes.
	account *vtype.PendingAccountWrite,
	// The account row before the block, or nil if there was none.
	previousValue []byte,
	// The block the write belongs to.
	blockHeight uint64,
) ([]byte, error) {
	var prior *vtype.AccountData
	if previousValue != nil {
		parsed, err := vtype.DeserializeAccountData(previousValue)
		if err != nil {
			return nil, fmt.Errorf("failed to deserialize the prior account row: %w", err)
		}
		prior = parsed
	}
	merged := account.Merge(prior, int64(blockHeight)) //nolint:gosec // block heights fit in an int64
	if merged.IsDelete() {
		return nil, nil
	}
	return merged.Serialize(), nil
}

// rowValue returns the storage, code or misc row that write produces, or nil if write deletes the row.
func rowValue(
	// The kind of row: keys.EVMKeyStorage, keys.EVMKeyCode or keys.EVMKeyMisc.
	kind keys.EVMKeyKind,
	// The changeset entry writing the row: its raw value, or Delete set for a deletion.
	write *proto.KVPair,
	// The block the write belongs to.
	blockHeight uint64,
) ([]byte, error) {
	if write.Delete {
		return nil, nil
	}
	height := int64(blockHeight) //nolint:gosec // block heights fit in an int64
	switch kind {
	case keys.EVMKeyStorage:
		// An all-zero storage value is a deletion, for which SerializeStorage returns nil.
		value, err := vtype.SerializeStorage(height, write.Value)
		if err != nil {
			return nil, fmt.Errorf("invalid storage value: %w", err)
		}
		return value, nil
	case keys.EVMKeyCode:
		// Empty bytecode is a deletion.
		if len(write.Value) == 0 {
			return nil, nil
		}
		return vtype.SerializeCode(height, write.Value), nil
	default:
		// Protobuf does not distinguish an empty value from nil, and only Delete marks a deletion, so a nil
		// value is an empty row.
		return vtype.SerializeMisc(height, write.Value), nil
	}
}

// changeSetPairs returns the changeset pairs that write the mutation's new value.
func (m *Mutation) changeSetPairs(
	// The module the mutation's key belongs to.
	moduleName string,
	// The mutation's key with the module prefix removed.
	key []byte,
) ([]*proto.KVPair, error) {
	kind := keys.EVMKeyMisc
	strippedKey := key
	if moduleName == keys.EVMStoreKey {
		kind, strippedKey = keys.ParseEVMKey(key)
	}

	switch kind {
	case keys.EVMKeyEmpty:
		return nil, fmt.Errorf("empty EVM key")
	case ktype.EVMKeyAccount:
		return accountPairs(strippedKey, m.NewValue)
	case keys.EVMKeyCodeHash, keys.EVMKeyBalance:
		return nil, fmt.Errorf("account field key %x is not an account row key", key)
	}

	if m.NewValue == nil {
		return []*proto.KVPair{{Key: key, Delete: true}}, nil
	}
	var value []byte
	switch kind {
	case keys.EVMKeyStorage:
		row, err := vtype.DeserializeStorageData(m.NewValue)
		if err != nil {
			return nil, fmt.Errorf("failed to deserialize storage row: %w", err)
		}
		value = row.GetValue()[:]
	case keys.EVMKeyCode:
		row, err := vtype.DeserializeCodeData(m.NewValue)
		if err != nil {
			return nil, fmt.Errorf("failed to deserialize code row: %w", err)
		}
		value = row.GetBytecode()
	default:
		row, err := vtype.DeserializeMiscData(m.NewValue)
		if err != nil {
			return nil, fmt.Errorf("failed to deserialize misc row: %w", err)
		}
		value = row.GetValue()
	}
	return []*proto.KVPair{{Key: key, Value: value}}, nil
}

// accountPairs returns the nonce, code hash and balance pairs that write an account row. A zero code hash or balance
// is written as a deletion.
func accountPairs(
	// The account's address.
	address []byte,
	// The account row, or nil if the account is deleted.
	row []byte,
) ([]*proto.KVPair, error) {
	nonceKey := keys.BuildEVMKey(keys.EVMKeyNonce, address)
	codeHashKey := keys.BuildEVMKey(keys.EVMKeyCodeHash, address)
	balanceKey := keys.BuildEVMKey(keys.EVMKeyBalance, address)
	if row == nil {
		return []*proto.KVPair{
			{Key: nonceKey, Delete: true},
			{Key: codeHashKey, Delete: true},
			{Key: balanceKey, Delete: true},
		}, nil
	}

	account, err := vtype.DeserializeAccountData(row)
	if err != nil {
		return nil, fmt.Errorf("failed to deserialize account row: %w", err)
	}
	nonce := binary.BigEndian.AppendUint64(nil, account.GetNonce())
	return []*proto.KVPair{
		{Key: nonceKey, Value: nonce},
		fixedFieldPair(codeHashKey, account.GetCodeHash()[:]),
		fixedFieldPair(balanceKey, account.GetBalance()[:]),
	}, nil
}

// fixedFieldPair returns a pair writing value to key, or deleting key when value is all zeros.
func fixedFieldPair(key []byte, value []byte) *proto.KVPair {
	for _, b := range value {
		if b != 0 {
			return &proto.KVPair{Key: key, Value: value}
		}
	}
	return &proto.KVPair{Key: key, Delete: true}
}

// MutationsToStateChangeSet converts mutations into a StateChangeSet object.
func MutationsToStateChangeSet(
	// The mutations to convert.
	mutations []Mutation,
	// If set, a change whose new value equals its prior value is included. Otherwise it is omitted.
	includeUnchangedValues bool,
	// If set, mutations of keys of a kind a state change set does not support are skipped without being parsed.
	// Otherwise the presence of a state change that doesn't fit into a StateChangeSet results in an error.
	ignoreUnsupportedKeys bool,
) (*gigatypes.StateChangeSet, error) {
	changes := &gigatypes.StateChangeSet{}
	for i := range mutations {
		supported, err := mutations[i].appendStateChanges(changes, includeUnchangedValues)
		if err != nil {
			return nil, fmt.Errorf("failed to convert mutation %d: %w", i, err)
		}
		if !supported && !ignoreUnsupportedKeys {
			return nil, fmt.Errorf("mutation %d writes key %x, which a state change set cannot hold",
				i, mutations[i].Key)
		}
	}
	return changes, nil
}

// appendStateChanges appends the changes the mutation makes to changes. Returns false, appending nothing, if the
// mutation's key is of a kind a state change set does not hold.
func (m *Mutation) appendStateChanges(
	// The set to append to.
	changes *gigatypes.StateChangeSet,
	// If set, a change whose new value equals its prior value is appended.
	includeUnchangedValues bool,
) (bool, error) {
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
		err = m.appendAccountChanges(changes, gigatypes.Address(strippedKey), includeUnchangedValues)
	case keys.EVMKeyCode:
		err = m.appendCodeChange(changes, gigatypes.Address(strippedKey), includeUnchangedValues)
	case keys.EVMKeyStorage:
		address := gigatypes.Address(strippedKey[:keys.AddressLen])
		slot := gigatypes.Hash(strippedKey[keys.AddressLen:])
		err = m.appendStorageChange(changes, address, slot, includeUnchangedValues)
	default:
		return false, fmt.Errorf("unknown EVM key kind %v", kind)
	}
	if err != nil {
		return false, fmt.Errorf("failed to convert key %x: %w", m.Key, err)
	}
	return true, nil
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

// appendAccountChanges appends the balance and nonce changes an account row mutation makes.
func (m *Mutation) appendAccountChanges(
	// The set to append to.
	changes *gigatypes.StateChangeSet,
	// The account's address.
	address gigatypes.Address,
	// If set, a balance or nonce change is appended even when the field equals its prior value.
	includeUnchangedValues bool,
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
	if includeUnchangedValues || *balance != *priorBalance {
		changes.Balances = append(changes.Balances, gigatypes.BalanceChange{
			Address:      address,
			Balance:      new(big.Int).SetBytes(balance[:]),
			PriorBalance: new(big.Int).SetBytes(priorBalance[:]),
		})
	}
	nonce := newRow.GetNonce()
	priorNonce := previousRow.GetNonce()
	if includeUnchangedValues || nonce != priorNonce {
		changes.Nonces = append(changes.Nonces, gigatypes.NonceChange{
			Address:    address,
			Nonce:      nonce,
			PriorNonce: priorNonce,
		})
	}
	return nil
}

// appendCodeChange appends the code change a code row mutation makes.
func (m *Mutation) appendCodeChange(
	// The set to append to.
	changes *gigatypes.StateChangeSet,
	// The address the code belongs to.
	address gigatypes.Address,
	// If set, the change is appended even when the code equals its prior code.
	includeUnchangedValues bool,
) error {
	var newRow *vtype.CodeData
	if m.NewValue != nil {
		row, err := vtype.DeserializeCodeData(m.NewValue)
		if err != nil {
			return fmt.Errorf("failed to deserialize new code row: %w", err)
		}
		newRow = row
	}
	var previousRow *vtype.CodeData
	if m.PreviousValue != nil {
		row, err := vtype.DeserializeCodeData(m.PreviousValue)
		if err != nil {
			return fmt.Errorf("failed to deserialize previous code row: %w", err)
		}
		previousRow = row
	}
	// A missing row reads as empty bytecode, so deleting absent code is unchanged.
	if !includeUnchangedValues && bytes.Equal(newRow.GetBytecode(), previousRow.GetBytecode()) {
		return nil
	}
	change := gigatypes.CodeChange{Address: address, Delete: newRow == nil}
	if newRow != nil {
		change.Code = newRow.GetBytecode()
	}
	if previousRow != nil {
		change.PriorCode = previousRow.GetBytecode()
	}
	changes.Code = append(changes.Code, change)
	return nil
}

// appendStorageChange appends the storage change a storage row mutation makes.
func (m *Mutation) appendStorageChange(
	// The set to append to.
	changes *gigatypes.StateChangeSet,
	// The address the storage belongs to.
	address gigatypes.Address,
	// The storage slot.
	slot gigatypes.Hash,
	// If set, the change is appended even when the value equals its prior value.
	includeUnchangedValues bool,
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
	if !includeUnchangedValues && value == priorValue {
		return nil
	}
	changes.Storage = append(changes.Storage, gigatypes.StorageChange{
		Address:    address,
		Key:        slot,
		Delete:     newRow == nil,
		Value:      value,
		PriorValue: priorValue,
	})
	return nil
}
