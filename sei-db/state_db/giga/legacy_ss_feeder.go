package giga

import (
	"encoding/binary"
	"fmt"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"

	"github.com/sei-protocol/sei-chain/sei-db/common/keys"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	gigatypes "github.com/sei-protocol/sei-chain/sei-db/state_db/giga/types"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/vtype"
)

// The number of committed blocks the feeder holds before a commit waits for it.
const legacySSFeederQueueDepth = 4

const (
	legacyAddressKeyLen = 1 + common.AddressLength
	legacyStorageKeyLen = 1 + common.AddressLength + common.HashLength
)

// legacySSFeeder hands each committed block to the EVM state store on a goroutine of its own, converting the block's
// state change set into the legacy changesets the store takes. Blocks reach the store in the order they were fed.
//
// It is a temporary mitigation that moves the conversion off the commit path. Delete it, along with the encoder in
// this file, once the EVM state store no longer requires []*proto.NamedChangeSet.
type legacySSFeeder struct {
	// The store blocks are handed to.
	ss legacySSStore
	// Blocks waiting to be handed over. Closed by Close().
	blocks chan legacySSBlock
	// Closed by the goroutine when it stops on a failure, after it sets err.
	failed chan struct{}
	// The failure the goroutine stopped on. Read only after failed is closed.
	err error
	// Closed by the goroutine when it exits.
	done chan struct{}
}

// legacySSStore is the part of the EVM state store the feeder hands blocks to.
type legacySSStore interface {
	// CommitBlock records a committed block at version.
	CommitBlock(version int64, changesets []*proto.NamedChangeSet) error
}

// legacySSBlock is one committed block waiting to be handed to the EVM state store.
type legacySSBlock struct {
	// The block's height.
	blockNum int64
	// The block's state changes. Never modified.
	changes *gigatypes.StateChangeSet
}

// newLegacySSFeeder returns a feeder handing blocks to ss, with its goroutine running.
func newLegacySSFeeder(ss legacySSStore) *legacySSFeeder {
	f := &legacySSFeeder{
		ss:     ss,
		blocks: make(chan legacySSBlock, legacySSFeederQueueDepth),
		failed: make(chan struct{}),
		done:   make(chan struct{}),
	}
	go f.run()
	return f
}

// Feed queues a committed block for the EVM state store, waiting while the queue is full. Returns an error once
// handing over any earlier block has failed.
func (f *legacySSFeeder) Feed(blockNum int64, changes *gigatypes.StateChangeSet) error {
	select {
	case <-f.failed:
		return f.err
	default:
	}
	select {
	case f.blocks <- legacySSBlock{blockNum: blockNum, changes: changes}:
		return nil
	case <-f.failed:
		return f.err
	}
}

// Close hands every queued block to the EVM state store, then stops the goroutine. Returns the failure the goroutine
// stopped on, if any. Must not be called concurrently with Feed().
func (f *legacySSFeeder) Close() error {
	close(f.blocks)
	<-f.done
	select {
	case <-f.failed:
		return f.err
	default:
		return nil
	}
}

// run hands queued blocks to the EVM state store until the queue closes or a block fails.
func (f *legacySSFeeder) run() {
	defer close(f.done)
	for block := range f.blocks {
		if err := f.handOver(block); err != nil {
			f.err = err
			close(f.failed)
			return
		}
	}
}

// handOver converts one block to legacy changesets and commits it to the EVM state store.
func (f *legacySSFeeder) handOver(block legacySSBlock) error {
	changesets := encodeLegacyChangeSets(block.changes)
	if err := f.ss.CommitBlock(block.blockNum, changesets); err != nil {
		return fmt.Errorf("commit block %d to the EVM state store: %w", block.blockNum, err)
	}
	return nil
}

// encodeLegacyChangeSets converts a state change set into legacy changesets: one over the EVM module holding each
// changed account field, code and storage slot, then one per module the misc changes write. Balances must already be
// known to fit in 256 bits.
func encodeLegacyChangeSets(changes *gigatypes.StateChangeSet) []*proto.NamedChangeSet {
	b := newLegacyChangeSetBuilder(changes)

	for _, change := range changes.Accounts {
		if change.BalanceChanged() {
			balance := change.Account.Balance
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

	var changeSets []*proto.NamedChangeSet
	if len(b.pairPtrs) != 0 {
		changeSets = append(changeSets, &proto.NamedChangeSet{
			Name:      keys.EVMStoreKey,
			Changeset: proto.ChangeSet{Pairs: b.pairPtrs},
		})
	}
	return append(changeSets, miscChangeSets(changes.Misc)...)
}

// miscChangeSets returns one changeset per module the misc changes write, in the order each module first appears.
func miscChangeSets(misc []gigatypes.MiscChange) []*proto.NamedChangeSet {
	var changeSets []*proto.NamedChangeSet
	byModule := make(map[string]*proto.NamedChangeSet)
	for _, change := range misc {
		changeSet, ok := byModule[change.Module]
		if !ok {
			changeSet = &proto.NamedChangeSet{Name: change.Module}
			byModule[change.Module] = changeSet
			changeSets = append(changeSets, changeSet)
		}
		pair := &proto.KVPair{Key: change.Key, Delete: change.Delete}
		if !change.Delete {
			pair.Value = change.Value
		}
		changeSet.Changeset.Pairs = append(changeSet.Changeset.Pairs, pair)
	}
	return changeSets
}

// legacyChangeSetBuilder assembles a block's EVM KVPairs, keys and values from per-call slabs. Keys and values are
// subslices of those slabs and outlive the changeset.
type legacyChangeSetBuilder struct {
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

func newLegacyChangeSetBuilder(changes *gigatypes.StateChangeSet) *legacyChangeSetBuilder {
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
	return &legacyChangeSetBuilder{
		pairs:       make([]proto.KVPair, pairCount),
		pairPtrs:    make([]*proto.KVPair, 0, pairCount),
		keys:        make([]byte, addressPairs*legacyAddressKeyLen+len(changes.Storage)*legacyStorageKeyLen),
		fixedValues: make([]byte, fixedValueBytes),
		codeValues:  make([]byte, codeValueBytes),
	}
}

func (b *legacyChangeSetBuilder) addAddressPair(kind keys.EVMKeyKind, address common.Address) *proto.KVPair {
	pair := b.nextPair(legacyAddressKeyLen)
	if !keys.PutEVMKey(pair.Key, kind, address[:]) {
		panic(fmt.Sprintf("no EVM key prefix for kind %v", kind))
	}
	return pair
}

func (b *legacyChangeSetBuilder) addStoragePair(address common.Address, slot common.Hash) *proto.KVPair {
	pair := b.nextPair(legacyStorageKeyLen)
	if !keys.PutEVMKey(pair.Key, keys.EVMKeyStorage, address[:], slot[:]) {
		panic("no EVM key prefix for storage")
	}
	return pair
}

func (b *legacyChangeSetBuilder) nextPair(keyLen int) *proto.KVPair {
	pair := &b.pairs[b.pairOffset]
	b.pairOffset++
	b.pairPtrs = append(b.pairPtrs, pair)
	pair.Key = b.keys[b.keyOffset : b.keyOffset+keyLen : b.keyOffset+keyLen]
	b.keyOffset += keyLen
	return pair
}

func (b *legacyChangeSetBuilder) takeFixedValue(size int) []byte {
	value := b.fixedValues[b.fixedOffset : b.fixedOffset+size : b.fixedOffset+size]
	b.fixedOffset += size
	return value
}

func (b *legacyChangeSetBuilder) takeCodeValue(size int) []byte {
	value := b.codeValues[b.codeOffset : b.codeOffset+size : b.codeOffset+size]
	b.codeOffset += size
	return value
}
