package flatkv

import (
	"encoding/binary"
	"fmt"
	"hash/maphash"
	"slices"

	"golang.org/x/sync/errgroup"

	"github.com/sei-protocol/sei-chain/sei-db/common/keys"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	gigatypes "github.com/sei-protocol/sei-chain/sei-db/state_db/giga/types"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/giga/types/mutation"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/ktype"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/vtype"
)

// This file converts between changesets and mutations. A mutation's key is a FlatKV physical key (ktype) and its
// values are FlatKV rows (vtype), the same bytes FlatKV stores and folds into its lattice hash. New values are
// written in the current vtype serialization version.

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
) ([]mutation.Mutation, error) {
	priorHeight := priorState.GetBlockHeight()
	//nolint:gosec // a non-negative height converts exactly
	if priorHeight < 0 || uint64(priorHeight)+1 != blockHeight {
		return nil, fmt.Errorf("prior state is at block %d, but block %d needs the state of the block before",
			priorHeight, blockHeight)
	}

	writes, err := keyWrites(changeSets)
	if err != nil {
		return nil, err
	}
	partitions := max(1, min(maxConversionPartitions, len(writes)/minWritesPerPartition))
	if partitions == 1 {
		return mutationsFromWrites(writes, blockHeight, priorState)
	}
	// A key's writes all land in one partition, in block order, so its last write still wins.
	seed := maphash.MakeSeed()
	byPartition := make([][]keyedWrite, partitions)
	for _, write := range writes {
		partition := maphash.Bytes(seed, write.key) % uint64(partitions) //nolint:gosec // partitions > 0
		byPartition[partition] = append(byPartition[partition], write)
	}
	collected := make([]*collectedWrites, partitions)
	var group errgroup.Group
	for i := range byPartition {
		group.Go(func() error {
			var err error
			collected[i], err = collectWrites(byPartition[i], blockHeight)
			return err
		})
	}
	if err := group.Wait(); err != nil {
		return nil, err
	}
	// One batch read for the whole block: the store spreads it across its own workers.
	var physicalKeys [][]byte
	for _, c := range collected {
		physicalKeys = append(physicalKeys, c.keys...)
	}
	priorValues := priorState.BatchGetRaw(physicalKeys)
	results := make([][]mutation.Mutation, partitions)
	for i, c := range collected {
		group.Go(func() error {
			var err error
			results[i], err = c.mutations(priorValues, blockHeight)
			return err
		})
	}
	if err := group.Wait(); err != nil {
		return nil, err
	}
	return slices.Concat(results...), nil
}

// maxConversionPartitions bounds the goroutines one block's conversion runs at once.
const maxConversionPartitions = 16

// minWritesPerPartition is the fewest writes a conversion partition is worth a goroutine for.
const minWritesPerPartition = 512

// keyedWrite is one changeset write with the physical key and kind it resolves to.
type keyedWrite struct {
	module string
	pair   *proto.KVPair
	kind   keys.EVMKeyKind
	key    []byte
}

// keyWrites resolves every write of the block to its physical key, in block order.
func keyWrites(changeSets []*proto.NamedChangeSet) ([]keyedWrite, error) {
	entryCount := 0
	for _, changeSet := range changeSets {
		if changeSet != nil {
			entryCount += len(changeSet.Changeset.Pairs)
		}
	}
	writes := make([]keyedWrite, 0, entryCount)
	for _, changeSet := range changeSets {
		if changeSet == nil {
			continue
		}
		for _, pair := range changeSet.Changeset.Pairs {
			writes = append(writes, keyedWrite{module: changeSet.Name, pair: pair})
		}
	}
	chunk := max(minWritesPerPartition, (len(writes)+maxConversionPartitions-1)/maxConversionPartitions)
	var group errgroup.Group
	for start := 0; start < len(writes); start += chunk {
		group.Go(func() error {
			for i := start; i < min(start+chunk, len(writes)); i++ {
				write := &writes[i]
				kind, strippedKey, err := ktype.ParseLogicalKey(write.module, write.pair.Key)
				if err != nil {
					return fmt.Errorf("failed to parse module %q key %x: %w", write.module, write.pair.Key, err)
				}
				write.kind = kind
				if kind == keys.EVMKeyMisc {
					write.key = ktype.BuildPhysicalKey(write.module, write.pair.Key)
				} else {
					write.key = ktype.BuildPhysicalEVMKey(kind, strippedKey)
				}
			}
			return nil
		})
	}
	if err := group.Wait(); err != nil {
		return nil, err
	}
	return writes, nil
}

// mutationsFromWrites converts writes into one mutation per physical key they write, reading each key's previous
// value from priorState.
func mutationsFromWrites(writes []keyedWrite, blockHeight uint64, priorState gigatypes.StateView) ([]mutation.Mutation, error) {
	c, err := collectWrites(writes, blockHeight)
	if err != nil {
		return nil, err
	}
	return c.mutations(priorState.BatchGetRaw(c.keys), blockHeight)
}

// collectedWrites is a set of writes folded to one pending value per physical key.
type collectedWrites struct {
	// The account fields written to each account row, keyed by string(physical key). Each is merged onto the
	// account's previous row to produce its new value.
	accountFieldWrites map[string]*vtype.PendingAccountWrite
	// The new value of each storage, code or misc key written, or nil if deleted, keyed by string(physical key).
	nonAccountNewValues map[string][]byte
	// Every physical key written.
	keys [][]byte
}

// collectWrites folds writes, in order, to the pending value of each physical key they write.
func collectWrites(writes []keyedWrite, blockHeight uint64) (*collectedWrites, error) {
	// Both maps are sized to the write count, an upper bound on each, so neither grows while collecting.
	c := &collectedWrites{
		accountFieldWrites:  make(map[string]*vtype.PendingAccountWrite, len(writes)),
		nonAccountNewValues: make(map[string][]byte, len(writes)),
	}
	for _, write := range writes {
		var err error
		if isAccountKind(write.kind) {
			fields, ok := c.accountFieldWrites[string(write.key)]
			if !ok {
				fields = vtype.NewPendingAccountWrite()
				c.accountFieldWrites[string(write.key)] = fields
			}
			err = fields.SetField(write.kind, write.pair.Value, write.pair.Delete)
		} else {
			c.nonAccountNewValues[string(write.key)], err = rowValue(write.kind, write.pair, blockHeight)
		}
		if err != nil {
			return nil, fmt.Errorf("failed to apply the write to module %q key %x: %w",
				write.module, write.pair.Key, err)
		}
	}
	c.keys = make([][]byte, 0, len(c.accountFieldWrites)+len(c.nonAccountNewValues))
	for key := range c.accountFieldWrites {
		c.keys = append(c.keys, []byte(key))
	}
	for key := range c.nonAccountNewValues {
		c.keys = append(c.keys, []byte(key))
	}
	return c, nil
}

// mutations returns one mutation per collected key, taking each key's previous value from priorValues.
func (c *collectedWrites) mutations(priorValues map[string][]byte, blockHeight uint64) ([]mutation.Mutation, error) {
	mutations := make([]mutation.Mutation, len(c.keys))
	for i, key := range c.keys {
		previousValue := priorValues[string(key)]
		var newValue []byte
		if fields, isAccount := c.accountFieldWrites[string(key)]; isAccount {
			var err error
			newValue, err = mergedAccountRow(fields, previousValue, blockHeight)
			if err != nil {
				return nil, fmt.Errorf("failed to compute the new value of key %x: %w", key, err)
			}
		} else {
			newValue = c.nonAccountNewValues[string(key)]
		}
		mutations[i] = mutation.Mutation{Key: key, PreviousValue: previousValue, NewValue: newValue}
	}
	return mutations, nil
}

// MutationToChangeSet returns a changeset that writes m's new value, with one entry per account field for an account
// row. The previous value is not carried over. Neither m nor the returned changeset may be modified.
func MutationToChangeSet(m *mutation.Mutation) (*proto.NamedChangeSet, error) {
	moduleName, key, err := ktype.StripModulePrefix(m.Key)
	if err != nil {
		return nil, fmt.Errorf("failed to parse key %x: %w", m.Key, err)
	}
	pairs, err := changeSetPairs(m, moduleName, key)
	if err != nil {
		return nil, fmt.Errorf("failed to convert key %x: %w", m.Key, err)
	}
	return &proto.NamedChangeSet{Name: moduleName, Changeset: proto.ChangeSet{Pairs: pairs}}, nil
}

// MutationsToChangeSets converts mutations into one changeset per module that they write. Previous values are not
// carried over. Neither the mutations nor the returned changesets may be modified.
func MutationsToChangeSets(mutations []mutation.Mutation) ([]*proto.NamedChangeSet, error) {
	var changeSets []*proto.NamedChangeSet
	changeSetByModule := make(map[string]*proto.NamedChangeSet)
	for i := range mutations {
		changeSet, err := MutationToChangeSet(&mutations[i])
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

// changeSetPairs returns the changeset pairs that write m's new value.
func changeSetPairs(
	// The mutation to convert.
	m *mutation.Mutation,
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
