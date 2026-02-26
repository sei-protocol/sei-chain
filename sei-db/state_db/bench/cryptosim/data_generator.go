package cryptosim

import (
	"encoding/binary"
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-db/common/evm"
)

const (
	// Used to store the next account ID in the database.
	accountIdCounterKey = "accountIdCounterKey"
	// Used to store the next ERC20 contract ID in the database.
	erc20IdCounterKey = "erc20IdCounterKey"
)

// Generates random data for the benchmark. This is not a thread safe utility.
type DataGenerator struct {
	config *CryptoSimConfig

	// The next account ID to be used when creating a new account.
	nextAccountID int64

	// The next ERC20 contract ID to be used when creating a new ERC20 contract.
	nextErc20ContractID int64

	// The random number generator.
	rand *CannedRandom

	// The address of the fee account (i.e. the account that collects gas fees). This is a special account
	// and has account ID 0. Since we reuse this account very often, it is cached for performance.
	feeCollectionAddress []byte

	// The database for the benchmark.
	database *Database
}

// Creates a new data generator.
func NewDataGenerator(
	config *CryptoSimConfig,
	database *Database,
	rand *CannedRandom,
) (*DataGenerator, error) {

	nextAccountIDBinary, found, err := database.Get(AccountIDCounterKey())
	if err != nil {
		return nil, fmt.Errorf("failed to read account counter: %w", err)
	}
	var nextAccountID int64
	if found {
		nextAccountID = int64(binary.BigEndian.Uint64(nextAccountIDBinary))
	}

	fmt.Printf("There are currently %s keys in the database.\n", int64Commas(nextAccountID))

	nextErc20ContractIDBinary, found, err := database.Get(Erc20IDCounterKey())
	if err != nil {
		return nil, fmt.Errorf("failed to read ERC20 contract counter: %w", err)
	}
	var nextErc20ContractID int64
	if found {
		nextErc20ContractID = int64(binary.BigEndian.Uint64(nextErc20ContractIDBinary))
	}

	fmt.Printf("There are currently %s ERC20 contracts in the database.\n", int64Commas(nextErc20ContractID))

	// Use EVMKeyCode for account data; EVMKeyNonce only accepts 8-byte values.
	feeCollectionAddress := evm.BuildMemIAVLEVMKey(
		evm.EVMKeyCode,
		rand.Address(accountPrefix, 0, AddressLen),
	)

	return &DataGenerator{
		config:               config,
		nextAccountID:        nextAccountID,
		nextErc20ContractID:  nextErc20ContractID,
		rand:                 rand,
		feeCollectionAddress: feeCollectionAddress,
		database:             database,
	}, nil
}

// Get the next account ID to be used when creating a new account. This is also the total number of accounts
// currently in the database.
func (d *DataGenerator) NextAccountID() int64 {
	return d.nextAccountID
}

// Get the next ERC20 contract ID to be used when creating a new ERC20 contract. This is also the total number of
// ERC20 contracts currently in the database.
func (d *DataGenerator) NextErc20ContractID() int64 {
	return d.nextErc20ContractID
}

// Creates a new account and optionally writes it to the database. Returns the address of the new account.
func (d *DataGenerator) CreateNewAccount(
	// The number of bytes to allocate for the account data.
	accountSize int,
	// If true, the account will be immediately written to the database.
	write bool,
) (id int64, address []byte, err error) {

	accountID := d.nextAccountID
	d.nextAccountID++

	// Use EVMKeyCode for account data (balance+padding); EVMKeyNonce only accepts 8-byte values.
	addr := d.rand.Address(accountPrefix, accountID, AddressLen)
	address = evm.BuildMemIAVLEVMKey(evm.EVMKeyCode, addr)

	if !write {
		return accountID, address, nil
	}

	balance := d.rand.Int64()

	accountData := make([]byte, accountSize)

	binary.BigEndian.PutUint64(accountData[:8], uint64(balance))

	// The remaining bytes are random data for padding.
	randomBytes := d.rand.Bytes(accountSize - 8)
	copy(accountData[8:], randomBytes)

	err = d.database.Put(address, accountData)
	if err != nil {
		return 0, nil, fmt.Errorf("failed to put account: %w", err)
	}

	return accountID, address, nil
}

// Creates a new ERC20 contract and optionally writes it to the database. Returns the address of the new ERC20 contract.
func (d *DataGenerator) CreateNewErc20Contract(
	cryptosim *CryptoSim,
	// The number of bytes to allocate for the ERC20 contract data.
	erc20ContractSize int,
	// If true, the ERC20 contract will be immediately written to the database.
	write bool,
) (id int64, address []byte, err error) {
	erc20ContractID := d.nextErc20ContractID
	d.nextErc20ContractID++

	erc20Address := d.rand.Address(contractPrefix, erc20ContractID, AddressLen)
	address = evm.BuildMemIAVLEVMKey(evm.EVMKeyCode, erc20Address)

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

// Select a random account for a transaction. If an existing account is selected then its ID is guaranteed to be
// less or equal to maxAccountID. If a new account is created, it may have an ID greater than maxAccountID.
func (d *DataGenerator) RandomAccount(maxAccountID int64) (id int64, address []byte, isNew bool, err error) {

	hot := d.rand.Float64() < d.config.HotAccountProbability

	if hot {
		firstHotAccountID := 1
		lastHotAccountID := d.config.HotAccountSetSize
		accountID := d.rand.Int64Range(int64(firstHotAccountID), int64(lastHotAccountID+1))
		addr := d.rand.Address(accountPrefix, accountID, AddressLen)
		return accountID, evm.BuildMemIAVLEVMKey(evm.EVMKeyCode, addr), false, nil
	} else {

		new := d.rand.Float64() < d.config.NewAccountProbability
		if new {
			// create a new account
			id, address, err := d.CreateNewAccount(d.config.PaddedAccountSize, false)
			if err != nil {
				return 0, nil, false, fmt.Errorf("failed to create new account: %w", err)
			}
			return id, address, true, nil
		}

		// select an existing account at random

		firstNonHotAccountID := d.config.HotAccountSetSize + 1
		accountID := d.rand.Int64Range(int64(firstNonHotAccountID), maxAccountID+1)
		addr := d.rand.Address(accountPrefix, accountID, AddressLen)
		return accountID, evm.BuildMemIAVLEVMKey(evm.EVMKeyCode, addr), false, nil
	}
}

// Selects a random account slot for a transaction.
// Uses EVMKeyStorage with addr||slot (AddressLen+SlotLen bytes) for proper storage slot format.
func (d *DataGenerator) randomAccountSlot(accountID int64) ([]byte, error) {
	slotNumber := d.rand.Int64Range(0, int64(d.config.Erc20InteractionsPerAccount))
	slotID := accountID*int64(d.config.Erc20InteractionsPerAccount) + slotNumber

	storageKeyBytes := d.rand.Address(ethStoragePrefix, slotID, StorageKeyLen)
	return evm.BuildMemIAVLEVMKey(evm.EVMKeyStorage, storageKeyBytes), nil
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
		addr := d.rand.Address(contractPrefix, erc20ContractID, AddressLen)
		return evm.BuildMemIAVLEVMKey(evm.EVMKeyCode, addr), nil
	}

	// Otherwise, select a cold ERC20 contract at random.
	if d.nextErc20ContractID <= int64(d.config.HotErc20ContractSetSize) {
		return nil, fmt.Errorf("no cold ERC20 contracts available (have %d, hot set size %d)",
			d.nextErc20ContractID, d.config.HotErc20ContractSetSize)
	}
	erc20ContractID := d.rand.Int64Range(
		int64(d.config.HotErc20ContractSetSize),
		d.nextErc20ContractID)
	addr := d.rand.Address(contractPrefix, erc20ContractID, AddressLen)
	return evm.BuildMemIAVLEVMKey(evm.EVMKeyCode, addr), nil
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
