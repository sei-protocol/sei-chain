package evmonly

import (
	"go.opentelemetry.io/otel"

	seidbmetrics "github.com/sei-protocol/sei-chain/sei-db/common/metrics"
)

// executorMeterName is the OTel meter this package's instruments are created on.
const executorMeterName = "evmonly_executor"

// Stages of a store-backed block, published as evmonly_block_phase_duration_seconds_total.
const (
	// phaseWaitingForBlock covers time the caller's loop is blocked with no block to run.
	phaseWaitingForBlock = "waiting_for_block"
	// phaseOpenView covers opening the block's state view.
	phaseOpenView = "open_view"
	// phaseExecute covers sequential execution, or OCC setup before speculation starts.
	phaseExecute = "execute"
	// phaseOCCSpeculate covers speculative execution across the OCC workers.
	phaseOCCSpeculate = "occ_speculate"
	// phaseOCCValidate covers validating speculative results and re-running conflicts.
	phaseOCCValidate = "occ_validate"
	// phaseOCCMerge covers merging OCC results into the block's result.
	phaseOCCMerge = "occ_merge"
	// phaseAwaitCommit covers waiting for the previous block's state commit to land.
	phaseAwaitCommit = "await_commit"
	// phaseEncodeChangesets covers encoding the block's state changes for the store.
	phaseEncodeChangesets = "encode_changesets"
	// phaseEncodeReceipts covers encoding the block's receipts.
	phaseEncodeReceipts = "encode_receipts"
	// phaseWriteReceipts covers writing the block's receipts to the receipt store.
	phaseWriteReceipts = "write_receipts"
	// phaseCommitState covers starting the block's state commit.
	phaseCommitState = "commit_state"
)

// newBlockPhases returns the executor's block phase timer. It is not safe for concurrent use.
func newBlockPhases() *seidbmetrics.PhaseTimer {
	return seidbmetrics.NewPhaseTimer(otel.Meter(executorMeterName), "evmonly_block")
}
