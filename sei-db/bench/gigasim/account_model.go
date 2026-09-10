package gigasim

import (
	"encoding/binary"
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-db/common/keys"
	crand "github.com/sei-protocol/sei-chain/sei-db/common/rand"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/vtype"
)

// Address type markers, so that an account, a contract and a storage slot sharing an identifier still
// get distinct keys.
const (
	accountPrefix    = 'a'
	contractPrefix   = 'c'
	ethStoragePrefix = 's'
)

// EVM key sizes, matching sei-db/common/keys.
const (
	slotLen       = 32
	storageKeyLen = keys.AddressLen + slotLen
)

// EVM value sizes. These are not configurable: FlatKV parses the value by the key it arrives under and
// rejects a write whose length does not match, so a record of any other size never reaches disk.
const (
	accountRecordLen    = vtype.CodeHashLen
	storageSlotValueLen = vtype.SlotLen
)

// accountKeyPrefix stands in for the account record. FlatKV has no way to force an update of the
// balance field, and writing the code hash updates the account DB, which is the part that matters
// here.
const accountKeyPrefix = keys.EVMKeyCodeHash

// The names of the identifier counters the benchmark persists in state, so a reopened data directory
// resumes generating identifiers where the previous run stopped.
const (
	accountIDCounterName = "accountIdCounterKey"
	erc20IDCounterName   = "erc20IdCounterKey"
)

// counterKeys are the keys the identifier counters are stored under, in the order every block commits
// them. They are built once because every block writes both.
var counterKeys = [...][]byte{
	keys.BuildEVMKey(keys.EVMKeyCode, paddedCounterKey(accountIDCounterName)),
	keys.BuildEVMKey(keys.EVMKeyCode, paddedCounterKey(erc20IDCounterName)),
}

// paddedCounterKey pads a name out to an address, which is what the EVM key builders accept.
func paddedCounterKey(name string) []byte {
	padded := make([]byte, keys.AddressLen)
	copy(padded, name)
	return padded
}

// accountPopulation is the identifier layout setup lays down. The fee collection account takes
// identifier zero, the hot accounts follow, then the dormant accounts, and the cold accounts take the
// highest identifiers.
type accountPopulation struct {
	// The number of accounts a fully prepopulated run holds.
	total int64

	// The lowest identifier belonging to a cold account.
	firstCold int64
}

// plannedAccountPopulation returns the layout the configured counts describe.
//
// Cold accounts are placed last because RandomAccount draws them from the identifiers just below the
// newest account rather than from a recorded set, so they have to occupy the top of the range.
func plannedAccountPopulation(config *GigasimConfig) accountPopulation {
	// One account above the configured populations, because the fee collection account takes identifier
	// zero and is never selected as a transfer counterparty.
	total := 1 + int64(config.NumberOfHotAccounts) +
		int64(config.MinimumNumberOfDormantAccounts) +
		int64(config.MinimumNumberOfColdAccounts)
	return accountPopulation{
		total:     total,
		firstCold: total - int64(config.MinimumNumberOfColdAccounts),
	}
}

// accountModel picks which accounts, contracts and storage slots each transaction touches, and mints
// new ones. It holds the account population the benchmark's read and write distribution is drawn from.
//
// Not thread safe: it belongs to the block generator's goroutine.
type accountModel struct {
	config *GigasimConfig

	// The state DB new accounts and contracts are written to during setup.
	state *executionState

	rand *crand.CannedRandom

	// The identifier the next account created takes, and so also the number of accounts in existence.
	nextAccountID int64

	// The identifier the next ERC20 contract created takes.
	nextErc20ContractID int64

	// The highest account identifier that existed before the block being built. Executors run
	// concurrently, so an account minted for this block may not be committed when another transaction
	// in it reads; selection stays at or below this to keep every read target real.
	highestSafeAccountID int64

	// The number of cold accounts, which grows as new non-dormant accounts are created.
	numberOfColdAccounts int64

	// The fee collection account, held by every transaction and therefore cached.
	feeAccount []byte

	metrics *GigasimMetrics
}

// newAccountModel resumes the account population recorded in state, which is empty for a fresh data
// directory.
func newAccountModel(
	config *GigasimConfig,
	state *executionState,
	rand *crand.CannedRandom,
	metrics *GigasimMetrics,
) *accountModel {
	nextAccountID := readCounter(state, counterKeys[0])
	nextErc20ContractID := readCounter(state, counterKeys[1])

	return &accountModel{
		config:               config,
		state:                state,
		rand:                 rand,
		nextAccountID:        nextAccountID,
		nextErc20ContractID:  nextErc20ContractID,
		highestSafeAccountID: nextAccountID - 1,
		numberOfColdAccounts: max(0, nextAccountID-plannedAccountPopulation(config).firstCold),
		feeAccount:           keys.BuildEVMKey(accountKeyPrefix, rand.Address(accountPrefix, 0, keys.AddressLen)),
		metrics:              metrics,
	}
}

// readCounter reads an identifier counter from state, answering 0 when the key has never been written.
func readCounter(state *executionState, key []byte) int64 {
	encoded, found := state.Get(key)
	if !found {
		return 0
	}
	//nolint:gosec // G115 - persisted benchmark counter, overflow acceptable
	return int64(binary.BigEndian.Uint64(encoded))
}

// NextAccountID returns the identifier the next account created will take, which is also the number of
// accounts in existence.
func (a *accountModel) NextAccountID() int64 {
	return a.nextAccountID
}

// NextErc20ContractID returns the identifier the next ERC20 contract created will take.
func (a *accountModel) NextErc20ContractID() int64 {
	return a.nextErc20ContractID
}

// Counters snapshots the identifier counters so they can be committed from another goroutine.
func (a *accountModel) Counters() identifierCounters {
	return identifierCounters{
		nextAccountID:       a.nextAccountID,
		nextErc20ContractID: a.nextErc20ContractID,
	}
}

// FeeCollectionAddress returns the account every transaction credits its fee to.
func (a *accountModel) FeeCollectionAddress() []byte {
	return a.feeAccount
}

// CreateAccount mints an account and writes it to state, joining either the cold population that
// transactions select from or the dormant one that is never selected.
func (a *accountModel) CreateAccount(isCold bool) {
	accountID := a.nextAccountID
	a.nextAccountID++

	address := keys.BuildEVMKey(accountKeyPrefix, a.rand.Address(accountPrefix, accountID, keys.AddressLen))

	record := make([]byte, accountRecordLen)
	//nolint:gosec // G115 - simulated balance, overflow acceptable
	binary.BigEndian.PutUint64(record[:8], uint64(a.rand.Int64()))
	copy(record[8:], a.rand.Bytes(accountRecordLen-8))
	a.state.Put(address, record)

	if isCold {
		a.numberOfColdAccounts++
	}
}

// CreateErc20Contract mints an ERC20 contract and writes its code to state.
func (a *accountModel) CreateErc20Contract() {
	contractID := a.nextErc20ContractID
	a.nextErc20ContractID++

	address := keys.BuildEVMKey(keys.EVMKeyCode, a.rand.Address(contractPrefix, contractID, keys.AddressLen))
	a.state.Put(address, a.rand.Bytes(a.config.Erc20ContractSize))
}

// RandomAccount selects the account for one side of a transfer, minting a new one with the configured
// probability. It reports whether the account is new.
func (a *accountModel) RandomAccount() (address []byte, isNew bool, err error) {
	if a.rand.Float64() < a.config.HotAccountProbability {
		accountID := a.rand.Int64Range(1, int64(a.config.NumberOfHotAccounts)+1)
		return a.accountAddress(accountID), false, nil
	}

	if a.rand.Float64() < a.config.NewAccountProbability {
		accountID := a.nextAccountID
		a.nextAccountID++
		if a.rand.Float64() >= a.config.NewAccountDormancyProbability {
			a.numberOfColdAccounts++
		}
		return a.accountAddress(accountID), true, nil
	}

	// The cold population sits immediately below the newest account that existed before this block, so
	// the window slides forward as accounts are minted.
	lastColdAccountID := a.highestSafeAccountID + 1
	firstColdAccountID := lastColdAccountID - a.numberOfColdAccounts
	if firstColdAccountID >= lastColdAccountID {
		return nil, false, fmt.Errorf("no cold accounts available to select from")
	}
	return a.accountAddress(a.rand.Int64Range(firstColdAccountID, lastColdAccountID)), false, nil
}

// RandomAccountSlot selects one of the ERC20 storage slots an account may touch.
func (a *accountModel) RandomAccountSlot() []byte {
	slotID := a.rand.Int64Range(0, int64(a.config.Erc20InteractionsPerAccount)*a.nextAccountID+1)
	return keys.BuildEVMKey(keys.EVMKeyStorage, a.rand.Address(ethStoragePrefix, slotID, storageKeyLen))
}

// RandomErc20Contract selects the contract a transaction interacts with, from the hot set with the
// configured probability and from the rest otherwise.
func (a *accountModel) RandomErc20Contract() ([]byte, error) {
	hotSetSize := min(int64(a.config.HotErc20ContractSetSize), a.nextErc20ContractID)
	if hotSetSize <= 0 {
		return nil, fmt.Errorf("no ERC20 contracts exist to select from")
	}

	if a.rand.Float64() < a.config.HotErc20ContractProbability {
		return a.contractAddress(a.rand.Int64Range(0, hotSetSize)), nil
	}
	if a.nextErc20ContractID <= hotSetSize {
		return nil, fmt.Errorf("no cold ERC20 contracts exist: %d contracts, hot set of %d",
			a.nextErc20ContractID, hotSetSize)
	}
	return a.contractAddress(a.rand.Int64Range(hotSetSize, a.nextErc20ContractID)), nil
}

// ReportEndOfBlock marks every account minted so far as safe to select, which it becomes once the
// block that created it has been committed.
func (a *accountModel) ReportEndOfBlock() {
	a.highestSafeAccountID = a.nextAccountID - 1
	hot := int64(a.config.NumberOfHotAccounts)
	// Every account belongs to exactly one set, less the fee collection account at identifier zero, so
	// what is neither hot nor cold is dormant.
	dormant := max(0, a.nextAccountID-1-hot-a.numberOfColdAccounts)
	a.metrics.SetAccountCounts(a.nextAccountID, hot, a.numberOfColdAccounts, dormant)
	a.metrics.SetErc20ContractCount(a.nextErc20ContractID)
}

// Rand returns the random source, which shares the account model's single-threaded ownership.
func (a *accountModel) Rand() *crand.CannedRandom {
	return a.rand
}

// Close releases the random source, which holds the benchmark's largest single allocation.
func (a *accountModel) Close() {
	a.rand = nil
}

// accountAddress returns the state key holding an account's record.
func (a *accountModel) accountAddress(accountID int64) []byte {
	return keys.BuildEVMKey(accountKeyPrefix, a.rand.Address(accountPrefix, accountID, keys.AddressLen))
}

// contractAddress returns the state key holding an ERC20 contract's code.
func (a *accountModel) contractAddress(contractID int64) []byte {
	return keys.BuildEVMKey(keys.EVMKeyCode, a.rand.Address(contractPrefix, contractID, keys.AddressLen))
}
