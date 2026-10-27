package evmonly

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"

	"github.com/sei-protocol/sei-chain/sei-db/common/keys"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/vtype"
)

const (
	flatKVAddressKeyLen = 1 + common.AddressLength
	flatKVStorageKeyLen = 1 + common.AddressLength + common.HashLength
)

// NewFlatKVChangeSetEncoder returns an encoder for FlatKV's EVM keyspace.
func NewFlatKVChangeSetEncoder() NamedChangeSetEncoder {
	return encodeFlatKVChangeSet
}

func encodeFlatKVChangeSet(changes StateChangeSet) ([]*proto.NamedChangeSet, error) {
	b := newFlatKVChangeSetBuilder(changes)

	for i, change := range changes.Accounts {
		if change.BalanceChanged() {
			balance := change.Account.Balance
			if err := validateFlatKVBalance(balance); err != nil {
				return nil, fmt.Errorf("account change %d for %s: %w", i, change.Address, err)
			}
			pair := b.addAddressPair(keys.EVMKeyBalance, change.Address)
			if balance == nil || balance.Sign() == 0 {
				pair.Delete = true
			} else {
				pair.Value = b.takeFixedValue(vtype.BalanceLen)
				balance.FillBytes(pair.Value)
			}
		}
		if change.NonceChanged() {
			pair := b.addAddressPair(keys.EVMKeyNonce, change.Address)
			pair.Value = b.takeFixedValue(vtype.NonceLen)
			binary.BigEndian.PutUint64(pair.Value, change.Account.Nonce)
		}
		if change.CodeChanged() {
			code := change.Account.Code
			codeHashPair := b.addAddressPair(keys.EVMKeyCodeHash, change.Address)
			codePair := b.addAddressPair(keys.EVMKeyCode, change.Address)
			if len(code) == 0 {
				codeHashPair.Delete = true
				codePair.Delete = true
			} else {
				codeHashPair.Value = b.takeFixedValue(vtype.CodeHashLen)
				codeHash := crypto.Keccak256Hash(code)
				copy(codeHashPair.Value, codeHash[:])
				codePair.Value = b.takeCodeValue(len(code))
				copy(codePair.Value, code)
			}
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
	// Sized for every pair that could carry a value.
	addressPairs, codeValueBytes := 0, 0
	fixedValueBytes := len(changes.Storage) * common.HashLength
	for _, change := range changes.Accounts {
		if change.BalanceChanged() {
			addressPairs++
			fixedValueBytes += vtype.BalanceLen
		}
		if change.NonceChanged() {
			addressPairs++
			fixedValueBytes += vtype.NonceLen
		}
		if change.CodeChanged() {
			addressPairs += 2
			fixedValueBytes += vtype.CodeHashLen
			codeValueBytes += len(change.Account.Code)
		}
	}
	pairCount := addressPairs + len(changes.Storage)
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

func validateFlatKVBalance(balance *big.Int) error {
	if balance == nil {
		return nil
	}
	if balance.Sign() < 0 || balance.BitLen() > 8*vtype.BalanceLen {
		return errors.New("balance must fit in an unsigned 256-bit integer")
	}
	return nil
}
