package cryptosim

import (
	"encoding/binary"
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-db/common/evm"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/bench/wrappers"
)

const (
	// Used to store the next account ID in the database.
	accountIdCounterKey = "accountIdCounterKey"
	// Used to store the next ERC20 contract ID in the database.
	erc20IdCounterKey = "erc20IdCounterKey"
)

// Generates random data for the benchmark. This is not a thread safe utility.
type DataGenerator struct {
	nextAccountID       int64
	nextErc20ContractID int64

	rand *CannedRandom
}

// Creates a new data generator.
func NewDataGenerator(
	db wrappers.DBWrapper,
	rand *CannedRandom) (*DataGenerator, error) {

	nextAccountIDBinary, found, err := db.Read(AccountIDCounterKey())
	if err != nil {
		return nil, fmt.Errorf("failed to read account counter: %w", err)
	}
	var nextAccountID int64
	if found {
		nextAccountID = int64(binary.BigEndian.Uint64(nextAccountIDBinary))
	}

	fmt.Printf("There are currently %s keys in the database.\n", int64Commas(nextAccountID))

	// erc20IdCounterBytes := make([]byte, 20)
	// copy(erc20IdCounterBytes, []byte(erc20IdCounterKey))
	// erc20IDCounterKey := evm.BuildMemIAVLEVMKey(evm.EVMKeyNonce, erc20IdCounterBytes)

	nextErc20ContractIDBinary, found, err := db.Read(Erc20IDCounterKey())
	if err != nil {
		return nil, fmt.Errorf("failed to read ERC20 contract counter: %w", err)
	}
	var nextErc20ContractID int64
	if found {
		nextErc20ContractID = int64(binary.BigEndian.Uint64(nextErc20ContractIDBinary))
	}

	fmt.Printf("There are currently %s ERC20 contracts in the database.\n", int64Commas(nextErc20ContractID))

	return &DataGenerator{
		nextAccountID:       nextAccountID,
		nextErc20ContractID: nextErc20ContractID,
		rand:                rand,
	}, nil
}

// Get the key for the account ID counter in the database.
func AccountIDCounterKey() []byte {
	return evm.BuildMemIAVLEVMKey(evm.EVMKeyNonce, []byte(accountIdCounterKey))
}

// Get the key for the ERC20 contract ID counter in the database.
func Erc20IDCounterKey() []byte {
	return evm.BuildMemIAVLEVMKey(evm.EVMKeyNonce, []byte(erc20IdCounterKey))
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
	cryptosim *CryptoSim,
	// The number of bytes to allocate for the account data.
	accountSize int,
	// If true, the account will be immediately written to the database.
	write bool,
) (id int64, address []byte, err error) {

	accountID := d.nextAccountID
	d.nextAccountID++

	// Use memiavl code key format (0x07 + addr) so FlatKV persists account data.
	addr := d.rand.Address(accountPrefix, accountID)
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

	err = cryptosim.put(address, accountData)
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

	erc20Address := d.rand.Address(contractPrefix, erc20ContractID)
	address = evm.BuildMemIAVLEVMKey(evm.EVMKeyStorage, erc20Address)

	if !write {
		return erc20ContractID, address, nil
	}

	erc20Data := make([]byte, erc20ContractSize)
	randomBytes := d.rand.Bytes(erc20ContractSize)
	copy(erc20Data, randomBytes)

	err = cryptosim.put(address, erc20Data)
	if err != nil {
		return 0, nil, fmt.Errorf("failed to put ERC20 contract: %w", err)
	}

	return erc20ContractID, address, nil
}
