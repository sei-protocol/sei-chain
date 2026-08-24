package cryptosim

import (
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-db/common/metrics"
)

// The data needed to execute a transaction.
type transaction struct {
	// The simulated ERC20 contract that will be interacted with. This value is read.
	erc20Contract []byte

	// The source account that will be interacted with. This value is read and written.
	srcAccount []byte
	// If true, the source account is new and needs to be created.
	isSrcNew bool

	// The destination account that will be interacted with. This value is read and written.
	dstAccount []byte
	// If true, the destination account is new and needs to be created.
	isDstNew bool

	// The source account's storage slot that will be interacted with. This value is read and written.
	srcAccountSlot []byte
	// The destination account's storage slot that will be interacted with. This value is read and written.
	dstAccountSlot []byte

	// Pre-generated random value for the source account's new native balance.
	newSrcBalance []byte
	// Pre-generated random value for the destination account's new native balance.
	newDstBalance []byte
	// Pre-generated random value for the fee collection account's new native balance.
	newFeeBalance []byte
	// Pre-generated random value for the source account's ERC20 storage slot.
	newSrcAccountSlot []byte
	// Pre-generated random value for the destination account's ERC20 storage slot.
	newDstAccountSlot []byte

	// If true, capture detailed (and potentially expensive) metrics about this transaction.
	// We may only sample a small percentage of transactions with this flag set to true.
	captureMetrics bool
}

// Generate all data needed to execute a transaction.
//
// This method is not thread safe to call concurrently with other calls to BuildTransaction().
func BuildTransaction(
	dataGenerator *DataGenerator,
) (*transaction, error) {

	srcAccountID, srcAccountAddress, isSrcNew, err := dataGenerator.RandomAccount()
	if err != nil {
		return nil, fmt.Errorf("failed to select source account: %w", err)
	}
	dstAccountID, dstAccountAddress, isDstNew, err := dataGenerator.RandomAccount()
	if err != nil {
		return nil, fmt.Errorf("failed to select destination account: %w", err)
	}

	srcAccountSlot, err := dataGenerator.randomAccountSlot(srcAccountID)
	if err != nil {
		return nil, fmt.Errorf("failed to select source account slot: %w", err)
	}
	dstAccountSlot, err := dataGenerator.randomAccountSlot(dstAccountID)
	if err != nil {
		return nil, fmt.Errorf("failed to select destination account slot: %w", err)
	}
	erc20Contract, err := dataGenerator.randomErc20Contract()
	if err != nil {
		return nil, fmt.Errorf("failed to select ERC20 contract: %w", err)
	}

	captureMetrics := dataGenerator.rand.Float64() < dataGenerator.config.TransactionMetricsSampleRate

	return &transaction{
		srcAccount:     srcAccountAddress,
		isSrcNew:       isSrcNew,
		dstAccount:     dstAccountAddress,
		isDstNew:       isDstNew,
		srcAccountSlot: srcAccountSlot,
		dstAccountSlot: dstAccountSlot,
		erc20Contract:  erc20Contract,
		// Windows onto the canned buffer rather than copies of it. The buffer is never written after
		// construction, and every consumer of a value copies it — Put keeps the slice only until the
		// WAL write, and flatkv's value types copy into storage of their own. A copy here would be a
		// copy of bytes nothing can change, five times per transaction.
		newSrcBalance:     dataGenerator.rand.Bytes(dataGenerator.config.AccountBalanceSize),
		newDstBalance:     dataGenerator.rand.Bytes(dataGenerator.config.AccountBalanceSize),
		newFeeBalance:     dataGenerator.rand.Bytes(dataGenerator.config.AccountBalanceSize),
		newSrcAccountSlot: dataGenerator.rand.Bytes(dataGenerator.config.Erc20StorageSlotSize),
		newDstAccountSlot: dataGenerator.rand.Bytes(dataGenerator.config.Erc20StorageSlotSize),
		captureMetrics:    captureMetrics,
	}, nil
}

// Execute the transaction.
//
// This method is thread safe with other calls to Execute(),
// but must not be called concurrently with CryptoSim.finalizeBlock().
func (txn *transaction) Execute(
	database *Database,
	feeCollectionAddress []byte,
	phaseTimer *metrics.PhaseTimer,
) error {
	if !database.config.DisableTransactionReads {
		phaseTimer.SetPhase("read_erc20")

		// Read the simulated ERC20 contract.
		if _, _, err := database.Get(txn.erc20Contract); err != nil {
			return fmt.Errorf("failed to get ERC20 contract: %w", err)
		}

		// Read the following:
		// - the sender's native balance / nonce / codehash
		// - the receiver's native balance
		// - the sender's storage slot for the ERC20 contract
		// - the receiver's storage slot for the ERC20 contract
		// - the fee collection account's native balance

		phaseTimer.SetPhase("read_src_account")

		// Read the sender's native balance / nonce / codehash.
		// Technically, we are just requesting to read the codehash, but internally the codehash is bundled with
		// the nonce and balance, so all of this data will be read from low level storage, even if it isn't being
		// returned to the caller.
		if _, _, err := database.Get(txn.srcAccount); err != nil {
			return fmt.Errorf("failed to get source account: %w", err)
		}

		phaseTimer.SetPhase("read_dst_account")

		// Read the receiver's native balance / nonce / codehash.
		if _, _, err := database.Get(txn.dstAccount); err != nil {
			return fmt.Errorf("failed to get destination account: %w", err)
		}

		phaseTimer.SetPhase("read_src_account_slot")

		// Read the sender's storage slot for the ERC20 contract.
		// We don't care if the value isn't in the DB yet, since we don't pre-populate the database with storage slots.
		if _, _, err := database.Get(txn.srcAccountSlot); err != nil {
			return fmt.Errorf("failed to get source account slot: %w", err)
		}

		phaseTimer.SetPhase("read_dst_account_slot")

		// Read the receiver's storage slot for the ERC20 contract.
		// We don't care if the value isn't in the DB yet, since we don't pre-populate the database with storage slots.
		if _, _, err := database.Get(txn.dstAccountSlot); err != nil {
			return fmt.Errorf("failed to get destination account slot: %w", err)
		}

		phaseTimer.SetPhase("read_fee_collection_account")

		// Read the fee collection account's native balance.
		if _, _, err := database.Get(feeCollectionAddress); err != nil {
			return fmt.Errorf("failed to get fee collection account: %w", err)
		}
	}

	// The writes this transaction makes — both accounts' balances and both ERC20 storage slots, plus
	// the block's single fee collection write — were recorded when the block was generated, so there is
	// nothing to write here. See blockBuilder.writeTransaction: the values are pre-generated and depend
	// on nothing that was just read, so issuing them on this thread only took time away from the reads,
	// which are what this benchmark exists to measure.
	phaseTimer.Reset()

	return nil
}

// Returns true if metrics should be captured for this transaction.
func (txn *transaction) ShouldCaptureMetrics() bool {
	return txn.captureMetrics
}
