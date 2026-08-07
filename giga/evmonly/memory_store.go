package evmonly

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math/big"
	"sync"
	"sync/atomic"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"

	"github.com/sei-protocol/sei-chain/sei-db/proto"
	gigastore "github.com/sei-protocol/sei-chain/sei-db/state_db/giga"
)

// MemoryStoreChangeSetName identifies MemoryStore's direct key/value format.
const MemoryStoreChangeSetName = "evmonly-memory"

const (
	memoryStoreBalanceKey byte = iota + 1
	memoryStoreNonceKey
	memoryStoreCodeKey
	memoryStoreStorageClearKey
	memoryStoreStorageKeyKind

	memoryStoreAccountKeyLen = 1 + common.AddressLength
	memoryStoreStorageKeyLen = memoryStoreAccountKeyLen + common.HashLength
)

var _ gigastore.Store = (*MemoryStore)(nil)

// MemoryStore adapts an immutable StateReader to the giga Store interface. It
// is intended for tests and load generation, not production persistence.
// Commits are retained as versioned in-memory overlays so open and historical
// snapshots remain stable without cloning the complete base state per block.
type MemoryStore struct {
	mu sync.RWMutex

	base StateReader

	hasCurrentHeight bool
	currentHeight    int64
	committedHeights map[int64]struct{}

	balances     map[common.Address]*memoryStoreValue[gigastore.Hash]
	nonces       map[common.Address]*memoryStoreValue[uint64]
	code         map[common.Address]*memoryStoreValue[[]byte]
	storage      map[memoryStoreStorageKey]*memoryStoreValue[gigastore.Hash]
	storageClear map[common.Address]*memoryStoreValue[struct{}]
	storageTouch map[common.Address]int64
}

type memoryStoreValue[T any] struct {
	height   int64
	value    T
	delete   bool
	previous *memoryStoreValue[T]
}

type memoryStoreStorageKey struct {
	address common.Address
	slot    common.Hash
}

// NewMemoryStore constructs a giga Store backed by source plus in-memory
// committed overlays. Source must remain immutable for the lifetime of the
// store, and its methods must be safe for concurrent calls.
func NewMemoryStore(source StateReader) *MemoryStore {
	if source == nil {
		source = NewMemoryState()
	}
	return &MemoryStore{
		base:             source,
		committedHeights: map[int64]struct{}{},
		balances:         map[common.Address]*memoryStoreValue[gigastore.Hash]{},
		nonces:           map[common.Address]*memoryStoreValue[uint64]{},
		code:             map[common.Address]*memoryStoreValue[[]byte]{},
		storage:          map[memoryStoreStorageKey]*memoryStoreValue[gigastore.Hash]{},
		storageClear:     map[common.Address]*memoryStoreValue[struct{}]{},
		storageTouch:     map[common.Address]int64{},
	}
}

// EncodeChangeSet converts an executor-native state changeset into the direct
// key/value format consumed by MemoryStore.CommitStateChanges.
func (s *MemoryStore) EncodeChangeSet(changes StateChangeSet) ([]*proto.NamedChangeSet, error) {
	return EncodeMemoryStoreChangeSet(changes)
}

// EncodeMemoryStoreChangeSet converts an executor-native state changeset into
// MemoryStore's direct key/value format. The returned byte slices own their
// storage and remain valid after the input changeset is released or reused.
func EncodeMemoryStoreChangeSet(changes StateChangeSet) ([]*proto.NamedChangeSet, error) {
	if err := validateMemoryStoreChangeSet(changes); err != nil {
		return nil, err
	}

	pairCount := len(changes.Balances) + len(changes.Nonces) + len(changes.Code) + len(changes.StorageClears) + len(changes.Storage)
	accountKeyCount := len(changes.Balances) + len(changes.Nonces) + len(changes.Code) + len(changes.StorageClears)
	keyBytes := accountKeyCount*memoryStoreAccountKeyLen + len(changes.Storage)*memoryStoreStorageKeyLen
	fixedValueBytes := len(changes.Balances)*common.HashLength + len(changes.Nonces)*8 + len(changes.Storage)*common.HashLength
	codeValueBytes := 0
	for _, change := range changes.Code {
		if !change.Delete {
			codeValueBytes += len(change.Code)
		}
	}

	builder := memoryStoreChangeSetBuilder{
		pairs:       make([]proto.KVPair, pairCount),
		pairPtrs:    make([]*proto.KVPair, pairCount),
		keys:        make([]byte, keyBytes),
		fixedValues: make([]byte, fixedValueBytes),
		codeValues:  make([]byte, codeValueBytes),
	}
	for _, change := range changes.Balances {
		pair := builder.addAccountPair(memoryStoreBalanceKey, change.Address, false)
		pair.Value = builder.takeFixedValue(common.HashLength)
		if change.Balance != nil {
			change.Balance.FillBytes(pair.Value)
		}
	}
	for _, change := range changes.Nonces {
		pair := builder.addAccountPair(memoryStoreNonceKey, change.Address, false)
		pair.Value = builder.takeFixedValue(8)
		binary.BigEndian.PutUint64(pair.Value, change.Nonce)
	}
	for _, change := range changes.Code {
		pair := builder.addAccountPair(memoryStoreCodeKey, change.Address, change.Delete)
		if !change.Delete {
			pair.Value = builder.takeCodeValue(len(change.Code))
			copy(pair.Value, change.Code)
		}
	}
	for _, address := range changes.StorageClears {
		builder.addAccountPair(memoryStoreStorageClearKey, address, false)
	}
	for _, change := range changes.Storage {
		pair := builder.addStoragePair(change.Address, change.Key, change.Delete)
		if !change.Delete {
			pair.Value = builder.takeFixedValue(common.HashLength)
			copy(pair.Value, change.Value[:])
		}
	}

	return []*proto.NamedChangeSet{{
		Name:      MemoryStoreChangeSetName,
		Changeset: proto.ChangeSet{Pairs: builder.pairPtrs},
	}}, nil
}

type memoryStoreChangeSetBuilder struct {
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

func (b *memoryStoreChangeSetBuilder) addAccountPair(kind byte, address common.Address, deleteValue bool) *proto.KVPair {
	pair := b.nextPair(memoryStoreAccountKeyLen)
	pair.Delete = deleteValue
	pair.Key[0] = kind
	copy(pair.Key[1:], address[:])
	return pair
}

func (b *memoryStoreChangeSetBuilder) addStoragePair(address common.Address, slot common.Hash, deleteValue bool) *proto.KVPair {
	pair := b.nextPair(memoryStoreStorageKeyLen)
	pair.Delete = deleteValue
	pair.Key[0] = memoryStoreStorageKeyKind
	copy(pair.Key[1:memoryStoreAccountKeyLen], address[:])
	copy(pair.Key[memoryStoreAccountKeyLen:], slot[:])
	return pair
}

func (b *memoryStoreChangeSetBuilder) nextPair(keyLen int) *proto.KVPair {
	pair := &b.pairs[b.pairOffset]
	b.pairPtrs[b.pairOffset] = pair
	b.pairOffset++
	pair.Key = b.keys[b.keyOffset : b.keyOffset+keyLen]
	b.keyOffset += keyLen
	return pair
}

func (b *memoryStoreChangeSetBuilder) takeFixedValue(size int) []byte {
	value := b.fixedValues[b.fixedOffset : b.fixedOffset+size]
	b.fixedOffset += size
	return value
}

func (b *memoryStoreChangeSetBuilder) takeCodeValue(size int) []byte {
	value := b.codeValues[b.codeOffset : b.codeOffset+size]
	b.codeOffset += size
	return value
}

func (s *MemoryStore) CommitStateChanges(blockNum int64, changesets []*proto.NamedChangeSet) error {
	if blockNum < 0 {
		return fmt.Errorf("memory store block number must be non-negative: %d", blockNum)
	}

	var counts memoryStorePairCounts
	for changesetIndex, named := range changesets {
		if named == nil {
			return fmt.Errorf("memory store changeset %d is nil", changesetIndex)
		}
		if named.Name != MemoryStoreChangeSetName {
			return fmt.Errorf("memory store changeset %d has unsupported name %q", changesetIndex, named.Name)
		}
		for pairIndex, pair := range named.Changeset.Pairs {
			if pair == nil {
				return fmt.Errorf("memory store changeset %d pair %d is nil", changesetIndex, pairIndex)
			}
			if err := validateMemoryStorePair(pair); err != nil {
				return fmt.Errorf("memory store changeset %d pair %d: %w", changesetIndex, pairIndex, err)
			}
			counts.add(pair.Key[0])
		}
	}
	buffers := newMemoryStoreCommitBuffers(counts)

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.hasCurrentHeight && blockNum <= s.currentHeight {
		return fmt.Errorf("memory store block number %d is not after current height %d", blockNum, s.currentHeight)
	}
	for _, named := range changesets {
		for _, pair := range named.Changeset.Pairs {
			s.applyPairLocked(blockNum, pair, &buffers)
		}
	}
	s.currentHeight = blockNum
	s.hasCurrentHeight = true
	s.committedHeights[blockNum] = struct{}{}
	return nil
}

type memoryStorePairCounts struct {
	balances     int
	nonces       int
	code         int
	storageClear int
	storage      int
}

func (c *memoryStorePairCounts) add(kind byte) {
	switch kind {
	case memoryStoreBalanceKey:
		c.balances++
	case memoryStoreNonceKey:
		c.nonces++
	case memoryStoreCodeKey:
		c.code++
	case memoryStoreStorageClearKey:
		c.storageClear++
	case memoryStoreStorageKeyKind:
		c.storage++
	}
}

type memoryStoreCommitBuffers struct {
	balances     []memoryStoreValue[gigastore.Hash]
	nonces       []memoryStoreValue[uint64]
	code         []memoryStoreValue[[]byte]
	storageClear []memoryStoreValue[struct{}]
	storage      []memoryStoreValue[gigastore.Hash]

	balanceOffset      int
	nonceOffset        int
	codeOffset         int
	storageClearOffset int
	storageOffset      int
}

func newMemoryStoreCommitBuffers(counts memoryStorePairCounts) memoryStoreCommitBuffers {
	return memoryStoreCommitBuffers{
		balances:     make([]memoryStoreValue[gigastore.Hash], counts.balances),
		nonces:       make([]memoryStoreValue[uint64], counts.nonces),
		code:         make([]memoryStoreValue[[]byte], counts.code),
		storageClear: make([]memoryStoreValue[struct{}], counts.storageClear),
		storage:      make([]memoryStoreValue[gigastore.Hash], counts.storage),
	}
}

func (s *MemoryStore) applyPairLocked(height int64, pair *proto.KVPair, buffers *memoryStoreCommitBuffers) {
	address := common.Address(pair.Key[1:memoryStoreAccountKeyLen])
	switch pair.Key[0] {
	case memoryStoreBalanceKey:
		node := &buffers.balances[buffers.balanceOffset]
		buffers.balanceOffset++
		node.height = height
		node.value = gigastore.Hash(pair.Value)
		node.previous = s.balances[address]
		s.balances[address] = node
	case memoryStoreNonceKey:
		node := &buffers.nonces[buffers.nonceOffset]
		buffers.nonceOffset++
		node.height = height
		node.value = binary.BigEndian.Uint64(pair.Value)
		node.previous = s.nonces[address]
		s.nonces[address] = node
	case memoryStoreCodeKey:
		node := &buffers.code[buffers.codeOffset]
		buffers.codeOffset++
		node.height = height
		node.value = pair.Value
		node.delete = pair.Delete
		node.previous = s.code[address]
		s.code[address] = node
	case memoryStoreStorageClearKey:
		node := &buffers.storageClear[buffers.storageClearOffset]
		buffers.storageClearOffset++
		node.height = height
		node.previous = s.storageClear[address]
		s.storageClear[address] = node
		s.touchStorageAccountLocked(height, address)
	case memoryStoreStorageKeyKind:
		key := memoryStoreStorageKey{
			address: address,
			slot:    common.Hash(pair.Key[memoryStoreAccountKeyLen:]),
		}
		node := &buffers.storage[buffers.storageOffset]
		buffers.storageOffset++
		node.height = height
		node.delete = pair.Delete
		if !pair.Delete {
			node.value = gigastore.Hash(pair.Value)
		}
		node.previous = s.storage[key]
		s.storage[key] = node
		s.touchStorageAccountLocked(height, address)
	}
}

func (s *MemoryStore) touchStorageAccountLocked(height int64, address common.Address) {
	if _, touched := s.storageTouch[address]; !touched {
		s.storageTouch[address] = height
	}
}

func (s *MemoryStore) OpenSnapshot() gigastore.StateSnapshot {
	s.mu.RLock()
	height := int64(0)
	if s.hasCurrentHeight {
		height = s.currentHeight
	}
	s.mu.RUnlock()
	return &memoryStoreSnapshot{store: s, height: height}
}

func (s *MemoryStore) OpenSnapshotAt(blockNum int64) (gigastore.StateSnapshot, bool) {
	s.mu.RLock()
	_, ok := s.committedHeights[blockNum]
	s.mu.RUnlock()
	if !ok {
		return nil, false
	}
	return &memoryStoreSnapshot{store: s, height: blockNum}, true
}

type memoryStoreSnapshot struct {
	store  *MemoryStore
	height int64
	closed atomic.Bool
}

var _ gigastore.StateSnapshot = (*memoryStoreSnapshot)(nil)

func (s *memoryStoreSnapshot) AccountExists(address gigastore.Address) bool {
	s.requireOpen()
	addr := common.Address(address)
	s.store.mu.RLock()
	_, balanceTouched := latestMemoryStoreValue(s.store.balances[addr], s.height)
	_, nonceTouched := latestMemoryStoreValue(s.store.nonces[addr], s.height)
	_, codeTouched := latestMemoryStoreValue(s.store.code[addr], s.height)
	firstStorageTouch, storageTouched := s.store.storageTouch[addr]
	s.store.mu.RUnlock()
	if balanceTouched || nonceTouched || codeTouched || storageTouched && firstStorageTouch <= s.height {
		return true
	}
	if source, ok := s.store.base.(interface{ AccountExists(common.Address) bool }); ok {
		return source.AccountExists(addr)
	}
	balance := s.store.base.GetBalance(addr)
	return balance != nil && balance.Sign() != 0 || s.store.base.GetNonce(addr) != 0 || len(s.store.base.GetCode(addr)) != 0
}

func (s *memoryStoreSnapshot) GetStorage(address gigastore.Address, slot gigastore.Hash) gigastore.Hash {
	s.requireOpen()
	addr := common.Address(address)
	key := memoryStoreStorageKey{address: addr, slot: common.Hash(slot)}
	s.store.mu.RLock()
	value, valueOK := latestMemoryStoreValue(s.store.storage[key], s.height)
	clearHeight, clearOK := latestHeightAt(s.store.storageClear[addr], s.height)
	s.store.mu.RUnlock()
	if valueOK && (!clearOK || value.height >= clearHeight) {
		if value.delete {
			return gigastore.Hash{}
		}
		return value.value
	}
	if clearOK {
		return gigastore.Hash{}
	}
	return gigastore.Hash(s.store.base.GetState(addr, common.Hash(slot)))
}

func (s *memoryStoreSnapshot) GetBalance(address gigastore.Address) gigastore.Hash {
	s.requireOpen()
	addr := common.Address(address)
	s.store.mu.RLock()
	value, ok := latestMemoryStoreValue(s.store.balances[addr], s.height)
	s.store.mu.RUnlock()
	if ok {
		return value.value
	}
	var balance gigastore.Hash
	baseBalance := s.store.base.GetBalance(addr)
	if baseBalance != nil {
		if err := validateMemoryStoreBalance(baseBalance); err != nil {
			panic(err)
		}
		baseBalance.FillBytes(balance[:])
	}
	return balance
}

func (s *memoryStoreSnapshot) GetNonce(address gigastore.Address) uint64 {
	s.requireOpen()
	addr := common.Address(address)
	s.store.mu.RLock()
	value, ok := latestMemoryStoreValue(s.store.nonces[addr], s.height)
	s.store.mu.RUnlock()
	if ok {
		return value.value
	}
	return s.store.base.GetNonce(addr)
}

func (s *memoryStoreSnapshot) GetCodeSize(address gigastore.Address) int {
	return len(s.GetCode(address))
}

func (s *memoryStoreSnapshot) GetCodeHash(address gigastore.Address) gigastore.Hash {
	s.requireOpen()
	if !s.AccountExists(address) {
		return gigastore.Hash{}
	}
	return gigastore.Hash(crypto.Keccak256Hash(s.GetCode(address)))
}

func (s *memoryStoreSnapshot) GetCode(address gigastore.Address) []byte {
	s.requireOpen()
	addr := common.Address(address)
	s.store.mu.RLock()
	value, ok := latestMemoryStoreValue(s.store.code[addr], s.height)
	s.store.mu.RUnlock()
	if ok {
		if value.delete {
			return nil
		}
		return cloneBytes(value.value)
	}
	return cloneBytes(s.store.base.GetCode(addr))
}

func (s *memoryStoreSnapshot) GetBlockHeight() int64 {
	s.requireOpen()
	return s.height
}

func (s *memoryStoreSnapshot) Get(key []byte) ([]byte, bool) {
	s.requireOpen()
	if len(key) == 0 {
		return nil, false
	}

	switch key[0] {
	case memoryStoreBalanceKey:
		if len(key) != memoryStoreAccountKeyLen {
			return nil, false
		}
		address := common.Address(key[1:])
		s.store.mu.RLock()
		value, ok := latestMemoryStoreValue(s.store.balances[address], s.height)
		s.store.mu.RUnlock()
		if !ok {
			return nil, false
		}
		encoded := make([]byte, common.HashLength)
		copy(encoded, value.value[:])
		return encoded, true
	case memoryStoreNonceKey:
		if len(key) != memoryStoreAccountKeyLen {
			return nil, false
		}
		address := common.Address(key[1:])
		s.store.mu.RLock()
		value, ok := latestMemoryStoreValue(s.store.nonces[address], s.height)
		s.store.mu.RUnlock()
		if !ok {
			return nil, false
		}
		encoded := make([]byte, 8)
		binary.BigEndian.PutUint64(encoded, value.value)
		return encoded, true
	case memoryStoreCodeKey:
		if len(key) != memoryStoreAccountKeyLen {
			return nil, false
		}
		address := common.Address(key[1:])
		s.store.mu.RLock()
		value, ok := latestMemoryStoreValue(s.store.code[address], s.height)
		s.store.mu.RUnlock()
		if !ok || value.delete {
			return nil, false
		}
		return cloneBytes(value.value), true
	case memoryStoreStorageClearKey:
		if len(key) != memoryStoreAccountKeyLen {
			return nil, false
		}
		address := common.Address(key[1:])
		s.store.mu.RLock()
		_, ok := latestHeightAt(s.store.storageClear[address], s.height)
		s.store.mu.RUnlock()
		if !ok {
			return nil, false
		}
		return []byte{}, true
	case memoryStoreStorageKeyKind:
		if len(key) != memoryStoreStorageKeyLen {
			return nil, false
		}
		storageKey := memoryStoreStorageKey{
			address: common.Address(key[1:memoryStoreAccountKeyLen]),
			slot:    common.Hash(key[memoryStoreAccountKeyLen:]),
		}
		s.store.mu.RLock()
		value, valueOK := latestMemoryStoreValue(s.store.storage[storageKey], s.height)
		clearHeight, clearOK := latestHeightAt(s.store.storageClear[storageKey.address], s.height)
		s.store.mu.RUnlock()
		if !valueOK || value.delete || clearOK && value.height < clearHeight {
			return nil, false
		}
		encoded := make([]byte, common.HashLength)
		copy(encoded, value.value[:])
		return encoded, true
	default:
		return nil, false
	}
}

func (s *memoryStoreSnapshot) Close() {
	s.closed.Store(true)
}

func (s *memoryStoreSnapshot) requireOpen() {
	if s == nil || s.store == nil {
		panic("memory store snapshot is nil")
	}
	if s.closed.Load() {
		panic("memory store snapshot is closed")
	}
}

func latestMemoryStoreValue[T any](value *memoryStoreValue[T], height int64) (*memoryStoreValue[T], bool) {
	for value != nil && value.height > height {
		value = value.previous
	}
	return value, value != nil
}

func latestHeightAt(value *memoryStoreValue[struct{}], height int64) (int64, bool) {
	value, ok := latestMemoryStoreValue(value, height)
	if !ok {
		return 0, false
	}
	return value.height, true
}

func validateMemoryStoreBalance(balance *big.Int) error {
	if balance == nil {
		return nil
	}
	if balance.Sign() < 0 || balance.BitLen() > 256 {
		return errors.New("memory store balance must fit in an unsigned 256-bit integer")
	}
	return nil
}

func validateMemoryStoreChangeSet(changes StateChangeSet) error {
	for i, change := range changes.Balances {
		if err := validateMemoryStoreBalance(change.Balance); err != nil {
			return fmt.Errorf("memory store balance change %d: %w", i, err)
		}
	}
	return nil
}

func validateMemoryStorePair(pair *proto.KVPair) error {
	if len(pair.Key) == 0 {
		return errors.New("key is empty")
	}

	switch pair.Key[0] {
	case memoryStoreBalanceKey:
		if len(pair.Key) != memoryStoreAccountKeyLen {
			return fmt.Errorf("balance key length is %d, want %d", len(pair.Key), memoryStoreAccountKeyLen)
		}
		if pair.Delete {
			return errors.New("balance cannot be deleted")
		}
		if len(pair.Value) != common.HashLength {
			return fmt.Errorf("balance value length is %d, want %d", len(pair.Value), common.HashLength)
		}
	case memoryStoreNonceKey:
		if len(pair.Key) != memoryStoreAccountKeyLen {
			return fmt.Errorf("nonce key length is %d, want %d", len(pair.Key), memoryStoreAccountKeyLen)
		}
		if pair.Delete {
			return errors.New("nonce cannot be deleted")
		}
		if len(pair.Value) != 8 {
			return fmt.Errorf("nonce value length is %d, want 8", len(pair.Value))
		}
	case memoryStoreCodeKey:
		if len(pair.Key) != memoryStoreAccountKeyLen {
			return fmt.Errorf("code key length is %d, want %d", len(pair.Key), memoryStoreAccountKeyLen)
		}
		if pair.Delete && len(pair.Value) != 0 {
			return errors.New("deleted code has a value")
		}
	case memoryStoreStorageClearKey:
		if len(pair.Key) != memoryStoreAccountKeyLen {
			return fmt.Errorf("storage-clear key length is %d, want %d", len(pair.Key), memoryStoreAccountKeyLen)
		}
		if pair.Delete {
			return errors.New("storage-clear marker cannot be deleted")
		}
		if len(pair.Value) != 0 {
			return errors.New("storage-clear marker has a value")
		}
	case memoryStoreStorageKeyKind:
		if len(pair.Key) != memoryStoreStorageKeyLen {
			return fmt.Errorf("storage key length is %d, want %d", len(pair.Key), memoryStoreStorageKeyLen)
		}
		if pair.Delete {
			if len(pair.Value) != 0 {
				return errors.New("deleted storage has a value")
			}
		} else if len(pair.Value) != common.HashLength {
			return fmt.Errorf("storage value length is %d, want %d", len(pair.Value), common.HashLength)
		}
	default:
		return fmt.Errorf("unsupported key kind %d", pair.Key[0])
	}
	return nil
}
