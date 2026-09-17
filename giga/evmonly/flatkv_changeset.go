package evmonly

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"

	"github.com/sei-protocol/sei-chain/sei-db/common/keys"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/ktype"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/vtype"
)

const (
	flatKVAddressKeyLen = 1 + common.AddressLength
	flatKVStorageKeyLen = 1 + common.AddressLength + common.HashLength
)

// NewFlatKVChangeSetEncoder returns an encoder for FlatKV's EVM keyspace. The
// store is used to expand storage-prefix clears.
func NewFlatKVChangeSetEncoder(store *flatkv.CommitStore) NamedChangeSetEncoder {
	return func(changes StateChangeSet) ([]*proto.NamedChangeSet, error) {
		return encodeFlatKVChangeSet(store, changes)
	}
}

func encodeFlatKVChangeSet(store *flatkv.CommitStore, changes StateChangeSet) ([]*proto.NamedChangeSet, error) {
	if store == nil {
		return nil, errors.New("flatkv changeset encoder requires a store")
	}
	b := newFlatKVChangeSetBuilder(changes)

	for i, change := range changes.Balances {
		if err := validateFlatKVBalance(change.Balance); err != nil {
			return nil, fmt.Errorf("balance change %d for %s: %w", i, change.Address, err)
		}
		pair := b.addAddressPair(keys.EVMKeyBalance, change.Address)
		if change.Balance == nil || change.Balance.Sign() == 0 {
			pair.Delete = true
			continue
		}
		pair.Value = b.takeFixedValue(vtype.BalanceLen)
		change.Balance.FillBytes(pair.Value)
	}
	for _, change := range changes.Nonces {
		pair := b.addAddressPair(keys.EVMKeyNonce, change.Address)
		pair.Value = b.takeFixedValue(vtype.NonceLen)
		binary.BigEndian.PutUint64(pair.Value, change.Nonce)
	}
	for _, change := range changes.Code {
		codeHashPair := b.addAddressPair(keys.EVMKeyCodeHash, change.Address)
		codePair := b.addAddressPair(keys.EVMKeyCode, change.Address)
		if change.Delete || len(change.Code) == 0 {
			codeHashPair.Delete = true
			codePair.Delete = true
			continue
		}
		codeHashPair.Value = b.takeFixedValue(vtype.CodeHashLen)
		codeHash := crypto.Keccak256Hash(change.Code)
		copy(codeHashPair.Value, codeHash[:])
		codePair.Value = b.takeCodeValue(len(change.Code))
		copy(codePair.Value, change.Code)
	}
	for _, address := range changes.StorageClears {
		// Clear pairs come from iterating the store, so their count is unknown up front and they are allocated individually.
		var err error
		b.pairPtrs, err = appendFlatKVStorageClearPairs(store, b.pairPtrs, address)
		if err != nil {
			return nil, err
		}
	}
	for _, change := range changes.Storage {
		pair := b.addStoragePair(change.Address, change.Key)
		if change.Delete || change.Value == (common.Hash{}) {
			pair.Delete = true
			continue
		}
		pair.Value = b.takeFixedValue(common.HashLength)
		copy(pair.Value, change.Value[:])
	}
	if len(b.pairPtrs) == 0 {
		return nil, nil
	}
	return []*proto.NamedChangeSet{{
		Name:      keys.EVMStoreKey,
		Changeset: proto.ChangeSet{Pairs: b.pairPtrs},
	}}, nil
}

// flatKVChangeSetBuilder assembles a block's KVPairs, keys and values from per-call
// slabs. Keys and values are subslices of those slabs and outlive the changeset.
type flatKVChangeSetBuilder struct {
	pairs       []proto.KVPair
	pairPtrs    []*proto.KVPair
	keys        []byte
	fixedValues []byte
	codeValues  []byte
	pairOffset  int
	keyOffset   int
	fixedOffset int
	codeOffset  int
}

func newFlatKVChangeSetBuilder(changes StateChangeSet) *flatKVChangeSetBuilder {
	addressPairs := len(changes.Balances) + len(changes.Nonces) + 2*len(changes.Code)
	pairCount := addressPairs + len(changes.Storage)
	// Sized for every pair that could carry a value, so a block of deletions overshoots
	// rather than reallocating.
	fixedValueBytes := len(changes.Balances)*vtype.BalanceLen +
		len(changes.Nonces)*vtype.NonceLen +
		len(changes.Code)*vtype.CodeHashLen +
		len(changes.Storage)*common.HashLength
	codeValueBytes := 0
	for _, change := range changes.Code {
		if !change.Delete {
			codeValueBytes += len(change.Code)
		}
	}
	return &flatKVChangeSetBuilder{
		pairs:       make([]proto.KVPair, pairCount),
		pairPtrs:    make([]*proto.KVPair, 0, pairCount),
		keys:        make([]byte, addressPairs*flatKVAddressKeyLen+len(changes.Storage)*flatKVStorageKeyLen),
		fixedValues: make([]byte, fixedValueBytes),
		codeValues:  make([]byte, codeValueBytes),
	}
}

func (b *flatKVChangeSetBuilder) addAddressPair(kind keys.EVMKeyKind, address common.Address) *proto.KVPair {
	pair := b.nextPair(flatKVAddressKeyLen)
	if !keys.PutEVMKey(pair.Key, kind, address[:]) {
		panic(fmt.Sprintf("no EVM key prefix for kind %v", kind))
	}
	return pair
}

func (b *flatKVChangeSetBuilder) addStoragePair(address common.Address, slot common.Hash) *proto.KVPair {
	pair := b.nextPair(flatKVStorageKeyLen)
	if !keys.PutEVMKey(pair.Key, keys.EVMKeyStorage, address[:], slot[:]) {
		panic("no EVM key prefix for storage")
	}
	return pair
}

func (b *flatKVChangeSetBuilder) nextPair(keyLen int) *proto.KVPair {
	pair := &b.pairs[b.pairOffset]
	b.pairOffset++
	b.pairPtrs = append(b.pairPtrs, pair)
	pair.Key = b.keys[b.keyOffset : b.keyOffset+keyLen : b.keyOffset+keyLen]
	b.keyOffset += keyLen
	return pair
}

func (b *flatKVChangeSetBuilder) takeFixedValue(size int) []byte {
	value := b.fixedValues[b.fixedOffset : b.fixedOffset+size : b.fixedOffset+size]
	b.fixedOffset += size
	return value
}

func (b *flatKVChangeSetBuilder) takeCodeValue(size int) []byte {
	value := b.codeValues[b.codeOffset : b.codeOffset+size : b.codeOffset+size]
	b.codeOffset += size
	return value
}

func appendFlatKVStorageClearPairs(
	store *flatkv.CommitStore,
	pairs []*proto.KVPair,
	address common.Address,
) ([]*proto.KVPair, error) {
	start := flatKVStoragePrefix(address)
	iterator, err := store.Iterator(keys.EVMStoreKey, start, ktype.PrefixEnd(start), true)
	if err != nil {
		return nil, fmt.Errorf("iterate storage for clear of %s: %w", address, err)
	}
	defer func() { _ = iterator.Close() }()

	for ; iterator.Valid(); iterator.Next() {
		key := iterator.Key()
		if len(key) != 1+common.AddressLength+common.HashLength ||
			key[0] != flatKVStoragePrefixByte() || !bytes.Equal(key[1:1+common.AddressLength], address[:]) {
			return nil, fmt.Errorf("unexpected storage key while clearing %s: %x", address, key)
		}
		pairs = append(pairs, &proto.KVPair{Key: cloneBytes(key), Delete: true})
	}
	if err := iterator.Error(); err != nil {
		return nil, fmt.Errorf("iterate storage for clear of %s: %w", address, err)
	}
	return pairs, nil
}

func flatKVStoragePrefix(address common.Address) []byte {
	return keys.BuildEVMKey(keys.EVMKeyStorage, address[:])
}

func flatKVStoragePrefixByte() byte {
	prefix, ok := keys.EVMKeyPrefixByte(keys.EVMKeyStorage)
	if !ok {
		panic("missing EVM storage prefix")
	}
	return prefix
}

func validateFlatKVBalance(balance *big.Int) error {
	if balance == nil {
		return nil
	}
	if balance.Sign() < 0 || balance.BitLen() > 8*vtype.BalanceLen {
		return errors.New("balance must fit in an unsigned 256-bit integer")
	}
	return nil
}
