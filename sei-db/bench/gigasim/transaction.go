package gigasim

import (
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-db/common/metrics"
)

// transaction is one simulated ERC20 transfer: the keys it touches and the values it writes, all
// resolved up front so that executing it is nothing but the reads and writes a real transfer would
// make against state.
type transaction struct {
	// The ERC20 contract's code, which is read.
	erc20Contract []byte

	// The sending account, which is read and written.
	srcAccount []byte

	// The receiving account, which is read and written.
	dstAccount []byte

	// The sender's storage slot for the ERC20 contract, which is read and written.
	srcAccountSlot []byte

	// The receiver's storage slot for the ERC20 contract, which is read and written.
	dstAccountSlot []byte

	newSrcBalance     []byte
	newDstBalance     []byte
	newFeeBalance     []byte
	newSrcAccountSlot []byte
	newDstAccountSlot []byte

	// If true, record per-phase timings while executing. Only a sampled fraction of transactions do,
	// since the instrumentation costs more than the work it measures.
	captureMetrics bool
}

// buildTransaction resolves every key and value one transfer needs into txn, which the caller owns. The
// values alias the canned random buffer rather than copying out of it, and stay valid for the life of
// the run because the buffer is written once at startup and only read afterwards.
//
// Not thread safe: it draws from the account model, which belongs to a single goroutine.
func buildTransaction(txn *transaction, accounts *accountModel) error {
	srcAccount, _, err := accounts.RandomAccount()
	if err != nil {
		return fmt.Errorf("failed to select the source account: %w", err)
	}
	dstAccount, _, err := accounts.RandomAccount()
	if err != nil {
		return fmt.Errorf("failed to select the destination account: %w", err)
	}
	erc20Contract, err := accounts.RandomErc20Contract()
	if err != nil {
		return fmt.Errorf("failed to select the ERC20 contract: %w", err)
	}

	rand := accounts.Rand()
	*txn = transaction{
		erc20Contract:     erc20Contract,
		srcAccount:        srcAccount,
		dstAccount:        dstAccount,
		srcAccountSlot:    accounts.RandomAccountSlot(),
		dstAccountSlot:    accounts.RandomAccountSlot(),
		newSrcBalance:     rand.Bytes(accountRecordLen),
		newDstBalance:     rand.Bytes(accountRecordLen),
		newFeeBalance:     rand.Bytes(accountRecordLen),
		newSrcAccountSlot: rand.Bytes(storageSlotValueLen),
		newDstAccountSlot: rand.Bytes(storageSlotValueLen),
		captureMetrics:    rand.Float64() < accounts.config.TransactionMetricsSampleRate,
	}
	return nil
}

// execute replays the reads and writes an ERC20 transfer makes against state. The transfer arithmetic
// is not performed: what is under measurement is the storage traffic, not the EVM.
//
// Safe to call concurrently with other executions, but not with executionState.commitBlock.
func (txn *transaction) execute(state *executionState, feeAccount []byte, phaseTimer *metrics.PhaseTimer) {
	// A transfer reads the contract code, both accounts' balance/nonce/codehash records, both storage
	// slots holding the ERC20 balances, and the fee account. Every result is discarded: the read is
	// the work being measured, and a slot absent from state is a normal outcome because slots are
	// never prepopulated.
	phaseTimer.SetPhase("read_erc20")
	state.Get(txn.erc20Contract)

	phaseTimer.SetPhase("read_src_account")
	state.Get(txn.srcAccount)

	phaseTimer.SetPhase("read_dst_account")
	state.Get(txn.dstAccount)

	phaseTimer.SetPhase("read_src_account_slot")
	state.Get(txn.srcAccountSlot)

	phaseTimer.SetPhase("read_dst_account_slot")
	state.Get(txn.dstAccountSlot)

	phaseTimer.SetPhase("read_fee_account")
	state.Get(feeAccount)

	phaseTimer.SetPhase("update_balances")
	state.Put(txn.srcAccount, txn.newSrcBalance)
	state.Put(txn.dstAccount, txn.newDstBalance)
	state.Put(txn.srcAccountSlot, txn.newSrcAccountSlot)
	state.Put(txn.dstAccountSlot, txn.newDstAccountSlot)
	state.Put(feeAccount, txn.newFeeBalance)

	phaseTimer.Reset()
}
