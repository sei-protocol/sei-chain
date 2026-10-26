package evmonly

import (
	"encoding/binary"
	"errors"
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"

	"github.com/ethereum/go-ethereum/common"

	"github.com/sei-protocol/sei-chain/sei-db/common/keys"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	gigatypes "github.com/sei-protocol/sei-chain/sei-db/state_db/giga/types"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/ktype"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/lthash"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/vtype"
)

const (
	// flatKVMemoryShards is the number of independently locked partitions of the row maps.
	flatKVMemoryShards = 256
	// flatKVMemoryRowRetention is the number of versions kept per row, which bounds how far behind
	// the head a view may be opened and how many commits a row may take while a view reads it.
	flatKVMemoryRowRetention = 64
)

var _ gigatypes.StateDB = (*FlatKVMemoryStore)(nil)

// FlatKVMemoryStore is a giga StateDB that keeps FlatKV's serialized rows in process memory. It
// consumes the changesets NewFlatKVChangeSetEncoder produces, marshals each commit into a
// write-ahead log buffer, folds the pairs into FlatKV's account, storage and code row encodings and
// mixes every replaced and written row into an LtHash, so it differs from the disk-backed store
// only in where the bytes land. Nothing it holds survives the process; it is for benchmarks.
//
// Changesets for modules other than the EVM keep only their latest value per key.
type FlatKVMemoryStore struct {
	shards [flatKVMemoryShards]flatKVMemoryShard

	commitMu sync.Mutex
	height   atomic.Int64
	root     *lthash.LtHash
	wal      []byte

	modulesMu sync.RWMutex
	modules   map[string]map[string][]byte
}

type flatKVMemoryShard struct {
	mu       sync.RWMutex
	accounts map[common.Address]*flatKVMemoryRow
	code     map[common.Address]*flatKVMemoryRow
	storage  map[flatKVMemoryStorageKey]*flatKVMemoryRow
}

type flatKVMemoryStorageKey struct {
	address common.Address
	slot    common.Hash
}

// flatKVMemoryRow is one version of a serialized row. A nil value is a deletion. The value bytes
// are never modified once linked, so a reader may hold them after releasing the shard lock.
type flatKVMemoryRow struct {
	height   int64
	value    []byte
	previous *flatKVMemoryRow
	// truncated reports that older versions of this row were discarded.
	truncated bool
}

// NewFlatKVMemoryStore returns an empty store at height 0.
func NewFlatKVMemoryStore() *FlatKVMemoryStore {
	s := &FlatKVMemoryStore{root: lthash.New(), modules: map[string]map[string][]byte{}}
	for i := range s.shards {
		s.shards[i] = flatKVMemoryShard{
			accounts: map[common.Address]*flatKVMemoryRow{},
			code:     map[common.Address]*flatKVMemoryRow{},
			storage:  map[flatKVMemoryStorageKey]*flatKVMemoryRow{},
		}
	}
	return s
}

func flatKVMemoryShardIndex(address common.Address) int {
	return int(address[common.AddressLength-1])
}

// GetLatestVersion returns the height of the last commit.
func (s *FlatKVMemoryStore) GetLatestVersion() (int64, error) {
	return s.height.Load(), nil
}

// SetInitialVersion moves an empty store to the height preceding initialVersion, so the next
// commit is at initialVersion.
func (s *FlatKVMemoryStore) SetInitialVersion(initialVersion int64) error {
	s.commitMu.Lock()
	defer s.commitMu.Unlock()
	if current := s.height.Load(); current != 0 {
		return fmt.Errorf("flatkv memory store: cannot set initial version on a store at height %d", current)
	}
	if initialVersion <= 0 {
		return fmt.Errorf("flatkv memory store: initial version must be positive: %d", initialVersion)
	}
	s.height.Store(initialVersion - 1)
	return nil
}

// Get returns the latest value committed under key in a module other than the EVM.
func (s *FlatKVMemoryStore) Get(module string, key []byte) ([]byte, bool) {
	s.modulesMu.RLock()
	defer s.modulesMu.RUnlock()
	value, ok := s.modules[module][string(key)]
	return value, ok
}

// CommitStateChanges writes one block's changesets at blockNum, which must be the height after the
// store's current one.
func (s *FlatKVMemoryStore) CommitStateChanges(blockNum int64, changesets []*proto.NamedChangeSet) error {
	s.commitMu.Lock()
	defer s.commitMu.Unlock()
	if current := s.height.Load(); blockNum != current+1 {
		return fmt.Errorf("flatkv memory store: commit height %d must be current height %d plus one", blockNum, current)
	}
	if err := s.appendWAL(blockNum, changesets); err != nil {
		return err
	}
	writes, err := s.classifyWrites(blockNum, changesets)
	if err != nil {
		return err
	}
	if err := s.applyWrites(blockNum, writes); err != nil {
		return err
	}
	s.height.Store(blockNum)
	return nil
}

// appendWAL marshals the block's changesets into the write-ahead log buffer. The buffer holds one
// block at a time: the bytes are produced exactly as a durable log would produce them and then
// dropped.
func (s *FlatKVMemoryStore) appendWAL(blockNum int64, changesets []*proto.NamedChangeSet) error {
	s.wal = s.wal[:0]
	s.wal = binary.BigEndian.AppendUint64(s.wal, uint64(blockNum)) //nolint:gosec // heights are positive
	for _, cs := range changesets {
		size := cs.Size()
		s.wal = binary.BigEndian.AppendUint32(s.wal, uint32(size)) //nolint:gosec // changesets fit in uint32
		start := len(s.wal)
		s.wal = append(s.wal, make([]byte, size)...)
		if _, err := cs.MarshalToSizedBuffer(s.wal[start:]); err != nil {
			return fmt.Errorf("flatkv memory store: marshal changeset %q: %w", cs.Name, err)
		}
	}
	return nil
}

// flatKVMemoryShardWrites holds one shard's share of a block's writes.
type flatKVMemoryShardWrites struct {
	accounts map[common.Address]*vtype.PendingAccountWrite
	code     []flatKVMemoryAddressWrite
	storage  []flatKVMemoryStorageWrite
}

type flatKVMemoryAddressWrite struct {
	address common.Address
	value   []byte
}

type flatKVMemoryStorageWrite struct {
	key   flatKVMemoryStorageKey
	value []byte
}

// classifyWrites parses a block's EVM pairs into serialized code and storage rows and pending
// account field writes, partitioned by shard. Pairs of other modules are applied directly.
func (s *FlatKVMemoryStore) classifyWrites(blockNum int64, changesets []*proto.NamedChangeSet) (*[flatKVMemoryShards]flatKVMemoryShardWrites, error) {
	writes := new([flatKVMemoryShards]flatKVMemoryShardWrites)
	for _, cs := range changesets {
		if cs == nil {
			continue
		}
		if cs.Name != keys.EVMStoreKey {
			s.applyModuleWrites(cs)
			continue
		}
		for _, pair := range cs.Changeset.Pairs {
			if err := classifyFlatKVMemoryPair(blockNum, pair, writes); err != nil {
				return nil, err
			}
		}
	}
	return writes, nil
}

func (s *FlatKVMemoryStore) applyModuleWrites(cs *proto.NamedChangeSet) {
	s.modulesMu.Lock()
	defer s.modulesMu.Unlock()
	rows := s.modules[cs.Name]
	if rows == nil {
		rows = map[string][]byte{}
		s.modules[cs.Name] = rows
	}
	for _, pair := range cs.Changeset.Pairs {
		if pair.Delete {
			delete(rows, string(pair.Key))
			continue
		}
		rows[string(pair.Key)] = append([]byte(nil), pair.Value...)
	}
}

func classifyFlatKVMemoryPair(blockNum int64, pair *proto.KVPair, writes *[flatKVMemoryShards]flatKVMemoryShardWrites) error {
	kind, keyBytes := keys.ParseEVMKey(pair.Key)
	switch kind {
	case keys.EVMKeyNonce, keys.EVMKeyCodeHash, keys.EVMKeyBalance:
		address := common.Address(keyBytes)
		shard := &writes[flatKVMemoryShardIndex(address)]
		if shard.accounts == nil {
			shard.accounts = map[common.Address]*vtype.PendingAccountWrite{}
		}
		pending := shard.accounts[address]
		if pending == nil {
			pending = vtype.NewPendingAccountWrite()
			shard.accounts[address] = pending
		}
		return setFlatKVMemoryAccountField(pending, kind, pair)
	case keys.EVMKeyCode:
		address := common.Address(keyBytes)
		var value []byte
		if !pair.Delete && len(pair.Value) > 0 {
			value = vtype.SerializeCode(blockNum, pair.Value)
		}
		shard := &writes[flatKVMemoryShardIndex(address)]
		shard.code = append(shard.code, flatKVMemoryAddressWrite{address: address, value: value})
		return nil
	case keys.EVMKeyStorage:
		key := flatKVMemoryStorageKey{
			address: common.Address(keyBytes[:common.AddressLength]),
			slot:    common.Hash(keyBytes[common.AddressLength:]),
		}
		var value []byte
		if !pair.Delete {
			serialized, err := vtype.SerializeStorage(blockNum, pair.Value)
			if err != nil {
				return fmt.Errorf("flatkv memory store: storage %x: %w", pair.Key, err)
			}
			value = serialized
		}
		shard := &writes[flatKVMemoryShardIndex(key.address)]
		shard.storage = append(shard.storage, flatKVMemoryStorageWrite{key: key, value: value})
		return nil
	default:
		return fmt.Errorf("flatkv memory store: unsupported EVM key %x", pair.Key)
	}
}

func setFlatKVMemoryAccountField(pending *vtype.PendingAccountWrite, kind keys.EVMKeyKind, pair *proto.KVPair) error {
	switch kind {
	case keys.EVMKeyNonce:
		if pair.Delete {
			pending.SetNonce(0)
			return nil
		}
		nonce, err := vtype.ParseNonce(pair.Value)
		if err != nil {
			return err
		}
		pending.SetNonce(nonce)
	case keys.EVMKeyCodeHash:
		if pair.Delete {
			pending.SetCodeHash(&vtype.CodeHash{})
			return nil
		}
		return pending.SetCodeHashBytes(pair.Value)
	case keys.EVMKeyBalance:
		if pair.Delete {
			pending.SetBalance(&vtype.Balance{})
			return nil
		}
		balance, err := vtype.ParseBalance(pair.Value)
		if err != nil {
			return err
		}
		pending.SetBalance(balance)
	}
	return nil
}

// applyWrites folds the block's writes into the row maps, one shard per task.
func (s *FlatKVMemoryStore) applyWrites(blockNum int64, writes *[flatKVMemoryShards]flatKVMemoryShardWrites) error {
	workers := min(runtime.GOMAXPROCS(0), 8)
	var next atomic.Int64
	errs := make([]error, workers)
	deltas := make([]*lthash.LtHash, flatKVMemoryShards)
	var wg sync.WaitGroup
	wg.Add(workers)
	for worker := range workers {
		go func() {
			defer wg.Done()
			for {
				index := int(next.Add(1) - 1)
				if index >= flatKVMemoryShards {
					return
				}
				delta, err := s.shards[index].apply(blockNum, &writes[index])
				if err != nil {
					errs[worker] = err
					return
				}
				deltas[index] = delta
			}
		}()
	}
	wg.Wait()
	if err := errors.Join(errs...); err != nil {
		return err
	}
	for _, delta := range deltas {
		if delta != nil {
			s.root.MixIn(delta)
		}
	}
	return nil
}

// apply links the shard's writes as new row versions and returns the LtHash delta of the rows it
// replaced and wrote, keyed by FlatKV's physical keys.
func (sh *flatKVMemoryShard) apply(blockNum int64, writes *flatKVMemoryShardWrites) (*lthash.LtHash, error) {
	if len(writes.accounts) == 0 && len(writes.code) == 0 && len(writes.storage) == 0 {
		return nil, nil
	}
	mutations := make([]lthash.KeyMutation, 0, len(writes.accounts)+len(writes.code)+len(writes.storage))
	sh.mu.Lock()
	for address, pending := range writes.accounts {
		head := sh.accounts[address]
		previous := latestFlatKVMemoryValue(head)
		var base *vtype.AccountData
		if previous != nil {
			decoded, err := vtype.DeserializeAccountData(previous)
			if err != nil {
				sh.mu.Unlock()
				return nil, fmt.Errorf("flatkv memory store: account %s: %w", address, err)
			}
			base = decoded
		}
		merged := pending.Merge(base, blockNum)
		var value []byte
		if !merged.IsDelete() {
			value = merged.Serialize()
		}
		sh.accounts[address] = pushFlatKVMemoryRow(head, blockNum, value)
		mutations = append(mutations, flatKVMemoryMutation(keys.EVMKeyNonce, address[:], previous, value))
	}
	for _, write := range writes.code {
		head := sh.code[write.address]
		sh.code[write.address] = pushFlatKVMemoryRow(head, blockNum, write.value)
		mutations = append(mutations, flatKVMemoryMutation(keys.EVMKeyCode, write.address[:], latestFlatKVMemoryValue(head), write.value))
	}
	for _, write := range writes.storage {
		head := sh.storage[write.key]
		sh.storage[write.key] = pushFlatKVMemoryRow(head, blockNum, write.value)
		key := append(write.key.address.Bytes(), write.key.slot.Bytes()...)
		mutations = append(mutations, flatKVMemoryMutation(keys.EVMKeyStorage, key, latestFlatKVMemoryValue(head), write.value))
	}
	sh.mu.Unlock()
	return lthash.ComputeLtHash(nil, mutations), nil
}

func latestFlatKVMemoryValue(head *flatKVMemoryRow) []byte {
	if head == nil {
		return nil
	}
	return head.value
}

// flatKVMemoryMutation is the LtHash input for one row write, keyed as FlatKV keys its rows.
func flatKVMemoryMutation(kind keys.EVMKeyKind, keyBytes []byte, previous []byte, value []byte) lthash.KeyMutation {
	return lthash.KeyMutation{
		Key:       ktype.BuildPhysicalEVMKey(kind, keyBytes),
		Value:     value,
		LastValue: previous,
		Delete:    value == nil,
	}
}

// pushFlatKVMemoryRow links a new version in front of head, discarding versions beyond the
// retention.
func pushFlatKVMemoryRow(head *flatKVMemoryRow, height int64, value []byte) *flatKVMemoryRow {
	if head != nil && head.height == height {
		return &flatKVMemoryRow{height: height, value: value, previous: head.previous, truncated: head.truncated}
	}
	row := &flatKVMemoryRow{height: height, value: value, previous: head}
	kept := row
	for depth := 1; kept.previous != nil; depth++ {
		if depth == flatKVMemoryRowRetention {
			kept.previous = nil
			kept.truncated = true
			break
		}
		kept = kept.previous
	}
	return row
}

// flatKVMemoryRowAt returns the version of a row visible at height, and whether one exists.
func flatKVMemoryRowAt(row *flatKVMemoryRow, height int64) ([]byte, bool) {
	for row != nil {
		if row.height <= height {
			return row.value, row.value != nil
		}
		if row.previous == nil && row.truncated {
			panic(fmt.Sprintf("flatkv memory store: height %d is older than the retained history", height))
		}
		row = row.previous
	}
	return nil, false
}

// OpenView returns a view of the latest committed height.
func (s *FlatKVMemoryStore) OpenView() gigatypes.StateView {
	return &flatKVMemoryView{store: s, height: s.height.Load()}
}

// OpenViewAt returns a view of blockNum when it is within the retained history.
func (s *FlatKVMemoryStore) OpenViewAt(blockNum int64) (gigatypes.StateView, bool) {
	current := s.height.Load()
	if blockNum > current || blockNum <= current-flatKVMemoryRowRetention {
		return nil, false
	}
	return &flatKVMemoryView{store: s, height: blockNum}, true
}

// RegisterHashListener accepts no listeners and reports no hash: the store computes its root but
// publishes none.
func (s *FlatKVMemoryStore) RegisterHashListener(gigatypes.HashListener) (lthash.BlockHash, error) {
	return lthash.BlockHash{}, nil
}

// Close releases nothing; the rows are dropped with the store.
func (s *FlatKVMemoryStore) Close() error { return nil }

// flatKVMemoryView reads the rows visible at one height, parsing them as FlatKV's view does.
type flatKVMemoryView struct {
	store  *FlatKVMemoryStore
	height int64
}

var _ gigatypes.StateView = (*flatKVMemoryView)(nil)

func (v *flatKVMemoryView) accountRow(address common.Address) (vtype.AccountRow, bool) {
	shard := &v.store.shards[flatKVMemoryShardIndex(address)]
	shard.mu.RLock()
	raw, ok := flatKVMemoryRowAt(shard.accounts[address], v.height)
	shard.mu.RUnlock()
	if !ok {
		return vtype.AccountRow{}, false
	}
	row, err := vtype.ParseAccountRow(raw)
	if err != nil {
		panic(fmt.Sprintf("flatkv memory store: parse account %s at height %d: %v", address, v.height, err))
	}
	if row.IsDelete() {
		return vtype.AccountRow{}, false
	}
	return row, true
}

func (v *flatKVMemoryView) AccountExists(address gigatypes.Address) bool {
	_, ok := v.accountRow(address)
	return ok
}

func (v *flatKVMemoryView) GetBalance(address gigatypes.Address) gigatypes.Hash {
	row, ok := v.accountRow(address)
	if !ok {
		return gigatypes.Hash{}
	}
	return gigatypes.Hash(row.Balance())
}

func (v *flatKVMemoryView) GetNonce(address gigatypes.Address) uint64 {
	row, ok := v.accountRow(address)
	if !ok {
		return 0
	}
	return row.Nonce()
}

func (v *flatKVMemoryView) GetCodeHash(address gigatypes.Address) gigatypes.Hash {
	row, ok := v.accountRow(address)
	if !ok {
		return gigatypes.Hash{}
	}
	if codeHash := gigatypes.Hash(row.CodeHash()); codeHash != (gigatypes.Hash{}) {
		return codeHash
	}
	return gigatypes.EmptyCodeHash
}

func (v *flatKVMemoryView) ReadAccount(address gigatypes.Address) (gigatypes.Account, bool) {
	row, ok := v.accountRow(address)
	if !ok {
		return gigatypes.Account{}, false
	}
	codeHash := gigatypes.Hash(row.CodeHash())
	if codeHash == (gigatypes.Hash{}) {
		codeHash = gigatypes.EmptyCodeHash
	}
	return gigatypes.Account{Balance: gigatypes.Hash(row.Balance()), Nonce: row.Nonce(), CodeHash: codeHash}, true
}

func (v *flatKVMemoryView) GetCode(address gigatypes.Address) []byte {
	shard := &v.store.shards[flatKVMemoryShardIndex(address)]
	shard.mu.RLock()
	raw, ok := flatKVMemoryRowAt(shard.code[address], v.height)
	shard.mu.RUnlock()
	if !ok {
		return nil
	}
	code, err := vtype.DeserializeCodeData(raw)
	if err != nil {
		panic(fmt.Sprintf("flatkv memory store: parse code %s at height %d: %v", address, v.height, err))
	}
	return code.GetBytecode()
}

func (v *flatKVMemoryView) GetCodeSize(address gigatypes.Address) int {
	return len(v.GetCode(address))
}

func (v *flatKVMemoryView) GetStorage(address gigatypes.Address, slot gigatypes.Hash) gigatypes.Hash {
	shard := &v.store.shards[flatKVMemoryShardIndex(address)]
	shard.mu.RLock()
	raw, ok := flatKVMemoryRowAt(shard.storage[flatKVMemoryStorageKey{address: address, slot: slot}], v.height)
	shard.mu.RUnlock()
	if !ok {
		return gigatypes.Hash{}
	}
	row, err := vtype.ParseStorageRow(raw)
	if err != nil {
		panic(fmt.Sprintf("flatkv memory store: parse storage %s/%s at height %d: %v", address, slot, v.height, err))
	}
	return gigatypes.Hash(row.Value())
}

func (v *flatKVMemoryView) GetBlockHeight() int64 { return v.height }

// Get serves modules other than the EVM from their latest values, whatever the view's height. EVM
// rows are read through the typed getters.
func (v *flatKVMemoryView) Get(module string, key []byte) ([]byte, bool) {
	if module == keys.EVMStoreKey {
		return nil, false
	}
	return v.store.Get(module, key)
}

// GetRaw is not supported.
func (v *flatKVMemoryView) GetRaw([]byte) ([]byte, bool) {
	panic("flatkv memory store: GetRaw is not supported")
}

// BatchGetRaw is not supported.
func (v *flatKVMemoryView) BatchGetRaw([][]byte) map[string][]byte {
	panic("flatkv memory store: BatchGetRaw is not supported")
}

func (v *flatKVMemoryView) Close() {}
