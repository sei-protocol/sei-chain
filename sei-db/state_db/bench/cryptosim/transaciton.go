package cryptosim

import (
	"encoding/binary"
	"fmt"
)

// The data needed to execute a transaction.
type transaction struct {
	// The simualted ERC20 contract that will be interacted with. This value is read.
	erc20Contract []byte

	// The source account that will be interacted with. This value is read and written.
	srcAccount []byte
	// If true, the source account is new and needs to be created.
	isSrcNew bool
	// If the source account is new, this is the data that will be written to the account.
	// If not new, this will be nil.
	newSrcData []byte

	// The destination account that will be interacted with. This value is read and written.
	dstAccount []byte
	// If true, the destination account is new and needs to be created.
	isDstNew bool
	// If the destination account is new, this is the data that will be written to the account.
	// If not new, this will be nil.
	newDstData []byte

	// The source account's storage slot that will be interacted with. This value is read and written.
	srcAccountSlot []byte
	// The destination account's storage slot that will be interacted with. This value is read and written.
	dstAccountSlot []byte

	// Pre-generated random value for the source account's new native balance.
	newSrcBalance int64
	// Pre-generated random value for the destination account's new native balance.
	newDstBalance int64
	// Pre-generated random value for the fee collection account's new native balance.
	newFeeBalance int64
	// Pre-generated random value for the source account's ERC20 storage slot.
	newSrcAccountSlot []byte
	// Pre-generated random value for the destination account's ERC20 storage slot.
	newDstAccountSlot []byte
}

// Generate all data needed to execute a transaction.
//
// This method is not thread safe to call concurrently with other calls to BuildTransaction().
func BuildTransaction(cryptosim *CryptoSim) (*transaction, error) {
	srcAccountID, srcAccountAddress, isSrcNew, err := cryptosim.randomAccount()
	if err != nil {
		return nil, fmt.Errorf("failed to select source account: %w", err)
	}
	dstAccountID, dstAccountAddress, isDstNew, err := cryptosim.randomAccount()
	if err != nil {
		return nil, fmt.Errorf("failed to select destination account: %w", err)
	}

	srcAccountSlot, err := cryptosim.randomAccountSlot(srcAccountID)
	if err != nil {
		return nil, fmt.Errorf("failed to select source account slot: %w", err)
	}
	dstAccountSlot, err := cryptosim.randomAccountSlot(dstAccountID)
	if err != nil {
		return nil, fmt.Errorf("failed to select destination account slot: %w", err)
	}
	erc20Contract, err := cryptosim.randomErc20Contract()
	if err != nil {
		return nil, fmt.Errorf("failed to select ERC20 contract: %w", err)
	}

	var newSrcData []byte
	if isSrcNew {
		newSrcData = cryptosim.rand.Bytes(cryptosim.config.PaddedAccountSize)
	}
	var newDstData []byte
	if isDstNew {
		newDstData = cryptosim.rand.Bytes(cryptosim.config.PaddedAccountSize)
	}

	return &transaction{
		srcAccount:        srcAccountAddress,
		isSrcNew:          isSrcNew,
		newSrcData:        newSrcData,
		dstAccount:        dstAccountAddress,
		isDstNew:          isDstNew,
		newDstData:        newDstData,
		srcAccountSlot:    srcAccountSlot,
		dstAccountSlot:    dstAccountSlot,
		erc20Contract:     erc20Contract,
		newSrcBalance:     cryptosim.rand.Int64(),
		newDstBalance:     cryptosim.rand.Int64(),
		newFeeBalance:     cryptosim.rand.Int64(),
		newSrcAccountSlot: cryptosim.rand.Bytes(cryptosim.config.Erc20StorageSlotSize),
		newDstAccountSlot: cryptosim.rand.Bytes(cryptosim.config.Erc20StorageSlotSize),
	}, nil
}

// Execute the transaction.
//
// This method is thread safe with other calls to Execute(),
// but must not be called concurrently with CryptoSim.finalizeBlock().
func (txn *transaction) Execute(cryptosim *CryptoSim) error {

	// Read the simulated ERC20 contract.
	_, found, err := cryptosim.get(txn.erc20Contract)
	if err != nil {
		return fmt.Errorf("failed to get ERC20 contract: %w", err)
	}
	if !found {
		return fmt.Errorf("ERC20 contract not found")
	}

	// Read the following:
	// - the sender's native balance / nonce / codehash
	// - the receiver's native balance
	// - the sender's storage slot for the ERC20 contract
	// - the receiver's storage slot for the ERC20 contract
	// - the fee collection account's native balance

	// Read the sender's native balance / nonce / codehash.
	srcAccountValue, found, err := cryptosim.get(txn.srcAccount)
	if err != nil {
		return fmt.Errorf("failed to get source account: %w", err)
	}

	if txn.isSrcNew {
		// This is a new account, so we should not find it in the DB.
		if found {
			return fmt.Errorf("should not find source account in DB, account should be new")
		}
		srcAccountValue = txn.newSrcData
	} else {
		// This is an existing account, so we should find it in the DB.
		if !found {
			return fmt.Errorf("source account not found")
		}
	}

	// Read the receiver's native balance.
	dstAccountValue, found, err := cryptosim.get(txn.dstAccount)
	if err != nil {
		return fmt.Errorf("failed to get destination account: %w", err)
	}
	if txn.isDstNew {
		// This is a new account, so we should not find it in the DB.
		if found {
			return fmt.Errorf("should not find destination account in DB, account should be new")
		}
		dstAccountValue = txn.newDstData
	} else {
		// This is an existing account, so we should find it in the DB.
		if !found {
			return fmt.Errorf("destination account not found")
		}
	}

	// Read the sender's storage slot for the ERC20 contract.
	// We don't care if the value isn't in the DB yet, since we don't pre-populate the database with storage slots.
	_, _, err = cryptosim.get(txn.srcAccountSlot)
	if err != nil {
		return fmt.Errorf("failed to get source account slot: %w", err)
	}

	// Read the receiver's storage slot for the ERC20 contract.
	// We don't care if the value isn't in the DB yet, since we don't pre-populate the database with storage slots.
	_, _, err = cryptosim.get(txn.dstAccountSlot)
	if err != nil {
		return fmt.Errorf("failed to get destination account slot: %w", err)
	}

	// Read the fee collection account's native balance.
	feeValue, found, err := cryptosim.get(cryptosim.feeCollectionAddress)
	if err != nil {
		return fmt.Errorf("failed to get fee collection account: %w", err)
	}
	if !found {
		return fmt.Errorf("fee collection account not found")
	}

	// Apply the random values from the transaction to the account and slot data.
	binary.BigEndian.PutUint64(srcAccountValue[:8], uint64(txn.newSrcBalance))
	binary.BigEndian.PutUint64(dstAccountValue[:8], uint64(txn.newDstBalance))
	binary.BigEndian.PutUint64(feeValue[:8], uint64(txn.newFeeBalance))

	// Write the following:
	// - the sender's native balance / nonce / codehash
	// - the receiver's native balance
	// - the sender's storage slot for the ERC20 contract
	// - the receiver's storage slot for the ERC20 contract
	// - the fee collection account's native balance

	// Write the sender's account data.
	err = cryptosim.put(txn.srcAccount, srcAccountValue)
	if err != nil {
		return fmt.Errorf("failed to put source account: %w", err)
	}

	// Write the receiver's account data.
	err = cryptosim.put(txn.dstAccount, dstAccountValue)
	if err != nil {
		return fmt.Errorf("failed to put destination account: %w", err)
	}

	// Write the sender's storage slot for the ERC20 contract.
	err = cryptosim.put(txn.srcAccountSlot, txn.newSrcAccountSlot)
	if err != nil {
		return fmt.Errorf("failed to put source account slot: %w", err)
	}

	// Write the receiver's storage slot for the ERC20 contract.
	err = cryptosim.put(txn.dstAccountSlot, txn.newDstAccountSlot)
	if err != nil {
		return fmt.Errorf("failed to put destination account slot: %w", err)
	}

	// Write the fee collection account's native balance.
	err = cryptosim.put(cryptosim.feeCollectionAddress, feeValue)
	if err != nil {
		return fmt.Errorf("failed to put fee collection account: %w", err)
	}

	return nil
}
