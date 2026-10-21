package mutation

import (
	"bytes"
	"encoding/binary"
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-db/common/keys"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	gigatypes "github.com/sei-protocol/sei-chain/sei-db/state_db/giga/types"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/ktype"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/vtype"
)

// This file converts between changesets and mutations. A mutation's key is a FlatKV physical key (ktype) and its
// values are FlatKV rows (vtype), the same bytes FlatKV stores and folds into its lattice hash. New values are
// written in the current vtype serialization version.

// MutationsFromChangeSets converts one whole block's changesets into one mutation per physical key the block writes,
// in the order each key is first written. Account fields written separately in the changesets are merged onto the
// account's prior row into a single mutation.
func MutationsFromChangeSets(
	// Every changeset of the block, in the order they were produced. A key written more than once takes its last
	// write.
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

	changes, err := collectKeyChanges(changeSets)
	if err != nil {
		return nil, fmt.Errorf("failed to collect the changes of block %d: %w", blockHeight, err)
	}

	physicalKeys := make([][]byte, len(changes))
	for i := range changes {
		physicalKeys[i] = changes[i].key
	}
	priorValues := priorState.BatchGetRaw(physicalKeys)

	mutations := make([]Mutation, len(changes))
	for i := range changes {
		previousValue := bytes.Clone(priorValues[string(changes[i].key)])
		newValue, err := changes[i].newValue(previousValue, blockHeight)
		if err != nil {
			return nil, fmt.Errorf("failed to compute the new value of key %x: %w", changes[i].key, err)
		}
		mutations[i] = Mutation{Key: changes[i].key, PreviousValue: previousValue, NewValue: newValue}
	}
	return mutations, nil
}

// ToChangeSet converts the mutation into a changeset holding the pairs that write its new value. An account row
// becomes one pair per account field. The previous value is not carried over.
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

// MutationsToChangeSets converts mutations into one changeset per module, modules in the order they first appear
// and pairs in mutation order. Previous values are not carried over.
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

// keyChange is everything one block's changesets write to one physical key.
type keyChange struct {
	// The physical key.
	key []byte

	// The kind of row the key holds. Account keys are always keys.EVMKeyNonce (ktype.EVMKeyAccount).
	kind keys.EVMKeyKind

	// The last value written to a storage, code or misc key, or nil if the last write deleted it.
	value []byte

	// The account fields written to an account key.
	account vtype.PendingAccountWrite
}

// collectKeyChanges gathers the writes of changeSets into one keyChange per physical key, in the order each key is
// first written.
func collectKeyChanges(changeSets []*proto.NamedChangeSet) ([]keyChange, error) {
	var changes []keyChange
	indexByKey := make(map[string]int)
	for _, changeSet := range changeSets {
		if changeSet == nil {
			continue
		}
		for _, pair := range changeSet.Changeset.Pairs {
			key, kind, err := physicalKeyOf(changeSet.Name, pair.Key)
			if err != nil {
				return nil, fmt.Errorf("failed to build the physical key of module %q key %x: %w",
					changeSet.Name, pair.Key, err)
			}
			index, ok := indexByKey[string(key)]
			if !ok {
				rowKind := kind
				if isAccountKind(kind) {
					rowKind = ktype.EVMKeyAccount
				}
				index = len(changes)
				indexByKey[string(key)] = index
				changes = append(changes, keyChange{key: key, kind: rowKind})
			}
			if err := changes[index].apply(kind, pair); err != nil {
				return nil, fmt.Errorf("failed to apply the write to module %q key %x: %w",
					changeSet.Name, pair.Key, err)
			}
		}
	}
	return changes, nil
}

// physicalKeyOf returns the FlatKV physical key a changeset pair writes, and the kind of the pair.
func physicalKeyOf(
	// The name of the changeset holding the pair.
	moduleName string,
	// The pair's key.
	key []byte,
) ([]byte, keys.EVMKeyKind, error) {
	if moduleName != keys.EVMStoreKey {
		if moduleName == "" {
			return nil, keys.EVMKeyEmpty, fmt.Errorf("empty module name")
		}
		return ktype.ModulePhysicalKey(moduleName, key), keys.EVMKeyMisc, nil
	}
	kind, strippedKey := keys.ParseEVMKey(key)
	switch kind {
	case keys.EVMKeyEmpty:
		return nil, kind, fmt.Errorf("empty EVM key")
	case keys.EVMKeyMisc:
		return ktype.ModulePhysicalKey(keys.EVMStoreKey, key), kind, nil
	default:
		return ktype.EVMPhysicalKey(kind, strippedKey), kind, nil
	}
}

// isAccountKind reports whether kind is one of the account fields merged into a single account row.
func isAccountKind(kind keys.EVMKeyKind) bool {
	return kind == keys.EVMKeyNonce || kind == keys.EVMKeyCodeHash || kind == keys.EVMKeyBalance
}

// apply records one changeset pair's write to this key.
func (c *keyChange) apply(
	// The kind of the pair, which for an account key names the field written.
	kind keys.EVMKeyKind,
	// The pair.
	pair *proto.KVPair,
) error {
	switch kind {
	case keys.EVMKeyNonce:
		// A deleted nonce is a zero nonce.
		var nonce uint64
		if !pair.Delete {
			parsed, err := vtype.ParseNonce(pair.Value)
			if err != nil {
				return fmt.Errorf("invalid nonce: %w", err)
			}
			nonce = parsed
		}
		c.account.SetNonce(nonce)
	case keys.EVMKeyCodeHash:
		if pair.Delete {
			// A deleted code hash is a zero code hash.
			c.account.SetCodeHash(nil)
		} else if err := c.account.SetCodeHashBytes(pair.Value); err != nil {
			return fmt.Errorf("invalid code hash: %w", err)
		}
	case keys.EVMKeyBalance:
		// A deleted balance is a zero balance.
		var balance *vtype.Balance
		if !pair.Delete {
			parsed, err := vtype.ParseBalance(pair.Value)
			if err != nil {
				return fmt.Errorf("invalid balance: %w", err)
			}
			balance = parsed
		}
		c.account.SetBalance(balance)
	default:
		if pair.Delete {
			c.value = nil
		} else {
			// Protobuf does not distinguish an empty value from nil, and only Delete marks a deletion.
			c.value = bytes.Clone(pair.Value)
			if c.value == nil {
				c.value = []byte{}
			}
		}
	}
	return nil
}

// newValue returns the row the key holds after the block, or nil if the block leaves it deleted.
func (c *keyChange) newValue(
	// The row the key held before the block, or nil if it held none.
	previousValue []byte,
	// The block the change belongs to.
	blockHeight uint64,
) ([]byte, error) {
	height := int64(blockHeight) //nolint:gosec // block heights fit in an int64
	switch c.kind {
	case ktype.EVMKeyAccount:
		var prior *vtype.AccountData
		if previousValue != nil {
			parsed, err := vtype.DeserializeAccountData(previousValue)
			if err != nil {
				return nil, fmt.Errorf("failed to deserialize the prior account row: %w", err)
			}
			prior = parsed
		}
		merged := c.account.Merge(prior, height)
		if merged.IsDelete() {
			return nil, nil
		}
		return merged.Serialize(), nil
	case keys.EVMKeyStorage:
		if c.value == nil {
			return nil, nil
		}
		// An all-zero storage value is a deletion, for which SerializeStorage returns nil.
		value, err := vtype.SerializeStorage(height, c.value)
		if err != nil {
			return nil, fmt.Errorf("invalid storage value: %w", err)
		}
		return value, nil
	case keys.EVMKeyCode:
		// Empty bytecode is a deletion.
		if len(c.value) == 0 {
			return nil, nil
		}
		return vtype.SerializeCode(height, c.value), nil
	default:
		if c.value == nil {
			return nil, nil
		}
		return vtype.SerializeMisc(height, c.value), nil
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
	return []*proto.KVPair{{Key: key, Value: bytes.Clone(value)}}, nil
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
			return &proto.KVPair{Key: key, Value: bytes.Clone(value)}
		}
	}
	return &proto.KVPair{Key: key, Delete: true}
}
