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

// mintCycle is the number of consecutive identifiers one turn of the new-account split covers. A
// class takes a whole number of identifiers out of each cycle, which is what lets any identifier's
// class be computed from the identifier alone, and fixes the resolution of the configured shares at
// a tenth of a percent.
const mintCycle = 1000

// accountPopulation is the identifier layout setup lays down, and the split every identifier above it
// follows. The fee collection account takes identifier zero, the hot accounts follow, then the dormant
// accounts, and the cold accounts take the highest identifiers setup creates.
//
// Nothing here changes once a run starts. Every question about an account — which class it belongs to,
// how many of a class exist below some height — is answered from these numbers and the identifier
// counter, so a resumed run sees exactly the population the run that wrote the data saw.
type accountPopulation struct {
	// The number of accounts a fully prepopulated run holds, and so the first identifier a run mints.
	total int64

	// The number of hot accounts setup creates, holding identifiers [1, 1+hot).
	hot int64

	// The lowest identifier belonging to a cold account setup created.
	firstCold int64

	// The number of cold accounts setup creates, holding identifiers [firstCold, total).
	cold int64

	// How many identifiers of each mintCycle a minted account's class takes. Cold takes the rest.
	mintedHot     int64
	mintedDormant int64
}

// plannedAccountPopulation returns the layout the configured counts and shares describe.
func plannedAccountPopulation(config *GigasimConfig) accountPopulation {
	hot := int64(config.NumberOfHotAccounts)
	cold := int64(config.MinimumNumberOfColdAccounts)
	// One account above the configured populations, because the fee collection account takes identifier
	// zero and is never selected as a transfer counterparty.
	total := 1 + hot + int64(config.MinimumNumberOfDormantAccounts) + cold

	return accountPopulation{
		total:         total,
		hot:           hot,
		firstCold:     total - cold,
		cold:          cold,
		mintedHot:     int64(config.NewAccountHotProbability * mintCycle),
		mintedDormant: int64(config.NewAccountDormantProbability * mintCycle),
	}
}

// mintedCold is how many identifiers of each cycle a minted account's class leaves to cold.
func (p accountPopulation) mintedCold() int64 {
	return mintCycle - p.mintedHot - p.mintedDormant
}

// mintedClassSize returns how many of the first minted identifiers belong to a class taking perCycle
// identifiers out of every cycle, where those identifiers start at offset startOfCycle within it.
func mintedClassSize(minted int64, startOfCycle int64, perCycle int64) int64 {
	if minted <= 0 || perCycle <= 0 {
		return 0
	}
	whole, remainder := minted/mintCycle, minted%mintCycle
	return whole*perCycle + min(max(remainder-startOfCycle, 0), perCycle)
}

// mintedClassMember returns the identifier of the index-th minted account of a class, counting from
// the first account minted. It is the inverse of mintedClassSize, so drawing an index uniformly draws
// an account of that class uniformly.
func (p accountPopulation) mintedClassMember(index int64, startOfCycle int64, perCycle int64) int64 {
	cycle, withinCycle := index/perCycle, index%perCycle
	return p.total + cycle*mintCycle + startOfCycle + withinCycle
}

// counts returns how many accounts of each class exist once nextAccountID accounts have been created.
// Dormant is the remainder, since every account but the fee collection one belongs to exactly a class.
func (p accountPopulation) counts(nextAccountID int64) (hot int64, cold int64, dormant int64) {
	minted := max(0, nextAccountID-p.total)
	hot = min(p.hot, max(0, nextAccountID-1)) + mintedClassSize(minted, 0, p.mintedHot)
	cold = min(p.cold, max(0, nextAccountID-p.firstCold)) +
		mintedClassSize(minted, p.mintedHot+p.mintedDormant, p.mintedCold())
	return hot, cold, max(0, nextAccountID-1-hot-cold)
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

	// The identifier layout, fixed by the config. Population sizes are derived from it rather than
	// counted as accounts are minted, so there is no tally to drift from what the identifiers say.
	population accountPopulation

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
		population:           plannedAccountPopulation(config),
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

// CreateAccount mints an account and writes it to state. Which population it joins follows from the
// identifier it takes, so setup only has to create them in the order the layout describes.
func (a *accountModel) CreateAccount() {
	accountID := a.nextAccountID
	a.nextAccountID++

	address := keys.BuildEVMKey(accountKeyPrefix, a.rand.Address(accountPrefix, accountID, keys.AddressLen))

	record := make([]byte, accountRecordLen)
	//nolint:gosec // G115 - simulated balance, overflow acceptable
	binary.BigEndian.PutUint64(record[:8], uint64(a.rand.Int64()))
	copy(record[8:], a.rand.Bytes(accountRecordLen-8))
	a.state.Put(address, record)
}

// CreateErc20Contract mints an ERC20 contract and writes its code to state.
func (a *accountModel) CreateErc20Contract() {
	contractID := a.nextErc20ContractID
	a.nextErc20ContractID++

	address := keys.BuildEVMKey(keys.EVMKeyCode, a.rand.Address(contractPrefix, contractID, keys.AddressLen))
	a.state.Put(address, a.rand.Bytes(a.config.Erc20ContractSize))
}

// RandomAccount selects the account for one side of a transfer, minting a new one with the configured
// probability. The identifier is returned alongside the address because the storage slots a
// transaction touches are derived from it.
//
// A dormant account is never returned. Dormant identifiers are not excluded by narrowing the range,
// which is what let them be selected before: those minted during a run are interleaved with the hot
// and cold ones, so the classes have to be addressed rather than bounded.
func (a *accountModel) RandomAccount() (address []byte, accountID int64, err error) {
	if a.rand.Float64() < a.config.HotAccountProbability {
		return a.selectHot()
	}

	if a.rand.Float64() < a.config.NewAccountProbability {
		accountID := a.nextAccountID
		a.nextAccountID++
		return a.accountAddress(accountID), accountID, nil
	}

	return a.selectCold()
}

// mintedSoFar is how many accounts have been minted at or below the newest identifier selection may
// reach. Accounts minted for the block being built are excluded: they may not be committed yet.
func (a *accountModel) mintedSoFar() int64 {
	return max(0, a.highestSafeAccountID+1-a.population.total)
}

// selectHot picks uniformly from the hot accounts: those setup created, plus every hot account minted
// since. The minted ones are addressed by index rather than searched for, so the cost does not grow
// with the population.
func (a *accountModel) selectHot() (address []byte, accountID int64, err error) {
	population := a.population
	minted := mintedClassSize(a.mintedSoFar(), 0, population.mintedHot)
	if population.hot+minted == 0 {
		return nil, 0, fmt.Errorf("no hot accounts available to select from")
	}

	index := a.rand.Int64Range(0, population.hot+minted)
	if index < population.hot {
		accountID = 1 + index
	} else {
		accountID = population.mintedClassMember(index-population.hot, 0, population.mintedHot)
	}
	return a.accountAddress(accountID), accountID, nil
}

// selectCold picks uniformly from the cold accounts: those setup created, plus every cold account
// minted since. Cold identifiers take the end of each mint cycle, after the hot and dormant ones.
func (a *accountModel) selectCold() (address []byte, accountID int64, err error) {
	population := a.population
	startOfCycle := population.mintedHot + population.mintedDormant
	minted := mintedClassSize(a.mintedSoFar(), startOfCycle, population.mintedCold())
	if population.cold+minted == 0 {
		return nil, 0, fmt.Errorf("no cold accounts available to select from")
	}

	index := a.rand.Int64Range(0, population.cold+minted)
	if index < population.cold {
		accountID = population.firstCold + index
	} else {
		accountID = population.mintedClassMember(index-population.cold, startOfCycle, population.mintedCold())
	}
	return a.accountAddress(accountID), accountID, nil
}

// RandomAccountSlot selects one of the ERC20 storage slots the given account owns.
//
// Each account owns a contiguous block of Erc20InteractionsPerAccount slots, so the slots a hot
// account touches are as hot as the account is. Drawing from the whole slot space instead would
// spread every read over all accounts' slots and erase the locality the hot set exists to create.
func (a *accountModel) RandomAccountSlot(accountID int64) []byte {
	interactions := int64(a.config.Erc20InteractionsPerAccount)
	slotID := accountID*interactions + a.rand.Int64Range(0, interactions)
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
	hot, cold, dormant := a.population.counts(a.nextAccountID)
	a.metrics.SetAccountCounts(a.nextAccountID, hot, cold, dormant)
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
