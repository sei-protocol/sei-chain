package evmonly

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math/big"
	"runtime"
	"sync"
	"sync/atomic"

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
	b, err := newFlatKVChangeSetBuilder(changes)
	if err != nil {
		return nil, err
	}
	b.fill(changes)
	if len(b.pairPtrs) == 0 {
		return nil, nil
	}
	return []*proto.NamedChangeSet{{
		Name:      keys.EVMStoreKey,
		Changeset: proto.ChangeSet{Pairs: b.pairPtrs},
	}}, nil
}

// flatKVParallelEncodeChunk is how many changes one goroutine encodes at a time.
const flatKVParallelEncodeChunk = 4096

// flatKVAccountLayout is where one account change's pairs and values go in the builder's slabs, and
// which of its fields the block changed.
type flatKVAccountLayout struct {
	pairAt, fixedAt, codeAt int
	balance, nonce, code    bool
}

// flatKVChangeSetBuilder lays out a block's pairs in slabs so changes can be encoded in any order.
type flatKVChangeSetBuilder struct {
	pairs       []proto.KVPair
	pairPtrs    []*proto.KVPair
	keys        []byte
	fixedValues []byte
	codeValues  []byte
	accounts    []flatKVAccountLayout
	// addressPairs is how many pairs the account changes produce; the storage pairs follow them.
	addressPairs int
	// accountFixedBytes is how many fixed-size value bytes the account changes take; the storage
	// values follow them.
	accountFixedBytes int
}

// newFlatKVChangeSetBuilder lays out every pair the changes produce, in the order one serial pass
// emits them, and rejects a changed balance FlatKV cannot encode.
func newFlatKVChangeSetBuilder(changes StateChangeSet) (*flatKVChangeSetBuilder, error) {
	accounts := make([]flatKVAccountLayout, len(changes.Accounts))
	addressPairs, fixedBytes, codeBytes := 0, 0, 0
	for i := range changes.Accounts {
		change := &changes.Accounts[i]
		layout := flatKVAccountLayout{pairAt: addressPairs, fixedAt: fixedBytes, codeAt: codeBytes}
		if change.BalanceChanged() {
			if err := validateFlatKVBalance(change.Account.Balance); err != nil {
				return nil, fmt.Errorf("account change %d for %s: %w", i, change.Address, err)
			}
			layout.balance = true
			addressPairs++
			fixedBytes += vtype.BalanceLen
		}
		if change.NonceChanged() {
			layout.nonce = true
			addressPairs++
			fixedBytes += vtype.NonceLen
		}
		if change.CodeChanged() {
			layout.code = true
			addressPairs += 2
			fixedBytes += vtype.CodeHashLen
			codeBytes += len(change.Account.Code)
		}
		accounts[i] = layout
	}
	pairCount := addressPairs + len(changes.Storage)
	return &flatKVChangeSetBuilder{
		pairs:             make([]proto.KVPair, pairCount),
		pairPtrs:          make([]*proto.KVPair, pairCount),
		keys:              make([]byte, addressPairs*flatKVAddressKeyLen+len(changes.Storage)*flatKVStorageKeyLen),
		fixedValues:       make([]byte, fixedBytes+len(changes.Storage)*common.HashLength),
		codeValues:        make([]byte, codeBytes),
		accounts:          accounts,
		addressPairs:      addressPairs,
		accountFixedBytes: fixedBytes,
	}, nil
}

// fill encodes every account and storage change into its place.
func (b *flatKVChangeSetBuilder) fill(changes StateChangeSet) {
	total := len(changes.Accounts) + len(changes.Storage)
	if total < 2*flatKVParallelEncodeChunk {
		b.encodeRange(changes, 0, total)
		return
	}
	var claimed atomic.Int64
	var wg sync.WaitGroup
	workers := min(runtime.GOMAXPROCS(0), (total+flatKVParallelEncodeChunk-1)/flatKVParallelEncodeChunk)
	wg.Add(workers)
	for range workers {
		go func() {
			defer wg.Done()
			for {
				start := int(claimed.Add(flatKVParallelEncodeChunk)) - flatKVParallelEncodeChunk
				if start >= total {
					return
				}
				b.encodeRange(changes, start, min(start+flatKVParallelEncodeChunk, total))
			}
		}()
	}
	wg.Wait()
}

// encodeRange encodes changes [start, end) of the block's account and storage changes, taken in that
// order.
func (b *flatKVChangeSetBuilder) encodeRange(changes StateChangeSet, start, end int) {
	accounts := len(changes.Accounts)
	for i := start; i < end; i++ {
		if i < accounts {
			b.encodeAccount(&changes.Accounts[i], b.accounts[i])
		} else {
			b.encodeStorage(i-accounts, changes.Storage[i-accounts])
		}
	}
}

func (b *flatKVChangeSetBuilder) encodeAccount(change *AccountChange, layout flatKVAccountLayout) {
	pairAt, fixedAt := layout.pairAt, layout.fixedAt
	if layout.balance {
		pair := b.addressPair(pairAt, keys.EVMKeyBalance, change.Address)
		pairAt++
		if balance := change.Account.Balance; balance == nil || balance.Sign() == 0 {
			pair.Delete = true
		} else {
			pair.Value = b.fixedValue(fixedAt, vtype.BalanceLen)
			balance.FillBytes(pair.Value)
		}
		fixedAt += vtype.BalanceLen
	}
	if layout.nonce {
		pair := b.addressPair(pairAt, keys.EVMKeyNonce, change.Address)
		pairAt++
		pair.Value = b.fixedValue(fixedAt, vtype.NonceLen)
		binary.BigEndian.PutUint64(pair.Value, change.Account.Nonce)
		fixedAt += vtype.NonceLen
	}
	if layout.code {
		code := change.Account.Code
		codeHashPair := b.addressPair(pairAt, keys.EVMKeyCodeHash, change.Address)
		codePair := b.addressPair(pairAt+1, keys.EVMKeyCode, change.Address)
		if len(code) == 0 {
			codeHashPair.Delete = true
			codePair.Delete = true
			return
		}
		codeHashPair.Value = b.fixedValue(fixedAt, vtype.CodeHashLen)
		codeHash := crypto.Keccak256Hash(code)
		copy(codeHashPair.Value, codeHash[:])
		codePair.Value = b.codeValues[layout.codeAt : layout.codeAt+len(code) : layout.codeAt+len(code)]
		copy(codePair.Value, code)
	}
}

func (b *flatKVChangeSetBuilder) encodeStorage(s int, change StorageChange) {
	index := b.addressPairs + s
	pair := &b.pairs[index]
	b.pairPtrs[index] = pair
	keyStart := b.addressPairs*flatKVAddressKeyLen + s*flatKVStorageKeyLen
	pair.Key = b.keys[keyStart : keyStart+flatKVStorageKeyLen : keyStart+flatKVStorageKeyLen]
	if !keys.PutEVMKey(pair.Key, keys.EVMKeyStorage, change.Address[:], change.Key[:]) {
		panic("no EVM key prefix for storage")
	}
	if change.Delete || change.Value == (common.Hash{}) {
		pair.Delete = true
		return
	}
	pair.Value = b.fixedValue(b.accountFixedBytes+s*common.HashLength, common.HashLength)
	copy(pair.Value, change.Value[:])
}

func (b *flatKVChangeSetBuilder) addressPair(index int, kind keys.EVMKeyKind, address common.Address) *proto.KVPair {
	pair := &b.pairs[index]
	b.pairPtrs[index] = pair
	keyStart := index * flatKVAddressKeyLen
	pair.Key = b.keys[keyStart : keyStart+flatKVAddressKeyLen : keyStart+flatKVAddressKeyLen]
	if !keys.PutEVMKey(pair.Key, kind, address[:]) {
		panic(fmt.Sprintf("no EVM key prefix for kind %v", kind))
	}
	return pair
}

func (b *flatKVChangeSetBuilder) fixedValue(offset int, size int) []byte {
	return b.fixedValues[offset : offset+size : offset+size]
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
