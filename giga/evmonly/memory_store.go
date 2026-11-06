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
	gigatypes "github.com/sei-protocol/sei-chain/sei-db/state_db/giga/types"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/lthash"
)

// MemoryStoreChangeSetName identifies MemoryStore's direct key/value format.
const MemoryStoreChangeSetName = "evmonly-memory"

const (
	memoryStoreBalanceKey byte = iota + 1
	memoryStoreNonceKey
	memoryStoreCodeKey
	memoryStoreStorageKeyKind

	memoryStoreAccountKeyLen = 1 + common.AddressLength
	memoryStoreStorageKeyLen = memoryStoreAccountKeyLen + common.HashLength
)

var _ gigatypes.StateDB = (*MemoryStore)(nil)

// MemoryStore adapts an immutable StateReader to the giga StateDB interface. It
// is intended only for tests, not runtime persistence.
// Commits are retained as versioned in-memory overlays so open and historical
// snapshots remain stable without cloning the complete base state per block.
type MemoryStore struct {
	mu sync.RWMutex

	base StateReader

	hasCurrentHeight bool
	currentHeight    int64
	committedHeights map[int64]struct{}

	balances     map[common.Address]*memoryStoreValue[common.Hash]
	nonces       map[common.Address]*memoryStoreValue[uint64]
	code         map[common.Address]*memoryStoreValue[[]byte]
	storage      map[memoryStoreStorageKey]*memoryStoreValue[common.Hash]
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

// NewMemoryStore constructs a giga StateDB backed by source plus in-memory
// committed overlays. Source must remain immutable for the lifetime of the
// store, and its methods must be safe for concurrent calls.
func NewMemoryStore(source StateReader) *MemoryStore {
	if source == nil {
		source = NewMemoryState()
	}
	return &MemoryStore{
		base:             source,
		committedHeights: map[int64]struct{}{},
		balances:         map[common.Address]*memoryStoreValue[common.Hash]{},
		nonces:           map[common.Address]*memoryStoreValue[uint64]{},
		code:             map[common.Address]*memoryStoreValue[[]byte]{},
		storage:          map[memoryStoreStorageKey]*memoryStoreValue[common.Hash]{},
		storageTouch:     map[common.Address]int64{},
	}
}

// EncodeChangeSet converts an executor-native state changeset into the direct
// key/value format MemoryStore stores.
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

	accountKeyCount, codeValueBytes := 0, 0
	fixedValueBytes := len(changes.Storage) * common.HashLength
	for _, change := range changes.Accounts {
		if change.BalanceChanged() {
			accountKeyCount++
			fixedValueBytes += common.HashLength
		}
		if change.NonceChanged() {
			accountKeyCount++
			fixedValueBytes += 8
		}
		if change.CodeChanged() {
			accountKeyCount++
			codeValueBytes += len(change.Account.Code)
		}
	}
	pairCount := accountKeyCount + len(changes.Storage)
	keyBytes := accountKeyCount*memoryStoreAccountKeyLen + len(changes.Storage)*memoryStoreStorageKeyLen

	builder := memoryStoreChangeSetBuilder{
		pairs:       make([]proto.KVPair, pairCount),
		pairPtrs:    make([]*proto.KVPair, pairCount),
		keys:        make([]byte, keyBytes),
		fixedValues: make([]byte, fixedValueBytes),
		codeValues:  make([]byte, codeValueBytes),
	}
	// The EVM-only app hash is taken over these pairs in order, so they are grouped by kind: every
	// balance, then every nonce, then every code, then storage.
	for _, change := range changes.Accounts {
		if !change.BalanceChanged() {
			continue
		}
		pair := builder.addAccountPair(memoryStoreBalanceKey, change.Address, false)
		pair.Value = builder.takeFixedValue(common.HashLength)
		if change.Account.Balance != nil {
			change.Account.Balance.FillBytes(pair.Value)
		}
	}
	for _, change := range changes.Accounts {
		if !change.NonceChanged() {
			continue
		}
		pair := builder.addAccountPair(memoryStoreNonceKey, change.Address, false)
		pair.Value = builder.takeFixedValue(8)
		binary.BigEndian.PutUint64(pair.Value, change.Account.Nonce)
	}
	for _, change := range changes.Accounts {
		if !change.CodeChanged() {
			continue
		}
		code := change.Account.Code
		pair := builder.addAccountPair(memoryStoreCodeKey, change.Address, len(code) == 0)
		if len(code) != 0 {
			pair.Value = builder.takeCodeValue(len(code))
			copy(pair.Value, code)
		}
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

// CommitStateChanges commits a block's state changes. Non-EVM writes are rejected.
func (s *MemoryStore) CommitStateChanges(blockNum int64, changes *StateChangeSet) error {
	if len(changes.Misc) != 0 {
		return fmt.Errorf("memory store cannot hold the %d non-EVM writes of block %d",
			len(changes.Misc), blockNum)
	}
	changesets, err := EncodeMemoryStoreChangeSet(*changes)
	if err != nil {
		return fmt.Errorf("encode block %d for the memory store: %w", blockNum, err)
	}
	return s.commitChangeSets(blockNum, changesets)
}

// commitChangeSets commits a block's changesets in the memory store's key format.
func (s *MemoryStore) commitChangeSets(blockNum int64, changesets []*proto.NamedChangeSet) error {
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
	balances int
	nonces   int
	code     int
	storage  int
}

func (c *memoryStorePairCounts) add(kind byte) {
	switch kind {
	case memoryStoreBalanceKey:
		c.balances++
	case memoryStoreNonceKey:
		c.nonces++
	case memoryStoreCodeKey:
		c.code++
	case memoryStoreStorageKeyKind:
		c.storage++
	}
}

type memoryStoreCommitBuffers struct {
	balances []memoryStoreValue[common.Hash]
	nonces   []memoryStoreValue[uint64]
	code     []memoryStoreValue[[]byte]
	storage  []memoryStoreValue[common.Hash]

	balanceOffset int
	nonceOffset   int
	codeOffset    int
	storageOffset int
}

func newMemoryStoreCommitBuffers(counts memoryStorePairCounts) memoryStoreCommitBuffers {
	return memoryStoreCommitBuffers{
		balances: make([]memoryStoreValue[common.Hash], counts.balances),
		nonces:   make([]memoryStoreValue[uint64], counts.nonces),
		code:     make([]memoryStoreValue[[]byte], counts.code),
		storage:  make([]memoryStoreValue[common.Hash], counts.storage),
	}
}

func (s *MemoryStore) applyPairLocked(height int64, pair *proto.KVPair, buffers *memoryStoreCommitBuffers) {
	address := common.Address(pair.Key[1:memoryStoreAccountKeyLen])
	switch pair.Key[0] {
	case memoryStoreBalanceKey:
		node := &buffers.balances[buffers.balanceOffset]
		buffers.balanceOffset++
		node.height = height
		node.value = common.Hash(pair.Value)
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
			node.value = common.Hash(pair.Value)
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

func (s *MemoryStore) OpenView() gigatypes.StateView {
	s.mu.RLock()
	height := int64(0)
	if s.hasCurrentHeight {
		height = s.currentHeight
	}
	s.mu.RUnlock()
	return &memoryStoreSnapshot{store: s, height: height}
}

func (s *MemoryStore) OpenViewAt(blockNum int64) (gigatypes.StateView, bool) {
	s.mu.RLock()
	_, ok := s.committedHeights[blockNum]
	s.mu.RUnlock()
	if !ok {
		return nil, false
	}
	return &memoryStoreSnapshot{store: s, height: blockNum}, true
}

// RegisterHashListener reports that this store hashes nothing. It keeps state in maps rather than
// in a lattice, so there is no block hash for a listener to be given.
func (s *MemoryStore) RegisterHashListener(_ gigatypes.HashListener) (lthash.BlockHash, error) {
	return lthash.BlockHash{}, fmt.Errorf("evmonly: an in-memory store computes no block hashes")
}

// Close releases nothing. This store holds no handle outside its own maps, which go with it.
func (s *MemoryStore) Close() error { return nil }

type memoryStoreSnapshot struct {
	store  *MemoryStore
	height int64
	closed atomic.Bool
}

var _ gigatypes.StateView = (*memoryStoreSnapshot)(nil)

func (s *memoryStoreSnapshot) AccountExists(address gigatypes.Address) bool {
	s.requireOpen()
	s.store.mu.RLock()
	_, balanceTouched := latestMemoryStoreValue(s.store.balances[address], s.height)
	_, nonceTouched := latestMemoryStoreValue(s.store.nonces[address], s.height)
	_, codeTouched := latestMemoryStoreValue(s.store.code[address], s.height)
	firstStorageTouch, storageTouched := s.store.storageTouch[address]
	s.store.mu.RUnlock()
	if balanceTouched || nonceTouched || codeTouched || storageTouched && firstStorageTouch <= s.height {
		return true
	}
	if source, ok := s.store.base.(interface{ AccountExists(common.Address) bool }); ok {
		return source.AccountExists(address)
	}
	balance := s.store.base.GetBalance(address)
	return balance != nil && balance.Sign() != 0 || s.store.base.GetNonce(address) != 0 || len(s.store.base.GetCode(address)) != 0
}

func (s *memoryStoreSnapshot) GetStorage(address gigatypes.Address, slot gigatypes.Hash) gigatypes.Hash {
	s.requireOpen()
	key := memoryStoreStorageKey{address: address, slot: slot}
	s.store.mu.RLock()
	value, valueOK := latestMemoryStoreValue(s.store.storage[key], s.height)
	s.store.mu.RUnlock()
	if valueOK {
		if value.delete {
			return gigatypes.Hash{}
		}
		return value.value
	}
	return s.store.base.GetState(address, slot)
}

func (s *memoryStoreSnapshot) GetBalance(address gigatypes.Address) gigatypes.Hash {
	s.requireOpen()
	s.store.mu.RLock()
	value, ok := latestMemoryStoreValue(s.store.balances[address], s.height)
	s.store.mu.RUnlock()
	if ok {
		return value.value
	}
	var balance common.Hash
	baseBalance := s.store.base.GetBalance(address)
	if baseBalance != nil {
		if err := validateMemoryStoreBalance(baseBalance); err != nil {
			panic(err)
		}
		baseBalance.FillBytes(balance[:])
	}
	return balance
}

func (s *memoryStoreSnapshot) GetNonce(address gigatypes.Address) uint64 {
	s.requireOpen()
	s.store.mu.RLock()
	value, ok := latestMemoryStoreValue(s.store.nonces[address], s.height)
	s.store.mu.RUnlock()
	if ok {
		return value.value
	}
	return s.store.base.GetNonce(address)
}

func (s *memoryStoreSnapshot) GetCodeSize(address gigatypes.Address) int {
	return len(s.GetCode(address))
}

func (s *memoryStoreSnapshot) GetCodeHash(address gigatypes.Address) gigatypes.Hash {
	s.requireOpen()
	if !s.AccountExists(address) {
		return gigatypes.Hash{}
	}
	return crypto.Keccak256Hash(s.GetCode(address))
}

func (s *memoryStoreSnapshot) GetCode(address gigatypes.Address) []byte {
	s.requireOpen()
	s.store.mu.RLock()
	value, ok := latestMemoryStoreValue(s.store.code[address], s.height)
	s.store.mu.RUnlock()
	if ok {
		if value.delete {
			return nil
		}
		return cloneBytes(value.value)
	}
	return cloneBytes(s.store.base.GetCode(address))
}

func (s *memoryStoreSnapshot) ReadAccount(address gigatypes.Address) (gigatypes.Account, bool) {
	if !s.AccountExists(address) {
		return gigatypes.Account{}, false
	}
	return gigatypes.Account{
		Balance:  s.GetBalance(address),
		Nonce:    s.GetNonce(address),
		CodeHash: crypto.Keccak256Hash(s.GetCode(address)),
	}, true
}

func (s *memoryStoreSnapshot) GetBlockHeight() int64 {
	s.requireOpen()
	return s.height
}

func (s *memoryStoreSnapshot) Get(module string, key []byte) ([]byte, bool) {
	s.requireOpen()
	if module != MemoryStoreChangeSetName || len(key) == 0 {
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
		s.store.mu.RUnlock()
		if !valueOK || value.delete {
			return nil, false
		}
		encoded := make([]byte, common.HashLength)
		copy(encoded, value.value[:])
		return encoded, true
	default:
		return nil, false
	}
}

// GetRaw panics: a memory store snapshot holds no physical rows.
func (s *memoryStoreSnapshot) GetRaw([]byte) ([]byte, bool) {
	panic("memory store snapshot holds no physical rows")
}

// BatchGetRaw panics: a memory store snapshot holds no physical rows.
func (s *memoryStoreSnapshot) BatchGetRaw([][]byte) map[string][]byte {
	panic("memory store snapshot holds no physical rows")
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
	for i, change := range changes.Accounts {
		if err := validateMemoryStoreBalance(change.Account.Balance); err != nil {
			return fmt.Errorf("memory store account change %d: %w", i, err)
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
