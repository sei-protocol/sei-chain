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
	pairs := make([]*proto.KVPair, 0,
		len(changes.Balances)+len(changes.Nonces)+2*len(changes.Code)+len(changes.Storage))

	for i, change := range changes.Balances {
		value, err := flatKVBalanceBytes(change.Balance)
		if err != nil {
			return nil, fmt.Errorf("balance change %d for %s: %w", i, change.Address, err)
		}
		pair := &proto.KVPair{Key: flatKVAddressKey(keys.EVMKeyBalance, change.Address), Value: value}
		if change.Balance == nil || change.Balance.Sign() == 0 {
			pair.Value = nil
			pair.Delete = true
		}
		pairs = append(pairs, pair)
	}
	for _, change := range changes.Nonces {
		value := make([]byte, vtype.NonceLen)
		binary.BigEndian.PutUint64(value, change.Nonce)
		pairs = append(pairs, &proto.KVPair{
			Key:   flatKVAddressKey(keys.EVMKeyNonce, change.Address),
			Value: value,
		})
	}
	for _, change := range changes.Code {
		codeHashPair := &proto.KVPair{Key: flatKVAddressKey(keys.EVMKeyCodeHash, change.Address)}
		codePair := &proto.KVPair{Key: flatKVAddressKey(keys.EVMKeyCode, change.Address)}
		if change.Delete || len(change.Code) == 0 {
			codeHashPair.Delete = true
			codePair.Delete = true
		} else {
			codeHash := crypto.Keccak256Hash(change.Code)
			codeHashPair.Value = codeHash[:]
			codePair.Value = cloneBytes(change.Code)
		}
		pairs = append(pairs, codeHashPair, codePair)
	}
	for _, address := range changes.StorageClears {
		var err error
		pairs, err = appendFlatKVStorageClearPairs(store, pairs, address)
		if err != nil {
			return nil, err
		}
	}
	for _, change := range changes.Storage {
		pair := &proto.KVPair{Key: flatKVStorageKey(change.Address, change.Key)}
		if change.Delete || change.Value == (common.Hash{}) {
			pair.Delete = true
		} else {
			pair.Value = cloneBytes(change.Value[:])
		}
		pairs = append(pairs, pair)
	}
	if len(pairs) == 0 {
		return nil, nil
	}
	return []*proto.NamedChangeSet{{
		Name:      keys.EVMStoreKey,
		Changeset: proto.ChangeSet{Pairs: pairs},
	}}, nil
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

func flatKVAddressKey(kind keys.EVMKeyKind, address common.Address) []byte {
	return keys.BuildEVMKey(kind, address[:])
}

func flatKVStorageKey(address common.Address, slot common.Hash) []byte {
	key := make([]byte, 0, common.AddressLength+common.HashLength)
	key = append(key, address[:]...)
	key = append(key, slot[:]...)
	return keys.BuildEVMKey(keys.EVMKeyStorage, key)
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

func flatKVBalanceBytes(balance *big.Int) ([]byte, error) {
	value := make([]byte, vtype.BalanceLen)
	if balance == nil {
		return value, nil
	}
	if balance.Sign() < 0 || balance.BitLen() > 8*vtype.BalanceLen {
		return nil, errors.New("balance must fit in an unsigned 256-bit integer")
	}
	balance.FillBytes(value)
	return value, nil
}
