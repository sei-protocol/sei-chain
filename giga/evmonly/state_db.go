package evmonly

import (
	"bytes"
	"errors"
	"math/big"
	"sort"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/stateless"
	"github.com/ethereum/go-ethereum/core/tracing"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/params"
	ethutils "github.com/ethereum/go-ethereum/trie/utils"
	"github.com/holiman/uint256"
)

var errInsufficientBalance = errors.New("insufficient balance")

type nativeStateDB struct {
	source StateReader

	accounts map[common.Address]*nativeAccount
	base     map[common.Address]*nativeAccount

	refund    uint64
	logs      []*ethtypes.Log
	preimages map[common.Hash][]byte

	accessList               accessList
	transientStates          map[common.Address]map[common.Hash]common.Hash
	finaliseAddrs            map[common.Address]struct{}
	committedStorage         map[common.Address]map[common.Hash]storageValue
	txStorageWrites          map[common.Address]map[common.Hash]struct{}
	txStorageClears          map[common.Address]struct{}
	commutativeBalanceDeltas map[common.Address]*uint256.Int
	journal                  []nativeJournalEntry
	snapshots                []nativeSnapshot
	readSet                  map[stateAccessKey]struct{}
	writeSet                 map[stateAccessKey]struct{}

	txHash      common.Hash
	txIndex     int
	txIndexUint uint
	err         error
	evm         *vm.EVM
}

type nativeAccount struct {
	Balance        *uint256.Int
	Nonce          uint64
	Code           []byte
	Storage        map[common.Hash]storageValue
	StorageCleared bool
	SelfDestructed bool
	Created        bool
}

type storageValue struct {
	value common.Hash
}

type nativeSnapshot struct {
	journalLen               int
	refund                   uint64
	logsLen                  int
	accessList               accessList
	transientStates          map[common.Address]map[common.Hash]common.Hash
	finaliseAddrs            map[common.Address]struct{}
	txStorageWrites          map[common.Address]map[common.Hash]struct{}
	txStorageClears          map[common.Address]struct{}
	commutativeBalanceDeltas map[common.Address]*uint256.Int
	preimages                map[common.Hash][]byte
	journaledAddrs           map[common.Address]struct{}
	err                      error
}

type nativeJournalKind uint8

const (
	nativeJournalAccount nativeJournalKind = iota
)

type nativeJournalEntry struct {
	kind    nativeJournalKind
	address common.Address
	account *nativeAccount
}

type accessList struct {
	addresses map[common.Address]struct{}
	slots     map[common.Address]map[common.Hash]struct{}
}

type stateAccessKind uint8

const (
	stateAccessAccount stateAccessKind = iota
	stateAccessBalance
	stateAccessNonce
	stateAccessCode
	stateAccessStorage
)

type stateAccessKey struct {
	kind    stateAccessKind
	address common.Address
	slot    common.Hash
}

func newNativeStateDB(source StateReader) *nativeStateDB {
	if source == nil {
		source = NewMemoryState()
	}
	return &nativeStateDB{
		source:                   source,
		accounts:                 map[common.Address]*nativeAccount{},
		base:                     map[common.Address]*nativeAccount{},
		preimages:                map[common.Hash][]byte{},
		accessList:               newAccessList(),
		transientStates:          map[common.Address]map[common.Hash]common.Hash{},
		finaliseAddrs:            map[common.Address]struct{}{},
		committedStorage:         map[common.Address]map[common.Hash]storageValue{},
		txStorageWrites:          map[common.Address]map[common.Hash]struct{}{},
		txStorageClears:          map[common.Address]struct{}{},
		commutativeBalanceDeltas: map[common.Address]*uint256.Int{},
	}
}

func (s *nativeStateDB) ChangeSet() StateChangeSet {
	var changes StateChangeSet
	s.ChangeSetInto(&changes)
	return changes
}

func (s *nativeStateDB) ChangeSetInto(changes *StateChangeSet) {
	changes.resetForReuse()
	addresses := make([]common.Address, 0, len(s.accounts))
	for addr := range s.accounts {
		addresses = append(addresses, addr)
	}
	sort.Slice(addresses, func(i, j int) bool {
		return bytes.Compare(addresses[i][:], addresses[j][:]) < 0
	})

	for _, addr := range addresses {
		acct := s.accounts[addr]
		base := s.baseAccount(addr)

		_, hasCommutativeDelta := s.commutativeBalanceDeltas[addr]
		if !acct.Balance.Eq(base.Balance) || hasCommutativeDelta {
			changes.Balances = append(changes.Balances, BalanceChange{
				Address: addr,
				Balance: acct.Balance.ToBig(),
			})
		}
		if acct.Nonce != base.Nonce {
			changes.Nonces = append(changes.Nonces, NonceChange{
				Address: addr,
				Nonce:   acct.Nonce,
			})
		}
		if !bytes.Equal(acct.Code, base.Code) {
			changes.Code = append(changes.Code, CodeChange{
				Address: addr,
				Code:    cloneBytes(acct.Code),
				Delete:  len(acct.Code) == 0,
			})
		}
		storageKeys := storageKeyUnion(base.Storage, acct.Storage)
		for _, key := range storageKeys {
			oldValue := storageHash(base.Storage, key)
			newValue := storageHash(acct.Storage, key)
			if acct.StorageCleared {
				oldValue = common.Hash{}
			}
			if oldValue == newValue {
				continue
			}
			changes.Storage = append(changes.Storage, StorageChange{
				Address: addr,
				Key:     key,
				Value:   newValue,
				Delete:  newValue == (common.Hash{}),
			})
		}
		if acct.StorageCleared {
			changes.StorageClears = append(changes.StorageClears, addr)
		}
	}
}

func (s *nativeStateDB) CreateAccount(addr common.Address) {
	acct := s.account(addr)
	s.recordAccount(addr)
	s.markWrite(stateAccessKey{kind: stateAccessAccount, address: addr})
	balance := acct.Balance.Clone()
	storageCleared := acct.StorageCleared
	selfDestructed := acct.SelfDestructed
	*acct = nativeAccount{
		Balance:        balance,
		Storage:        map[common.Hash]storageValue{},
		StorageCleared: storageCleared,
		SelfDestructed: selfDestructed,
		Created:        true,
	}
	s.markForFinalise(addr)
}

func (s *nativeStateDB) CreateContract(addr common.Address) {
	acct := s.account(addr)
	s.recordAccount(addr)
	s.markWrite(stateAccessKey{kind: stateAccessAccount, address: addr})
	acct.Created = true
	s.markForFinalise(addr)
}

func (s *nativeStateDB) SubBalance(addr common.Address, amount *uint256.Int, _ tracing.BalanceChangeReason) uint256.Int {
	prev := *s.GetBalance(addr)
	if amount == nil || amount.IsZero() {
		return prev
	}
	acct := s.account(addr)
	if acct.Balance.Cmp(amount) < 0 {
		s.err = errInsufficientBalance
		return prev
	}
	s.recordAccount(addr)
	s.markWrite(stateAccessKey{kind: stateAccessBalance, address: addr})
	acct.Balance.Sub(acct.Balance, amount)
	return prev
}

func (s *nativeStateDB) AddBalance(addr common.Address, amount *uint256.Int, reason tracing.BalanceChangeReason) uint256.Int {
	if reason == tracing.BalanceIncreaseRewardTransactionFee {
		return s.addCommutativeBalance(addr, amount)
	}
	prev := *s.GetBalance(addr)
	if amount == nil || amount.IsZero() {
		return prev
	}
	acct := s.account(addr)
	s.recordAccount(addr)
	s.markWrite(stateAccessKey{kind: stateAccessBalance, address: addr})
	acct.Balance.Add(acct.Balance, amount)
	return prev
}

func (s *nativeStateDB) addCommutativeBalance(addr common.Address, amount *uint256.Int) uint256.Int {
	acct := s.account(addr)
	prev := *acct.Balance.Clone()
	if amount == nil || amount.IsZero() {
		return prev
	}
	s.recordAccount(addr)
	acct.Balance.Add(acct.Balance, amount)
	if s.commutativeBalanceDeltas == nil {
		s.commutativeBalanceDeltas = map[common.Address]*uint256.Int{}
	}
	delta, ok := s.commutativeBalanceDeltas[addr]
	if !ok {
		delta = uint256.NewInt(0)
		s.commutativeBalanceDeltas[addr] = delta
	}
	delta.Add(delta, amount)
	return prev
}

func (s *nativeStateDB) commutativeBalanceDeltasBig() map[common.Address]*big.Int {
	if len(s.commutativeBalanceDeltas) == 0 {
		return nil
	}
	deltas := make(map[common.Address]*big.Int, len(s.commutativeBalanceDeltas))
	for addr, delta := range s.commutativeBalanceDeltas {
		deltas[addr] = delta.ToBig()
	}
	return deltas
}

func (s *nativeStateDB) GetBalance(addr common.Address) *uint256.Int {
	s.markRead(stateAccessKey{kind: stateAccessBalance, address: addr})
	return s.account(addr).Balance.Clone()
}

func (s *nativeStateDB) SetBalance(addr common.Address, balance *uint256.Int, _ tracing.BalanceChangeReason) {
	acct := s.account(addr)
	s.recordAccount(addr)
	s.markWrite(stateAccessKey{kind: stateAccessBalance, address: addr})
	if balance == nil {
		acct.Balance = uint256.NewInt(0)
		return
	}
	acct.Balance = balance.Clone()
}

func (s *nativeStateDB) GetNonce(addr common.Address) uint64 {
	s.markRead(stateAccessKey{kind: stateAccessNonce, address: addr})
	return s.account(addr).Nonce
}

func (s *nativeStateDB) SetNonce(addr common.Address, nonce uint64, _ tracing.NonceChangeReason) {
	acct := s.account(addr)
	s.recordAccount(addr)
	s.markWrite(stateAccessKey{kind: stateAccessNonce, address: addr})
	acct.Nonce = nonce
}

func (s *nativeStateDB) GetCodeHash(addr common.Address) common.Hash {
	s.markRead(stateAccessKey{kind: stateAccessCode, address: addr})
	acct := s.account(addr)
	if len(acct.Code) > 0 {
		return crypto.Keccak256Hash(acct.Code)
	}
	if acct.Nonce == 0 && acct.Balance.IsZero() {
		return common.Hash{}
	}
	return ethtypes.EmptyCodeHash
}

func (s *nativeStateDB) GetCode(addr common.Address) []byte {
	s.markRead(stateAccessKey{kind: stateAccessCode, address: addr})
	return cloneBytes(s.account(addr).Code)
}

func (s *nativeStateDB) SetCode(addr common.Address, code []byte) []byte {
	acct := s.account(addr)
	prev := cloneBytes(acct.Code)
	s.recordAccount(addr)
	s.markWrite(stateAccessKey{kind: stateAccessCode, address: addr})
	acct.Code = cloneBytes(code)
	return prev
}

func (s *nativeStateDB) GetCodeSize(addr common.Address) int {
	s.markRead(stateAccessKey{kind: stateAccessCode, address: addr})
	return len(s.account(addr).Code)
}

func (s *nativeStateDB) AddRefund(gas uint64) {
	s.refund += gas
}

func (s *nativeStateDB) SubRefund(gas uint64) {
	if gas > s.refund {
		panic("refund counter underflow")
	}
	s.refund -= gas
}

func (s *nativeStateDB) GetRefund() uint64 {
	return s.refund
}

func (s *nativeStateDB) GetCommittedState(addr common.Address, key common.Hash) common.Hash {
	s.markRead(stateAccessKey{kind: stateAccessStorage, address: addr, slot: key})
	return s.committedState(addr, key)
}

func (s *nativeStateDB) GetState(addr common.Address, key common.Hash) common.Hash {
	s.markRead(stateAccessKey{kind: stateAccessStorage, address: addr, slot: key})
	s.ensureStorage(addr, key)
	return storageHash(s.account(addr).Storage, key)
}

func (s *nativeStateDB) SetState(addr common.Address, key common.Hash, value common.Hash) common.Hash {
	s.ensureStorage(addr, key)
	acct := s.account(addr)
	prev := storageHash(acct.Storage, key)
	s.recordAccount(addr)
	s.markWrite(stateAccessKey{kind: stateAccessStorage, address: addr, slot: key})
	acct.Storage[key] = storageValue{value: value}
	s.markTxStorageWrite(addr, key)
	return prev
}

func (s *nativeStateDB) SetStorage(addr common.Address, states map[common.Hash]common.Hash) {
	acct := s.account(addr)
	s.recordAccount(addr)
	s.markWrite(stateAccessKey{kind: stateAccessAccount, address: addr})
	s.markTxStorageClear(addr)
	acct.Storage = map[common.Hash]storageValue{}
	acct.StorageCleared = true
	for key, value := range states {
		acct.Storage[key] = storageValue{value: value}
		s.markTxStorageWrite(addr, key)
	}
}

func (s *nativeStateDB) GetStorageRoot(common.Address) common.Hash {
	return common.Hash{}
}

func (s *nativeStateDB) GetTransientState(addr common.Address, key common.Hash) common.Hash {
	if states, ok := s.transientStates[addr]; ok {
		return states[key]
	}
	return common.Hash{}
}

func (s *nativeStateDB) SetTransientState(addr common.Address, key, value common.Hash) {
	states, ok := s.transientStates[addr]
	if !ok {
		states = map[common.Hash]common.Hash{}
		s.transientStates[addr] = states
	}
	if value == (common.Hash{}) {
		delete(states, key)
		return
	}
	states[key] = value
}

func (s *nativeStateDB) SelfDestruct(addr common.Address) uint256.Int {
	acct := s.account(addr)
	prev := *acct.Balance.Clone()
	s.recordAccount(addr)
	s.markWrite(stateAccessKey{kind: stateAccessAccount, address: addr})
	acct.Balance.Clear()
	acct.SelfDestructed = true
	s.markForFinalise(addr)
	return prev
}

func (s *nativeStateDB) SelfDestruct6780(addr common.Address) (uint256.Int, bool) {
	acct := s.account(addr)
	if !acct.Created {
		return *acct.Balance.Clone(), false
	}
	return s.SelfDestruct(addr), true
}

func (s *nativeStateDB) HasSelfDestructed(addr common.Address) bool {
	return s.account(addr).SelfDestructed
}

func (s *nativeStateDB) Exist(addr common.Address) bool {
	s.markRead(stateAccessKey{kind: stateAccessAccount, address: addr})
	acct := s.account(addr)
	return acct.SelfDestructed || acct.Nonce != 0 || !acct.Balance.IsZero() || len(acct.Code) != 0
}

func (s *nativeStateDB) Empty(addr common.Address) bool {
	s.markRead(stateAccessKey{kind: stateAccessAccount, address: addr})
	acct := s.account(addr)
	return acct.Nonce == 0 && acct.Balance.IsZero() && len(acct.Code) == 0
}

func (s *nativeStateDB) AddressInAccessList(addr common.Address) bool {
	_, ok := s.accessList.addresses[addr]
	return ok
}

func (s *nativeStateDB) SlotInAccessList(addr common.Address, slot common.Hash) (bool, bool) {
	_, addressOk := s.accessList.addresses[addr]
	if !addressOk {
		return false, false
	}
	slots, ok := s.accessList.slots[addr]
	if !ok {
		return true, false
	}
	_, slotOk := slots[slot]
	return true, slotOk
}

func (s *nativeStateDB) AddAddressToAccessList(addr common.Address) {
	s.accessList.addresses[addr] = struct{}{}
}

func (s *nativeStateDB) AddSlotToAccessList(addr common.Address, slot common.Hash) {
	s.AddAddressToAccessList(addr)
	slots, ok := s.accessList.slots[addr]
	if !ok {
		slots = map[common.Hash]struct{}{}
		s.accessList.slots[addr] = slots
	}
	slots[slot] = struct{}{}
}

func (s *nativeStateDB) Prepare(_ params.Rules, sender, coinbase common.Address, dest *common.Address, precompiles []common.Address, txAccesses ethtypes.AccessList) {
	s.accessList.reset()
	clearNestedHashMaps(s.transientStates)
	s.AddAddressToAccessList(sender)
	s.AddAddressToAccessList(coinbase)
	if dest != nil {
		s.AddAddressToAccessList(*dest)
	}
	for _, addr := range precompiles {
		s.AddAddressToAccessList(addr)
	}
	for _, tuple := range txAccesses {
		s.AddAddressToAccessList(tuple.Address)
		for _, key := range tuple.StorageKeys {
			s.AddSlotToAccessList(tuple.Address, key)
		}
	}
}

func (s *nativeStateDB) PointCache() *ethutils.PointCache {
	return nil
}

func (s *nativeStateDB) Snapshot() int {
	id := len(s.snapshots)
	s.snapshots = append(s.snapshots, nativeSnapshot{
		journalLen:               len(s.journal),
		refund:                   s.refund,
		logsLen:                  len(s.logs),
		accessList:               cloneAccessList(s.accessList),
		transientStates:          cloneTransientStates(s.transientStates),
		finaliseAddrs:            cloneAddressSet(s.finaliseAddrs),
		txStorageWrites:          cloneStorageWriteSet(s.txStorageWrites),
		txStorageClears:          cloneAddressSet(s.txStorageClears),
		commutativeBalanceDeltas: cloneUint256Map(s.commutativeBalanceDeltas),
		preimages:                clonePreimages(s.preimages),
		journaledAddrs:           map[common.Address]struct{}{},
		err:                      s.err,
	})
	return id
}

func (s *nativeStateDB) RevertToSnapshot(id int) {
	if id < 0 || id >= len(s.snapshots) {
		panic("invalid state snapshot")
	}
	snapshot := s.snapshots[id]
	for i := len(s.journal) - 1; i >= snapshot.journalLen; i-- {
		s.journal[i].revert(s)
	}
	s.journal = s.journal[:snapshot.journalLen]
	s.refund = snapshot.refund
	s.logs = s.logs[:snapshot.logsLen]
	s.accessList = cloneAccessList(snapshot.accessList)
	s.transientStates = cloneTransientStates(snapshot.transientStates)
	s.finaliseAddrs = cloneAddressSet(snapshot.finaliseAddrs)
	s.txStorageWrites = cloneStorageWriteSet(snapshot.txStorageWrites)
	s.txStorageClears = cloneAddressSet(snapshot.txStorageClears)
	s.commutativeBalanceDeltas = cloneUint256Map(snapshot.commutativeBalanceDeltas)
	s.preimages = clonePreimages(snapshot.preimages)
	s.err = snapshot.err
	s.snapshots = s.snapshots[:id]
}

func (s *nativeStateDB) AddLog(log *ethtypes.Log) {
	log.TxHash = s.txHash
	log.TxIndex = s.txIndexUint
	log.Index = uint(len(s.logs))
	s.logs = append(s.logs, log)
}

func (s *nativeStateDB) AddPreimage(hash common.Hash, preimage []byte) {
	s.preimages[hash] = cloneBytes(preimage)
}

func (s *nativeStateDB) Witness() *stateless.Witness {
	return nil
}

func (s *nativeStateDB) AccessEvents() *vm.AccessEvents {
	return nil
}

func (s *nativeStateDB) Finalise(bool) {
	for addr := range s.finaliseAddrs {
		acct := s.account(addr)
		if acct.SelfDestructed {
			s.recordAccount(addr)
			acct.Code = nil
			acct.Storage = map[common.Hash]storageValue{}
			acct.StorageCleared = true
			acct.Nonce = 0
			acct.SelfDestructed = false
			s.markTxStorageClear(addr)
		}
		if acct.Created {
			s.recordAccount(addr)
			acct.Created = false
		}
	}
	s.finaliseTxStorage()
	clear(s.finaliseAddrs)
	s.refund = 0
}

func (s *nativeStateDB) Error() error {
	return s.err
}

func (s *nativeStateDB) Commit(uint64, bool, bool) (common.Hash, error) {
	return common.Hash{}, s.err
}

func (s *nativeStateDB) SetTxContext(hash common.Hash, index int) {
	s.txHash = hash
	s.txIndex = index
}

func (s *nativeStateDB) setTxContext(hash common.Hash, index int, indexUint uint) {
	s.txHash = hash
	s.txIndex = index
	s.txIndexUint = indexUint
}

func (s *nativeStateDB) Copy() vm.StateDB {
	cp := &nativeStateDB{
		source:                   s.source,
		accounts:                 cloneAccounts(s.accounts),
		base:                     cloneAccounts(s.base),
		refund:                   s.refund,
		logs:                     append([]*ethtypes.Log(nil), s.logs...),
		preimages:                clonePreimages(s.preimages),
		accessList:               cloneAccessList(s.accessList),
		transientStates:          cloneTransientStates(s.transientStates),
		finaliseAddrs:            cloneAddressSet(s.finaliseAddrs),
		committedStorage:         cloneStorageValueMaps(s.committedStorage),
		txStorageWrites:          cloneStorageWriteSet(s.txStorageWrites),
		txStorageClears:          cloneAddressSet(s.txStorageClears),
		commutativeBalanceDeltas: cloneUint256Map(s.commutativeBalanceDeltas),
		journal:                  cloneJournal(s.journal),
		snapshots:                cloneSnapshots(s.snapshots),
		readSet:                  cloneAccessSet(s.readSet),
		writeSet:                 cloneAccessSet(s.writeSet),
		txHash:                   s.txHash,
		txIndex:                  s.txIndex,
		txIndexUint:              s.txIndexUint,
		err:                      s.err,
		evm:                      s.evm,
	}
	return cp
}

func (s *nativeStateDB) IntermediateRoot(bool) common.Hash {
	return common.Hash{}
}

func (s *nativeStateDB) GetLogs(common.Hash, uint64, common.Hash) []*ethtypes.Log {
	return s.Logs()
}

func (s *nativeStateDB) TxIndex() int {
	return s.txIndex
}

func (s *nativeStateDB) Preimages() map[common.Hash][]byte {
	return clonePreimages(s.preimages)
}

func (s *nativeStateDB) Logs() []*ethtypes.Log {
	return append([]*ethtypes.Log(nil), s.logs...)
}

func (s *nativeStateDB) SetEVM(evm *vm.EVM) {
	s.evm = evm
}

func (s *nativeStateDB) enableAccessTracking() {
	if s.readSet == nil {
		s.readSet = map[stateAccessKey]struct{}{}
	} else {
		clear(s.readSet)
	}
	if s.writeSet == nil {
		s.writeSet = map[stateAccessKey]struct{}{}
	} else {
		clear(s.writeSet)
	}
}

func (s *nativeStateDB) accessSets() (map[stateAccessKey]struct{}, map[stateAccessKey]struct{}) {
	return cloneAccessSet(s.readSet), cloneAccessSet(s.writeSet)
}

func (s *nativeStateDB) markRead(key stateAccessKey) {
	if s.readSet != nil {
		s.readSet[key] = struct{}{}
	}
}

func (s *nativeStateDB) markWrite(key stateAccessKey) {
	if s.writeSet != nil {
		s.writeSet[key] = struct{}{}
	}
}

func (s *nativeStateDB) recordAccount(addr common.Address) {
	if len(s.snapshots) == 0 {
		return
	}
	snapshot := &s.snapshots[len(s.snapshots)-1]
	if _, ok := snapshot.journaledAddrs[addr]; ok {
		return
	}
	snapshot.journaledAddrs[addr] = struct{}{}
	s.journal = append(s.journal, nativeJournalEntry{
		kind:    nativeJournalAccount,
		address: addr,
		account: s.account(addr).clone(),
	})
}

func (e nativeJournalEntry) revert(s *nativeStateDB) {
	switch e.kind {
	case nativeJournalAccount:
		s.accounts[e.address] = e.account.clone()
	default:
		panic("unknown native state journal entry")
	}
}

func (s *nativeStateDB) markForFinalise(addr common.Address) {
	s.finaliseAddrs[addr] = struct{}{}
}

func (s *nativeStateDB) clearSnapshots() {
	clear(s.journal)
	s.journal = s.journal[:0]
	clear(s.snapshots)
	s.snapshots = s.snapshots[:0]
}

func (s *nativeStateDB) reset(source StateReader) {
	if source == nil {
		source = NewMemoryState()
	}
	s.source = source
	clear(s.accounts)
	clear(s.base)
	s.refund = 0
	clear(s.logs)
	s.logs = s.logs[:0]
	clearBytesMap(s.preimages)
	s.accessList.reset()
	clearNestedHashMaps(s.transientStates)
	clear(s.finaliseAddrs)
	clearNestedStorageValueMaps(s.committedStorage)
	clearNestedStorageWriteSets(s.txStorageWrites)
	clear(s.txStorageClears)
	clear(s.commutativeBalanceDeltas)
	clear(s.journal)
	s.journal = s.journal[:0]
	clear(s.snapshots)
	s.snapshots = s.snapshots[:0]
	if s.readSet != nil {
		clear(s.readSet)
	}
	if s.writeSet != nil {
		clear(s.writeSet)
	}
	s.txHash = common.Hash{}
	s.txIndex = 0
	s.txIndexUint = 0
	s.err = nil
	s.evm = nil
}

func (s *nativeStateDB) account(addr common.Address) *nativeAccount {
	if acct, ok := s.accounts[addr]; ok {
		return acct
	}
	base, ok := s.base[addr]
	if !ok {
		base = s.loadAccount(addr)
		s.base[addr] = base.clone()
	}
	s.accounts[addr] = base.clone()
	return s.accounts[addr]
}

func (s *nativeStateDB) baseAccount(addr common.Address) *nativeAccount {
	if acct, ok := s.base[addr]; ok {
		return acct
	}
	acct := s.loadAccount(addr)
	s.base[addr] = acct.clone()
	return s.base[addr]
}

func (s *nativeStateDB) ensureStorage(addr common.Address, key common.Hash) {
	base := s.baseAccount(addr)
	acct := s.account(addr)
	if _, ok := acct.Storage[key]; ok {
		return
	}
	if slots, ok := s.committedStorage[addr]; ok {
		if value, committed := slots[key]; committed {
			acct.Storage[key] = value
			return
		}
	}
	if acct.StorageCleared {
		return
	}
	if _, ok := base.Storage[key]; !ok {
		if value := s.source.GetState(addr, key); value != (common.Hash{}) {
			base.Storage[key] = storageValue{value: value}
		}
	}
	if value := storageHash(base.Storage, key); value != (common.Hash{}) {
		acct.Storage[key] = storageValue{value: value}
	}
}

func (s *nativeStateDB) committedState(addr common.Address, key common.Hash) common.Hash {
	if slots, ok := s.committedStorage[addr]; ok {
		if value, committed := slots[key]; committed {
			return value.value
		}
	}
	if acct, ok := s.accounts[addr]; ok && acct.StorageCleared {
		return common.Hash{}
	}
	base := s.baseAccount(addr)
	if _, ok := base.Storage[key]; !ok {
		if value := s.source.GetState(addr, key); value != (common.Hash{}) {
			base.Storage[key] = storageValue{value: value}
		}
	}
	return storageHash(base.Storage, key)
}

func (s *nativeStateDB) markTxStorageWrite(addr common.Address, key common.Hash) {
	if s.txStorageWrites == nil {
		s.txStorageWrites = map[common.Address]map[common.Hash]struct{}{}
	}
	slots, ok := s.txStorageWrites[addr]
	if !ok {
		slots = map[common.Hash]struct{}{}
		s.txStorageWrites[addr] = slots
	}
	slots[key] = struct{}{}
}

func (s *nativeStateDB) markTxStorageClear(addr common.Address) {
	if s.txStorageClears == nil {
		s.txStorageClears = map[common.Address]struct{}{}
	}
	s.txStorageClears[addr] = struct{}{}
}

func (s *nativeStateDB) finaliseTxStorage() {
	if s.committedStorage == nil {
		s.committedStorage = map[common.Address]map[common.Hash]storageValue{}
	}
	for addr := range s.txStorageClears {
		s.committedStorage[addr] = map[common.Hash]storageValue{}
	}
	for addr, keys := range s.txStorageWrites {
		if len(keys) == 0 {
			continue
		}
		slots, ok := s.committedStorage[addr]
		if !ok {
			slots = map[common.Hash]storageValue{}
			s.committedStorage[addr] = slots
		}
		acct := s.account(addr)
		for key := range keys {
			value, ok := acct.Storage[key]
			if !ok {
				delete(slots, key)
				continue
			}
			slots[key] = value
		}
	}
	clearNestedStorageWriteSets(s.txStorageWrites)
	clear(s.txStorageClears)
}

func (s *nativeStateDB) loadAccount(addr common.Address) *nativeAccount {
	acct := &nativeAccount{
		Balance: uint256FromBig(s.source.GetBalance(addr)),
		Nonce:   s.source.GetNonce(addr),
		Code:    cloneBytes(s.source.GetCode(addr)),
		Storage: map[common.Hash]storageValue{},
	}
	return acct
}

func (a *nativeAccount) clone() *nativeAccount {
	if a == nil {
		return &nativeAccount{Balance: uint256.NewInt(0), Storage: map[common.Hash]storageValue{}}
	}
	cp := &nativeAccount{
		Balance:        uint256.NewInt(0),
		Nonce:          a.Nonce,
		Code:           cloneBytes(a.Code),
		Storage:        map[common.Hash]storageValue{},
		StorageCleared: a.StorageCleared,
		SelfDestructed: a.SelfDestructed,
		Created:        a.Created,
	}
	if a.Balance != nil {
		cp.Balance = a.Balance.Clone()
	}
	for key, value := range a.Storage {
		cp.Storage[key] = value
	}
	return cp
}

func newAccessList() accessList {
	return accessList{
		addresses: map[common.Address]struct{}{},
		slots:     map[common.Address]map[common.Hash]struct{}{},
	}
}

func (al *accessList) reset() {
	if al.addresses == nil {
		al.addresses = map[common.Address]struct{}{}
	} else {
		clear(al.addresses)
	}
	if al.slots == nil {
		al.slots = map[common.Address]map[common.Hash]struct{}{}
		return
	}
	for _, slots := range al.slots {
		clear(slots)
	}
	clear(al.slots)
}

func cloneAccessList(al accessList) accessList {
	cp := newAccessList()
	for addr := range al.addresses {
		cp.addresses[addr] = struct{}{}
	}
	for addr, slots := range al.slots {
		cp.slots[addr] = map[common.Hash]struct{}{}
		for slot := range slots {
			cp.slots[addr][slot] = struct{}{}
		}
	}
	return cp
}

func cloneAccounts(accounts map[common.Address]*nativeAccount) map[common.Address]*nativeAccount {
	cp := make(map[common.Address]*nativeAccount, len(accounts))
	for addr, acct := range accounts {
		cp[addr] = acct.clone()
	}
	return cp
}

func cloneTransientStates(states map[common.Address]map[common.Hash]common.Hash) map[common.Address]map[common.Hash]common.Hash {
	cp := make(map[common.Address]map[common.Hash]common.Hash, len(states))
	for addr, slots := range states {
		cp[addr] = map[common.Hash]common.Hash{}
		for key, value := range slots {
			cp[addr][key] = value
		}
	}
	return cp
}

func cloneStorageValueMaps(states map[common.Address]map[common.Hash]storageValue) map[common.Address]map[common.Hash]storageValue {
	cp := make(map[common.Address]map[common.Hash]storageValue, len(states))
	for addr, slots := range states {
		cp[addr] = map[common.Hash]storageValue{}
		for key, value := range slots {
			cp[addr][key] = value
		}
	}
	return cp
}

func cloneStorageWriteSet(states map[common.Address]map[common.Hash]struct{}) map[common.Address]map[common.Hash]struct{} {
	cp := make(map[common.Address]map[common.Hash]struct{}, len(states))
	for addr, slots := range states {
		cp[addr] = map[common.Hash]struct{}{}
		for key := range slots {
			cp[addr][key] = struct{}{}
		}
	}
	return cp
}

func cloneAddressSet(addrs map[common.Address]struct{}) map[common.Address]struct{} {
	cp := make(map[common.Address]struct{}, len(addrs))
	for addr := range addrs {
		cp[addr] = struct{}{}
	}
	return cp
}

func cloneUint256Map(values map[common.Address]*uint256.Int) map[common.Address]*uint256.Int {
	if values == nil {
		return nil
	}
	cp := make(map[common.Address]*uint256.Int, len(values))
	for addr, value := range values {
		if value == nil {
			cp[addr] = uint256.NewInt(0)
		} else {
			cp[addr] = value.Clone()
		}
	}
	return cp
}

func cloneAccessSet(set map[stateAccessKey]struct{}) map[stateAccessKey]struct{} {
	if set == nil {
		return nil
	}
	cp := make(map[stateAccessKey]struct{}, len(set))
	for key := range set {
		cp[key] = struct{}{}
	}
	return cp
}

func clonePreimages(preimages map[common.Hash][]byte) map[common.Hash][]byte {
	cp := make(map[common.Hash][]byte, len(preimages))
	for hash, preimage := range preimages {
		cp[hash] = cloneBytes(preimage)
	}
	return cp
}

func clearBytesMap(values map[common.Hash][]byte) {
	for key := range values {
		delete(values, key)
	}
}

func clearNestedHashMaps(values map[common.Address]map[common.Hash]common.Hash) {
	for _, slots := range values {
		clear(slots)
	}
	clear(values)
}

func clearNestedStorageValueMaps(values map[common.Address]map[common.Hash]storageValue) {
	for _, slots := range values {
		clear(slots)
	}
	clear(values)
}

func clearNestedStorageWriteSets(values map[common.Address]map[common.Hash]struct{}) {
	for _, slots := range values {
		clear(slots)
	}
	clear(values)
}

func cloneJournal(journal []nativeJournalEntry) []nativeJournalEntry {
	cp := make([]nativeJournalEntry, len(journal))
	for i, entry := range journal {
		cp[i] = nativeJournalEntry{
			kind:    entry.kind,
			address: entry.address,
			account: entry.account.clone(),
		}
	}
	return cp
}

func cloneSnapshots(snapshots []nativeSnapshot) []nativeSnapshot {
	cp := make([]nativeSnapshot, len(snapshots))
	for i, snapshot := range snapshots {
		cp[i] = nativeSnapshot{
			journalLen:               snapshot.journalLen,
			refund:                   snapshot.refund,
			logsLen:                  snapshot.logsLen,
			accessList:               cloneAccessList(snapshot.accessList),
			transientStates:          cloneTransientStates(snapshot.transientStates),
			finaliseAddrs:            cloneAddressSet(snapshot.finaliseAddrs),
			txStorageWrites:          cloneStorageWriteSet(snapshot.txStorageWrites),
			txStorageClears:          cloneAddressSet(snapshot.txStorageClears),
			commutativeBalanceDeltas: cloneUint256Map(snapshot.commutativeBalanceDeltas),
			preimages:                clonePreimages(snapshot.preimages),
			journaledAddrs:           cloneAddressSet(snapshot.journaledAddrs),
			err:                      snapshot.err,
		}
	}
	return cp
}

func storageHash(values map[common.Hash]storageValue, key common.Hash) common.Hash {
	return values[key].value
}

func storageKeyUnion(a, b map[common.Hash]storageValue) []common.Hash {
	seen := map[common.Hash]struct{}{}
	for key := range a {
		seen[key] = struct{}{}
	}
	for key := range b {
		seen[key] = struct{}{}
	}
	keys := make([]common.Hash, 0, len(seen))
	for key := range seen {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		return bytes.Compare(keys[i][:], keys[j][:]) < 0
	})
	return keys
}

func uint256FromBig(v *big.Int) *uint256.Int {
	if v == nil {
		return uint256.NewInt(0)
	}
	u, overflow := uint256.FromBig(v)
	if overflow {
		panic("state balance exceeds uint256")
	}
	if u == nil {
		return uint256.NewInt(0)
	}
	return u
}
