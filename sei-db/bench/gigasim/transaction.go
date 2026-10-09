package gigasim

import (
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-db/common/metrics"
)

// transactionKind is what a simulated transaction does, which decides the state it reads and writes.
type transactionKind uint8

const (
	// erc20Transfer moves an ERC20 token balance. It reads the token's code, the sender's account and
	// both holders' balance slots in the token's storage, and writes the sender's account and both slots.
	// The recipient's account is untouched: the transfer names it only as an argument to the token.
	erc20Transfer transactionKind = iota

	// nativeTransfer moves the native balance. It reads and writes the sender's and recipient's accounts
	// and touches no contract code or storage.
	nativeTransfer
)

// transaction is one simulated transfer: the keys it touches and the values it writes, all resolved up
// front so that executing it is nothing but the reads a real transfer would make against state. Its
// writes are staged when the block is generated; see blockGenerator.buildBlock().
type transaction struct {
	kind transactionKind

	// The sending account, which is read and written: it pays for gas and its nonce advances.
	srcAccount []byte

	// The receiving account. A native transfer reads and writes it; an ERC20 transfer does neither, and
	// uses its address only to key the recipient's balance slot and the receipt's Transfer log.
	dstAccount []byte

	// The ERC20 contract's code, which is read. Nil for a native transfer.
	erc20Contract []byte

	// The sender's and the receiver's balance slots in the ERC20 contract's storage, which are read and
	// written. Nil for a native transfer.
	srcAccountSlot []byte
	dstAccountSlot []byte

	newSrcBalance []byte
	newFeeBalance []byte

	// The receiver's new native balance. Nil for an ERC20 transfer.
	newDstBalance []byte

	// The holders' new token balances. Nil for a native transfer.
	newSrcAccountSlot []byte
	newDstAccountSlot []byte

	// The transaction type, gas price, and token amount its receipt records. A native transfer's amount
	// is zero.
	drawn receiptDraw

	// If true, record per-phase timings while executing. Only a sampled fraction of transactions do,
	// since the instrumentation costs more than the work it measures.
	captureMetrics bool
}

// buildTransaction resolves every key and value one transfer of the configured kind needs into txn,
// which the caller owns. The values alias the canned random buffer rather than copying out of it, and
// stay valid for the life of the run because the buffer is written once at startup and only read
// afterwards.
//
// Not thread safe: it draws from the account model, which belongs to a single goroutine.
func buildTransaction(txn *transaction, accounts *accountModel) error {
	srcAccount, srcAccountID, err := accounts.RandomAccount()
	if err != nil {
		return fmt.Errorf("failed to select the source account: %w", err)
	}
	dstAccount, dstAccountID, err := accounts.RandomAccount()
	if err != nil {
		return fmt.Errorf("failed to select the destination account: %w", err)
	}

	rand := accounts.Rand()
	*txn = transaction{
		kind:          accounts.config.transactionKind(),
		srcAccount:    srcAccount,
		dstAccount:    dstAccount,
		newSrcBalance: rand.Bytes(accountRecordLen),
		newFeeBalance: rand.Bytes(accountRecordLen),
	}
	if txn.kind == nativeTransfer {
		txn.newDstBalance = rand.Bytes(accountRecordLen)
	} else {
		contract, err := accounts.HeldErc20Contract(srcAccountID)
		if err != nil {
			return fmt.Errorf("failed to select the ERC20 contract: %w", err)
		}
		txn.erc20Contract = contractCodeKey(contract)
		txn.srcAccountSlot = accounts.Erc20BalanceSlot(contract, srcAccountID)
		txn.dstAccountSlot = accounts.Erc20BalanceSlot(contract, dstAccountID)
		txn.newSrcAccountSlot = rand.Bytes(storageSlotValueLen)
		txn.newDstAccountSlot = rand.Bytes(storageSlotValueLen)
	}
	txn.captureMetrics = rand.Float64() < accounts.config.TransactionMetricsSampleRate
	return nil
}

// execute replays the reads a transfer makes against state. The transfer arithmetic is not performed:
// what is under measurement is the storage traffic, not the EVM.
//
// Every result is discarded: the read is the work being measured, and a key absent from state is a
// normal outcome, because balance slots are never prepopulated and minted accounts are not written
// until a block commits them.
//
// The writes a transfer makes were staged when the block was generated, so there is nothing to write
// here. Their values are drawn up front and depend on nothing that was just read, so issuing them on
// this thread only took time away from the reads, which are what is under measurement.
//
// Safe to call concurrently with other executions, but not with executionState.commitBlock.
func (txn *transaction) execute(state *executionState, feeAccount []byte, phaseTimer *metrics.PhaseTimer) {
	if txn.kind == nativeTransfer {
		txn.readNativeTransfer(state, phaseTimer)
	} else {
		txn.readErc20Transfer(state, phaseTimer)
	}
	phaseTimer.SetPhase("read_fee_account")
	state.Get(feeAccount)
	phaseTimer.Reset()
}

// readErc20Transfer reads what an ERC20 transfer reads before the fee account: the token's code, the
// sender's account and both balance slots in the token's storage.
func (txn *transaction) readErc20Transfer(state *executionState, phaseTimer *metrics.PhaseTimer) {
	phaseTimer.SetPhase("read_erc20")
	state.Get(txn.erc20Contract)

	phaseTimer.SetPhase("read_src_account")
	state.Get(txn.srcAccount)

	phaseTimer.SetPhase("read_src_account_slot")
	state.Get(txn.srcAccountSlot)

	phaseTimer.SetPhase("read_dst_account_slot")
	state.Get(txn.dstAccountSlot)
}

// readNativeTransfer reads what a native transfer reads before the fee account: both accounts.
func (txn *transaction) readNativeTransfer(state *executionState, phaseTimer *metrics.PhaseTimer) {
	phaseTimer.SetPhase("read_src_account")
	state.Get(txn.srcAccount)

	phaseTimer.SetPhase("read_dst_account")
	state.Get(txn.dstAccount)
}
