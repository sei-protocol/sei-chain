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
	if err := validateFlatKVBalances(changes.Balances); err != nil {
		return nil, err
	}
	b := newFlatKVChangeSetBuilder(changes)
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

// validateFlatKVBalances reports the first balance that does not fit FlatKV's encoding.
func validateFlatKVBalances(balances []BalanceChange) error {
	for i, change := range balances {
		if err := validateFlatKVBalance(change.Balance); err != nil {
			return fmt.Errorf("balance change %d for %s: %w", i, change.Address, err)
		}
	}
	return nil
}

// flatKVChangeSetBuilder lays out a block's pairs in slabs so changes can be encoded in any order.
type flatKVChangeSetBuilder struct {
	pairs       []proto.KVPair
	pairPtrs    []*proto.KVPair
	keys        []byte
	fixedValues []byte
	codeValues  []byte
	// codeOffsets[k] is where code change k's bytes start in codeValues.
	codeOffsets []int
}

func newFlatKVChangeSetBuilder(changes StateChangeSet) *flatKVChangeSetBuilder {
	addressPairs := len(changes.Balances) + len(changes.Nonces) + 2*len(changes.Code)
	pairCount := addressPairs + len(changes.Storage)
	fixedValueBytes := len(changes.Balances)*vtype.BalanceLen +
		len(changes.Nonces)*vtype.NonceLen +
		len(changes.Code)*vtype.CodeHashLen +
		len(changes.Storage)*common.HashLength
	codeOffsets := make([]int, len(changes.Code))
	codeValueBytes := 0
	for k, change := range changes.Code {
		codeOffsets[k] = codeValueBytes
		if !change.Delete {
			codeValueBytes += len(change.Code)
		}
	}
	return &flatKVChangeSetBuilder{
		pairs:       make([]proto.KVPair, pairCount),
		pairPtrs:    make([]*proto.KVPair, pairCount),
		keys:        make([]byte, addressPairs*flatKVAddressKeyLen+len(changes.Storage)*flatKVStorageKeyLen),
		fixedValues: make([]byte, fixedValueBytes),
		codeValues:  make([]byte, codeValueBytes),
		codeOffsets: codeOffsets,
	}
}

// fill encodes every balance, nonce, code and storage change into its place.
func (b *flatKVChangeSetBuilder) fill(changes StateChangeSet) {
	total := flatKVChangeCount(changes)
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

// flatKVChangeCount is the number of balance, nonce, code and storage changes fill encodes.
func flatKVChangeCount(changes StateChangeSet) int {
	return len(changes.Balances) + len(changes.Nonces) + len(changes.Code) + len(changes.Storage)
}

// encodeRange encodes changes [start, end) of the block's balance, nonce, code and storage changes,
// taken in that order.
func (b *flatKVChangeSetBuilder) encodeRange(changes StateChangeSet, start, end int) {
	balances, nonces, code := len(changes.Balances), len(changes.Nonces), len(changes.Code)
	nonceStart, codeStart, storageStart := balances, balances+nonces, balances+nonces+code
	for i := start; i < end; i++ {
		switch {
		case i < nonceStart:
			b.encodeBalance(i, changes.Balances[i])
		case i < codeStart:
			b.encodeNonce(balances, i-nonceStart, changes.Nonces[i-nonceStart])
		case i < storageStart:
			b.encodeCode(balances, nonces, i-codeStart, changes.Code[i-codeStart])
		default:
			b.encodeStorage(balances, nonces, code, i-storageStart, changes.Storage[i-storageStart])
		}
	}
}

func (b *flatKVChangeSetBuilder) encodeBalance(i int, change BalanceChange) {
	pair := b.addressPair(i, keys.EVMKeyBalance, change.Address)
	if change.Balance == nil || change.Balance.Sign() == 0 {
		pair.Delete = true
		return
	}
	pair.Value = b.fixedValue(i*vtype.BalanceLen, vtype.BalanceLen)
	change.Balance.FillBytes(pair.Value)
}

func (b *flatKVChangeSetBuilder) encodeNonce(balances int, j int, change NonceChange) {
	pair := b.addressPair(balances+j, keys.EVMKeyNonce, change.Address)
	pair.Value = b.fixedValue(balances*vtype.BalanceLen+j*vtype.NonceLen, vtype.NonceLen)
	binary.BigEndian.PutUint64(pair.Value, change.Nonce)
}

func (b *flatKVChangeSetBuilder) encodeCode(balances int, nonces int, k int, change CodeChange) {
	index := balances + nonces + 2*k
	codeHashPair := b.addressPair(index, keys.EVMKeyCodeHash, change.Address)
	codePair := b.addressPair(index+1, keys.EVMKeyCode, change.Address)
	if change.Delete || len(change.Code) == 0 {
		codeHashPair.Delete = true
		codePair.Delete = true
		return
	}
	codeHashPair.Value = b.fixedValue(balances*vtype.BalanceLen+nonces*vtype.NonceLen+k*vtype.CodeHashLen, vtype.CodeHashLen)
	codeHash := crypto.Keccak256Hash(change.Code)
	copy(codeHashPair.Value, codeHash[:])
	offset := b.codeOffsets[k]
	codePair.Value = b.codeValues[offset : offset+len(change.Code) : offset+len(change.Code)]
	copy(codePair.Value, change.Code)
}

func (b *flatKVChangeSetBuilder) encodeStorage(balances int, nonces int, code int, s int, change StorageChange) {
	addressPairs := balances + nonces + 2*code
	pair := &b.pairs[addressPairs+s]
	b.pairPtrs[addressPairs+s] = pair
	keyStart := addressPairs*flatKVAddressKeyLen + s*flatKVStorageKeyLen
	pair.Key = b.keys[keyStart : keyStart+flatKVStorageKeyLen : keyStart+flatKVStorageKeyLen]
	if !keys.PutEVMKey(pair.Key, keys.EVMKeyStorage, change.Address[:], change.Key[:]) {
		panic("no EVM key prefix for storage")
	}
	if change.Delete || change.Value == (common.Hash{}) {
		pair.Delete = true
		return
	}
	pair.Value = b.fixedValue(balances*vtype.BalanceLen+nonces*vtype.NonceLen+code*vtype.CodeHashLen+s*common.HashLength, common.HashLength)
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
