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

	accessList      accessList
	transientStates map[common.Address]map[common.Hash]common.Hash
	snapshots       []nativeSnapshot

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
	Storage        map[common.Hash]common.Hash
	SelfDestructed bool
	Created        bool
}

type nativeSnapshot struct {
	accounts        map[common.Address]*nativeAccount
	base            map[common.Address]*nativeAccount
	refund          uint64
	logs            []*ethtypes.Log
	accessList      accessList
	transientStates map[common.Address]map[common.Hash]common.Hash
	preimages       map[common.Hash][]byte
	err             error
}

type accessList struct {
	addresses map[common.Address]struct{}
	slots     map[common.Address]map[common.Hash]struct{}
}

func newNativeStateDB(source StateReader) *nativeStateDB {
	if source == nil {
		source = NewMemoryState()
	}
	return &nativeStateDB{
		source:          source,
		accounts:        map[common.Address]*nativeAccount{},
		base:            map[common.Address]*nativeAccount{},
		preimages:       map[common.Hash][]byte{},
		accessList:      newAccessList(),
		transientStates: map[common.Address]map[common.Hash]common.Hash{},
	}
}

func (s *nativeStateDB) ChangeSet() StateChangeSet {
	addresses := make([]common.Address, 0, len(s.accounts))
	for addr := range s.accounts {
		addresses = append(addresses, addr)
	}
	sort.Slice(addresses, func(i, j int) bool {
		return bytes.Compare(addresses[i][:], addresses[j][:]) < 0
	})

	var changes StateChangeSet
	for _, addr := range addresses {
		acct := s.accounts[addr]
		base := s.baseAccount(addr)

		if !acct.Balance.Eq(base.Balance) {
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
			oldValue := base.Storage[key]
			newValue := acct.Storage[key]
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
	}
	return changes
}

func (s *nativeStateDB) CreateAccount(addr common.Address) {
	acct := s.account(addr)
	balance := acct.Balance.Clone()
	*acct = nativeAccount{
		Balance: balance,
		Storage: map[common.Hash]common.Hash{},
		Created: true,
	}
}

func (s *nativeStateDB) CreateContract(addr common.Address) {
	s.account(addr).Created = true
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
	acct.Balance.Sub(acct.Balance, amount)
	return prev
}

func (s *nativeStateDB) AddBalance(addr common.Address, amount *uint256.Int, _ tracing.BalanceChangeReason) uint256.Int {
	prev := *s.GetBalance(addr)
	if amount == nil || amount.IsZero() {
		return prev
	}
	acct := s.account(addr)
	acct.Balance.Add(acct.Balance, amount)
	return prev
}

func (s *nativeStateDB) GetBalance(addr common.Address) *uint256.Int {
	return s.account(addr).Balance.Clone()
}

func (s *nativeStateDB) SetBalance(addr common.Address, balance *uint256.Int, _ tracing.BalanceChangeReason) {
	acct := s.account(addr)
	if balance == nil {
		acct.Balance = uint256.NewInt(0)
		return
	}
	acct.Balance = balance.Clone()
}

func (s *nativeStateDB) GetNonce(addr common.Address) uint64 {
	return s.account(addr).Nonce
}

func (s *nativeStateDB) SetNonce(addr common.Address, nonce uint64, _ tracing.NonceChangeReason) {
	s.account(addr).Nonce = nonce
}

func (s *nativeStateDB) GetCodeHash(addr common.Address) common.Hash {
	code := s.GetCode(addr)
	if len(code) == 0 {
		return common.Hash{}
	}
	return crypto.Keccak256Hash(code)
}

func (s *nativeStateDB) GetCode(addr common.Address) []byte {
	return cloneBytes(s.account(addr).Code)
}

func (s *nativeStateDB) SetCode(addr common.Address, code []byte) []byte {
	acct := s.account(addr)
	prev := cloneBytes(acct.Code)
	acct.Code = cloneBytes(code)
	return prev
}

func (s *nativeStateDB) GetCodeSize(addr common.Address) int {
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
	s.ensureStorage(addr, key)
	return s.baseAccount(addr).Storage[key]
}

func (s *nativeStateDB) GetState(addr common.Address, key common.Hash) common.Hash {
	s.ensureStorage(addr, key)
	return s.account(addr).Storage[key]
}

func (s *nativeStateDB) SetState(addr common.Address, key common.Hash, value common.Hash) common.Hash {
	s.ensureStorage(addr, key)
	acct := s.account(addr)
	prev := acct.Storage[key]
	if value == (common.Hash{}) {
		delete(acct.Storage, key)
	} else {
		acct.Storage[key] = value
	}
	return prev
}

func (s *nativeStateDB) SetStorage(addr common.Address, states map[common.Hash]common.Hash) {
	acct := s.account(addr)
	acct.Storage = map[common.Hash]common.Hash{}
	for key, value := range states {
		if value != (common.Hash{}) {
			acct.Storage[key] = value
		}
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
	acct.Balance.Clear()
	acct.SelfDestructed = true
	return prev
}

func (s *nativeStateDB) SelfDestruct6780(addr common.Address) (uint256.Int, bool) {
	if !s.account(addr).Created {
		return *uint256.NewInt(0), false
	}
	return s.SelfDestruct(addr), true
}

func (s *nativeStateDB) HasSelfDestructed(addr common.Address) bool {
	return s.account(addr).SelfDestructed
}

func (s *nativeStateDB) Exist(addr common.Address) bool {
	acct := s.account(addr)
	return acct.SelfDestructed || acct.Nonce != 0 || !acct.Balance.IsZero() || len(acct.Code) != 0
}

func (s *nativeStateDB) Empty(addr common.Address) bool {
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
	s.accessList = newAccessList()
	s.transientStates = map[common.Address]map[common.Hash]common.Hash{}
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
		accounts:        cloneAccounts(s.accounts),
		base:            cloneAccounts(s.base),
		refund:          s.refund,
		logs:            append([]*ethtypes.Log(nil), s.logs...),
		accessList:      cloneAccessList(s.accessList),
		transientStates: cloneTransientStates(s.transientStates),
		preimages:       clonePreimages(s.preimages),
		err:             s.err,
	})
	return id
}

func (s *nativeStateDB) RevertToSnapshot(id int) {
	if id < 0 || id >= len(s.snapshots) {
		panic("invalid state snapshot")
	}
	snapshot := s.snapshots[id]
	s.accounts = cloneAccounts(snapshot.accounts)
	s.base = cloneAccounts(snapshot.base)
	s.refund = snapshot.refund
	s.logs = append([]*ethtypes.Log(nil), snapshot.logs...)
	s.accessList = cloneAccessList(snapshot.accessList)
	s.transientStates = cloneTransientStates(snapshot.transientStates)
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
	for _, acct := range s.accounts {
		if acct.SelfDestructed {
			acct.Code = nil
			acct.Storage = map[common.Hash]common.Hash{}
		}
	}
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
		source:          s.source,
		accounts:        cloneAccounts(s.accounts),
		base:            cloneAccounts(s.base),
		refund:          s.refund,
		logs:            append([]*ethtypes.Log(nil), s.logs...),
		preimages:       clonePreimages(s.preimages),
		accessList:      cloneAccessList(s.accessList),
		transientStates: cloneTransientStates(s.transientStates),
		snapshots:       cloneSnapshots(s.snapshots),
		txHash:          s.txHash,
		txIndex:         s.txIndex,
		txIndexUint:     s.txIndexUint,
		err:             s.err,
		evm:             s.evm,
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

func (s *nativeStateDB) account(addr common.Address) *nativeAccount {
	if acct, ok := s.accounts[addr]; ok {
		return acct
	}
	acct := s.loadAccount(addr)
	s.accounts[addr] = acct.clone()
	s.base[addr] = acct.clone()
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
	if _, ok := base.Storage[key]; !ok {
		if value := s.source.GetState(addr, key); value != (common.Hash{}) {
			base.Storage[key] = value
		}
	}
	acct := s.account(addr)
	if _, ok := acct.Storage[key]; !ok {
		if value := base.Storage[key]; value != (common.Hash{}) {
			acct.Storage[key] = value
		}
	}
}

func (s *nativeStateDB) loadAccount(addr common.Address) *nativeAccount {
	acct := &nativeAccount{
		Balance: uint256FromBig(s.source.GetBalance(addr)),
		Nonce:   s.source.GetNonce(addr),
		Code:    cloneBytes(s.source.GetCode(addr)),
		Storage: map[common.Hash]common.Hash{},
	}
	return acct
}

func (a *nativeAccount) clone() *nativeAccount {
	if a == nil {
		return &nativeAccount{Balance: uint256.NewInt(0), Storage: map[common.Hash]common.Hash{}}
	}
	cp := &nativeAccount{
		Balance:        uint256.NewInt(0),
		Nonce:          a.Nonce,
		Code:           cloneBytes(a.Code),
		Storage:        map[common.Hash]common.Hash{},
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

func clonePreimages(preimages map[common.Hash][]byte) map[common.Hash][]byte {
	cp := make(map[common.Hash][]byte, len(preimages))
	for hash, preimage := range preimages {
		cp[hash] = cloneBytes(preimage)
	}
	return cp
}

func cloneSnapshots(snapshots []nativeSnapshot) []nativeSnapshot {
	cp := make([]nativeSnapshot, len(snapshots))
	for i, snapshot := range snapshots {
		cp[i] = nativeSnapshot{
			accounts:        cloneAccounts(snapshot.accounts),
			base:            cloneAccounts(snapshot.base),
			refund:          snapshot.refund,
			logs:            append([]*ethtypes.Log(nil), snapshot.logs...),
			accessList:      cloneAccessList(snapshot.accessList),
			transientStates: cloneTransientStates(snapshot.transientStates),
			preimages:       clonePreimages(snapshot.preimages),
			err:             snapshot.err,
		}
	}
	return cp
}

func storageKeyUnion(a, b map[common.Hash]common.Hash) []common.Hash {
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
