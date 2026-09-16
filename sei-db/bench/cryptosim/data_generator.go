package cryptosim

import (
	"encoding/binary"
	"fmt"
	"math"

	"github.com/sei-protocol/sei-chain/sei-db/common/keys"
	crand "github.com/sei-protocol/sei-chain/sei-db/common/rand"
)

const (
	// Used to store the next account ID in the database.
	accountIdCounterKey = "accountIdCounterKey"
	// Used to store the next ERC20 contract ID in the database.
	erc20IdCounterKey = "erc20IdCounterKey"

	// Use the code hash as a proxy. There is currently no mechanism to force FlatKV to update the account balance
	// field, and code hash keys will cause the account DB to get updated, which is the important part for this
	// simulation.
	accountKeyPrefix = keys.EVMKeyCodeHash
)

// selectionPatternCycle is the number of account selections the hot pattern repeats over. A
// probability is rounded to this many parts, so it also sets the resolution of the hot share.
const selectionPatternCycle = 1_000_000

// Generates random data for the benchmark. This is not a thread safe utility.
type DataGenerator struct {
	config *CryptoSimConfig

	// The next account ID to be used when creating a new account.
	nextAccountID int64

	// The next ERC20 contract ID to be used when creating a new ERC20 contract.
	nextErc20ContractID int64

	// The random number generator.
	rand *crand.CannedRandom

	// The address of the fee account (i.e. the account that collects gas fees). This is a special account
	// and has account ID 0. Since we reuse this account very often, it is cached for performance.
	feeCollectionAddress []byte

	// The database for the benchmark.
	database *Database

	// The highest account ID that has been read in the current block.
	// Since there are multiple threads of execution, it's possible that one executor may create a new account,
	// and another may attempt to read/write it before the account is actually created. To avoid this, we will only
	// choose read/write targets from acccounts that were created before the current block. This field tracks the
	// highest account ID that was created before the current block.
	highestSafeAccountIDInBlock int64

	// How many account selections this generator has served. Which selections create an account is a
	// function of this count alone, which is what makes a fork's account IDs computable in advance.
	selectionCount int64

	// The first account ID this generator may mint, so that what it has minted is a subtraction.
	firstMintableAccountID int64

	// The number of cold accounts this generator started from, so that what it has minted is a
	// subtraction.
	coldAccountsAtStart int64

	// The current number of cold accounts. These are accounts that are not used frequently, but are not
	// entirely dormant.
	numberOfColdAccounts int64

	// The metrics for the benchmark.
	metrics *CryptosimMetrics
}

// Creates a new data generator.
func NewDataGenerator(
	config *CryptoSimConfig,
	database *Database,
	rand *crand.CannedRandom,
	metrics *CryptosimMetrics,
) *DataGenerator {

	nextAccountIDBinary, found := database.Get(AccountIDCounterKey())
	var nextAccountID int64
	if found {
		//nolint:gosec // G115 - persisted counter value, overflow acceptable
		nextAccountID = int64(binary.BigEndian.Uint64(nextAccountIDBinary))
	}

	fmt.Printf("There are currently %s keys in the database.\n", int64Commas(nextAccountID))
	hot := min(int64(config.NumberOfHotAccounts), max(0, nextAccountID-1))
	cold := min(int64(config.MinimumNumberOfColdAccounts), max(0, nextAccountID-1-hot))
	metrics.SetTotalNumberOfAccounts(nextAccountID, hot, cold)

	nextErc20ContractIDBinary, found := database.Get(Erc20IDCounterKey())
	var nextErc20ContractID int64
	if found {
		//nolint:gosec // G115 - persisted counter value, overflow acceptable
		nextErc20ContractID = int64(binary.BigEndian.Uint64(nextErc20ContractIDBinary))
	}

	fmt.Printf("There are currently %s ERC20 contracts in the database.\n", int64Commas(nextErc20ContractID))
	metrics.SetTotalNumberOfERC20Contracts(nextErc20ContractID)

	feeCollectionAddress := keys.BuildEVMKey(
		accountKeyPrefix,
		rand.Address(accountPrefix, 0, keys.AddressLen),
	)

	return &DataGenerator{
		config:                      config,
		nextAccountID:               nextAccountID,
		firstMintableAccountID:      nextAccountID,
		nextErc20ContractID:         nextErc20ContractID,
		rand:                        rand,
		feeCollectionAddress:        feeCollectionAddress,
		database:                    database,
		highestSafeAccountIDInBlock: nextAccountID - 1,
		numberOfColdAccounts:        int64(config.MinimumNumberOfColdAccounts),
		coldAccountsAtStart:         int64(config.MinimumNumberOfColdAccounts),
		metrics:                     metrics,
	}
}

// Get the next account ID to be used when creating a new account. This is also the total number of accounts
// currently in the database.
func (d *DataGenerator) NextAccountID() int64 {
	return d.nextAccountID
}

// NumberOfColdAccounts returns the current count of cold accounts.
func (d *DataGenerator) NumberOfColdAccounts() int64 {
	return d.numberOfColdAccounts
}

// ReportAccountCounts updates the metrics with the current account counts (total, hot, cold).
func (d *DataGenerator) ReportAccountCounts() {
	d.metrics.SetTotalNumberOfAccounts(d.nextAccountID, int64(d.config.NumberOfHotAccounts), d.numberOfColdAccounts)
}

// Get the next ERC20 contract ID to be used when creating a new ERC20 contract. This is also the total number of
// ERC20 contracts currently in the database.
func (d *DataGenerator) NextErc20ContractID() int64 {
	return d.nextErc20ContractID
}

// Creates a new account and optionally writes it to the database. Returns the address of the new
// account and whether it is a cold account (vs dormant).
func (d *DataGenerator) CreateNewAccount(
	// The number of bytes to allocate for the account data.
	accountSize int,
	// If true, the account will be immediately written to the database.
	write bool,
) (id int64, address []byte, isCold bool, err error) {

	accountID := d.nextAccountID
	d.nextAccountID++

	addr := d.rand.Address(accountPrefix, accountID, keys.AddressLen)
	address = keys.BuildEVMKey(accountKeyPrefix, addr)

	isCold = d.rand.Float64() >= d.config.NewAccountDormancyProbability

	if !write {
		if isCold {
			d.numberOfColdAccounts++
		}
		return accountID, address, isCold, nil
	}

	balance := d.rand.Int64()

	accountData := make([]byte, accountSize)

	//nolint:gosec // G115 - balance is benchmark simulation value, overflow acceptable
	binary.BigEndian.PutUint64(accountData[:8], uint64(balance))

	// The remaining bytes are random data for padding.
	randomBytes := d.rand.Bytes(accountSize - 8)
	copy(accountData[8:], randomBytes)

	err = d.database.Put(address, accountData)
	if err != nil {
		return 0, nil, false, fmt.Errorf("failed to put account: %w", err)
	}

	if isCold {
		d.numberOfColdAccounts++
	}

	return accountID, address, isCold, nil
}

// Creates a new ERC20 contract and optionally writes it to the database. Returns the address of the new ERC20 contract.
func (d *DataGenerator) CreateNewErc20Contract(
	// The number of bytes to allocate for the ERC20 contract data.
	erc20ContractSize int,
	// If true, the ERC20 contract will be immediately written to the database.
	write bool,
) (id int64, address []byte, err error) {
	erc20ContractID := d.nextErc20ContractID
	d.nextErc20ContractID++

	erc20Address := d.rand.Address(contractPrefix, erc20ContractID, keys.AddressLen)
	address = keys.BuildEVMKey(keys.EVMKeyCode, erc20Address)

	if !write {
		return erc20ContractID, address, nil
	}

	erc20Data := make([]byte, erc20ContractSize)
	randomBytes := d.rand.Bytes(erc20ContractSize)
	copy(erc20Data, randomBytes)

	err = d.database.Put(address, erc20Data)
	if err != nil {
		return 0, nil, fmt.Errorf("failed to put ERC20 contract: %w", err)
	}

	return erc20ContractID, address, nil
}

// Select a random account for a transaction. A newly created account may have an ID greater than any
// existing one; an account selected from the hot set or the cold window never does.
//
// Which of the three a selection is follows from its position in the selection sequence rather than
// from a draw, so the accounts a run of selections creates are known before any of them run. Which
// account it lands on within the hot set or the cold window is still drawn at random.
func (d *DataGenerator) RandomAccount() (id int64, address []byte, isNew bool, err error) {

	selection := d.selectionCount
	d.selectionCount++

	// Creating takes precedence over a hot selection where the two coincide. Both patterns run over the
	// same counter, so they intersect, and letting the hot selection win there would make the accounts
	// a run of selections creates depend on the hot pattern — which is exactly what the arithmetic that
	// reserves ID ranges for parallel generation cannot see.
	if d.selectionCreatesAccount(selection) {
		id, address, _, err := d.CreateNewAccount(d.config.PaddedAccountSize, false)
		if err != nil {
			return 0, nil, false, fmt.Errorf("failed to create new account: %w", err)
		}
		return id, address, true, nil
	}

	if d.selectionIsHot(selection) {
		firstHotAccountID := 1
		lastHotAccountID := d.config.NumberOfHotAccounts
		accountID := d.rand.Int64Range(int64(firstHotAccountID), int64(lastHotAccountID+1))
		addr := d.rand.Address(accountPrefix, accountID, keys.AddressLen)
		return accountID, keys.BuildEVMKey(accountKeyPrefix, addr), false, nil
	}

	// Select an existing account from the cold window at random.
	lastLegalColdAccountID := d.highestSafeAccountIDInBlock + 1
	firstLegalColdAccountID := lastLegalColdAccountID - d.numberOfColdAccounts

	accountID := d.rand.Int64Range(firstLegalColdAccountID, lastLegalColdAccountID)
	addr := d.rand.Address(accountPrefix, accountID, keys.AddressLen)
	return accountID, keys.BuildEVMKey(accountKeyPrefix, addr), false, nil
}

// selectionCreatesAccount reports whether the selection at the given count creates a new account.
//
// A function of the count alone, so the accounts any span of selections will create are known before
// any of them run. A cadence of zero never creates.
func (d *DataGenerator) selectionCreatesAccount(selection int64) bool {
	cadence := int64(d.config.SelectionsPerNewAccount)
	if cadence == 0 {
		return false
	}
	return selection%cadence == 0
}

// selectionIsHot reports whether the selection at the given count draws from the hot set.
//
// A function of the count alone, like selectionCreatesAccount(). The hot selections are spread evenly
// through each cycle of selectionPatternCycle, so their share of a cycle is HotAccountProbability at
// that resolution, and a run of selections anywhere in the sequence carries that share.
func (d *DataGenerator) selectionIsHot(selection int64) bool {
	share := int64(math.Round(d.config.HotAccountProbability * selectionPatternCycle))
	position := selection % selectionPatternCycle
	return (position+1)*share/selectionPatternCycle > position*share/selectionPatternCycle
}

// accountsMinted reports how many accounts this generator has minted since it was forked.
func (d *DataGenerator) accountsMinted() int64 {
	return d.nextAccountID - d.firstMintableAccountID
}

// AccountsMintedPerSelections returns how many accounts a run of selections mints, given how many
// selections precede it. Both are needed because a cadence hits on the count itself, so where a run
// starts decides how many hits it contains.
func (d *DataGenerator) AccountsMintedPerSelections(precedingSelections int64, selections int64) int64 {
	cadence := int64(d.config.SelectionsPerNewAccount)
	if cadence == 0 || selections <= 0 {
		return 0
	}
	hitsThrough := func(count int64) int64 {
		if count <= 0 {
			return 0
		}
		// Counts multiples of cadence in [0, count), and 0 is a multiple.
		return (count-1)/cadence + 1
	}
	return hitsThrough(precedingSelections+selections) - hitsThrough(precedingSelections)
}

// Fork returns a generator that creates accounts from firstAccountID onwards, for one worker's share
// of a block.
//
// The fork shares the immutable random buffer through a cursor of its own; where that cursor reads,
// and which selection it is serving, are both set per transaction by BeginTransaction(), so what a
// transaction draws and whether it creates an account follow from its index rather than from which
// fork served it. Two forks never create the same ID, because a selection creates an account by its
// position alone, so the run of IDs a fork will use is the run the caller reserved for it.
//
// The account selection window is frozen at the value the parent holds, so every fork of one block
// draws from the same set of pre-existing accounts — which is what the block-at-a-time visibility rule
// already guaranteed when selections were served in sequence.
func (d *DataGenerator) Fork(firstAccountID int64) *DataGenerator {

	fork := *d
	fork.rand = d.rand.Clone(false)
	fork.nextAccountID = firstAccountID
	fork.firstMintableAccountID = firstAccountID
	fork.coldAccountsAtStart = d.numberOfColdAccounts
	fork.highestSafeAccountIDInBlock = d.highestSafeAccountIDInBlock
	return &fork
}

// BeginTransaction points the generator at the randomness belonging to one transaction, and at the
// selections that transaction serves.
//
// transactionIndex counts from the first transaction of the run rather than from the first of its
// block, which both patterns over the selection count depend on: a cadence measured from a block's own
// start would restart at every boundary, which rounds the accounts a block creates up to a whole
// number and floors it at one however large the cadence is.
//
// Both are functions of which transaction it is rather than of how many came before on this goroutine,
// which is what makes a block's contents independent of how it was divided among workers: the same
// transaction index always draws the same values and creates the same accounts.
func (d *DataGenerator) BeginTransaction(transactionIndex int64) {
	d.rand.SeekTo(transactionIndex)
	d.selectionCount = transactionIndex * selectionsPerTransaction
}

// AdoptForkResults folds what a block's forks minted back into the generator they were taken from, so
// the next block's arithmetic starts from the right place.
func (d *DataGenerator) AdoptForkResults(accountsMinted int64, coldAccountsMinted int64) {
	d.nextAccountID += accountsMinted
	d.numberOfColdAccounts += coldAccountsMinted
}

// ColdAccountsMinted reports how many of the accounts this generator minted since it was forked were
// cold rather than dormant.
func (d *DataGenerator) ColdAccountsMinted() int64 {
	return d.numberOfColdAccounts - d.coldAccountsAtStart
}

// AccountsMinted reports how many accounts this generator minted since it was forked.
func (d *DataGenerator) AccountsMinted() int64 {
	return d.accountsMinted()
}

// Selects a random account slot for a transaction.
// Uses EVMKeyStorage with addr||slot (AddressLen+SlotLen bytes) for proper storage slot format.
func (d *DataGenerator) randomAccountSlot(accountID int64) ([]byte, error) {
	slotNumber := d.rand.Int64Range(0, int64(d.config.Erc20InteractionsPerAccount))
	slotID := accountID*int64(d.config.Erc20InteractionsPerAccount) + slotNumber

	storageKeyBytes := d.rand.Address(ethStoragePrefix, slotID, StorageKeyLen)
	return keys.BuildEVMKey(keys.EVMKeyStorage, storageKeyBytes), nil
}

// Selects a random ERC20 contract for a transaction.
func (d *DataGenerator) randomErc20Contract() ([]byte, error) {

	hot := d.rand.Float64() < d.config.HotErc20ContractProbability

	if hot {
		hotMax := int64(d.config.HotErc20ContractSetSize)
		if d.nextErc20ContractID < hotMax {
			hotMax = d.nextErc20ContractID
		}
		if hotMax <= 0 {
			return nil, fmt.Errorf("no ERC20 contracts available for hot selection")
		}
		erc20ContractID := d.rand.Int64Range(0, hotMax)
		addr := d.rand.Address(contractPrefix, erc20ContractID, keys.AddressLen)
		return keys.BuildEVMKey(keys.EVMKeyCode, addr), nil
	}

	// Otherwise, select a cold ERC20 contract at random.
	if d.nextErc20ContractID <= int64(d.config.HotErc20ContractSetSize) {
		return nil, fmt.Errorf("no cold ERC20 contracts available (have %d, hot set size %d)",
			d.nextErc20ContractID, d.config.HotErc20ContractSetSize)
	}
	erc20ContractID := d.rand.Int64Range(
		int64(d.config.HotErc20ContractSetSize),
		d.nextErc20ContractID)
	addr := d.rand.Address(contractPrefix, erc20ContractID, keys.AddressLen)
	return keys.BuildEVMKey(keys.EVMKeyCode, addr), nil
}

// Close the data generator and release any resources.
func (d *DataGenerator) Close() {
	// Specifically release rand, since it's likely to hold a lot of memory.
	d.rand = nil
}

// Get the address of the fee collection account.
func (d *DataGenerator) FeeCollectionAddress() []byte {
	return d.feeCollectionAddress
}

// Call this to signal that we have reached the end of a block. This is a signal that it is now safe to use
// recently created accounts as read/write targets.
func (d *DataGenerator) ReportEndOfBlock() {
	d.highestSafeAccountIDInBlock = d.nextAccountID - 1
}

// Get the random number generator. Note that the random number generator is not thread safe, and
// so the caller is responsible for ensuring that it is not used concurrently with other calls to the data generator.
func (d *DataGenerator) Rand() *crand.CannedRandom {
	return d.rand
}
