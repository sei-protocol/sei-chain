package evmonly

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
)

func TestTxRejectionReasonMapsGethErrors(t *testing.T) {
	for _, tc := range []struct {
		err  error
		want string
	}{
		{core.ErrGasLimitReached, txRejectionGasLimitReached},
		{fmt.Errorf("%w: address 0x1, tx: 5 state: 3", core.ErrNonceTooHigh), txRejectionNonceTooHigh},
		{fmt.Errorf("%w: address 0x1, tx: 2 state: 3", core.ErrNonceTooLow), txRejectionNonceTooLow},
		{fmt.Errorf("%w: address 0x1", core.ErrInsufficientFunds), txRejectionInsufficientFunds},
		{fmt.Errorf("%w: address 0x1", core.ErrInsufficientFundsForTransfer), txRejectionInsufficientFunds},
		{core.ErrIntrinsicGas, txRejectionOther},
		{errors.New("unexpected"), txRejectionOther},
	} {
		require.Equal(t, tc.want, txRejectionReason(tc.err), "error %v", tc.err)
	}
}

func TestRecordTxExecutionStatsCountsRejectionReasons(t *testing.T) {
	reader := bindTestExecutionMetrics(t)
	recordTxExecutionStats(t.Context(), []TxResult{
		{Status: types.ReceiptStatusSuccessful},
		{Status: types.ReceiptStatusFailed, Rejected: true, Err: core.ErrGasLimitReached},
		{Status: types.ReceiptStatusFailed, Rejected: true, Err: core.ErrGasLimitReached},
		{Status: types.ReceiptStatusFailed, Rejected: true, Err: core.ErrNonceTooHigh},
	})

	collected := collectOCCMetrics(t, reader)
	want := map[string]int64{txRejectionGasLimitReached: 2, txRejectionNonceTooHigh: 1}
	// Every reason is reported, so a query can tell zero rejections from an absent series.
	for _, reason := range txRejectionReasons {
		require.Equal(t, want[reason], requireCounter(t, collected, "giga_evmonly_txs_rejected_total",
			attribute.String("reason", reason)), "reason %s", reason)
	}
}

// captureRejectedTxLogs sends the rejected transaction log to a buffer at level for the rest of
// the test, and returns a function that decodes the lines written so far.
func captureRejectedTxLogs(t *testing.T, level slog.Level) func() []map[string]any {
	t.Helper()
	var buf bytes.Buffer
	previous := rejectedTxLogger
	t.Cleanup(func() { rejectedTxLogger = previous })
	rejectedTxLogger = slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: level}))
	return func() []map[string]any {
		var lines []map[string]any
		for raw := range bytes.SplitSeq(bytes.TrimSpace(buf.Bytes()), []byte("\n")) {
			if len(raw) == 0 {
				continue
			}
			var line map[string]any
			require.NoError(t, json.Unmarshal(raw, &line))
			lines = append(lines, line)
		}
		return lines
	}
}

// rejectionTestBlock returns a block of three transactions in which the last two were rejected.
func rejectionTestBlock() (BlockContext, []PreparedTx, *BlockResult) {
	block := BlockContext{Number: 7, GasLimit: 100}
	var txs []PreparedTx
	var results []TxResult
	for nonce, err := range []error{nil, core.ErrGasLimitReached, core.ErrNonceTooHigh} {
		tx := types.NewTx(&types.LegacyTx{Nonce: uint64(nonce), Gas: 40})
		txs = append(txs, PreparedTx{Tx: tx, Sender: common.Address{1}})
		results = append(results, TxResult{Hash: tx.Hash(), Sender: common.Address{1}, Err: err, Rejected: err != nil})
	}
	return block, txs, &BlockResult{Txs: results, GasUsed: 30}
}

func TestLogRejectedTxsSummarizesTheBlock(t *testing.T) {
	logs := captureRejectedTxLogs(t, slog.LevelInfo)
	block, txs, result := rejectionTestBlock()
	logRejectedTxs(t.Context(), block, txs, result)

	lines := logs()
	require.Len(t, lines, 1)
	require.Equal(t, "block rejected transactions", lines[0]["msg"])
	require.InDelta(t, 7, lines[0]["height"], 0)
	require.InDelta(t, 3, lines[0]["txs"], 0)
	require.InDelta(t, 2, lines[0]["rejected"], 0)
	require.InDelta(t, 30, lines[0]["gas_used"], 0)
	require.InDelta(t, 100, lines[0]["gas_limit"], 0)
	require.InDelta(t, 1, lines[0][txRejectionGasLimitReached], 0)
	require.InDelta(t, 1, lines[0][txRejectionNonceTooHigh], 0)
	require.NotContains(t, lines[0], txRejectionNonceTooLow)
}

func TestLogRejectedTxsLogsEachRejectionAtDebug(t *testing.T) {
	logs := captureRejectedTxLogs(t, slog.LevelDebug)
	block, txs, result := rejectionTestBlock()
	logRejectedTxs(t.Context(), block, txs, result)

	lines := logs()
	require.Len(t, lines, 3)
	for i, want := range []struct {
		hash   common.Hash
		nonce  float64
		reason string
	}{
		{result.Txs[1].Hash, 1, txRejectionGasLimitReached},
		{result.Txs[2].Hash, 2, txRejectionNonceTooHigh},
	} {
		require.Equal(t, "block rejected transaction", lines[i]["msg"])
		require.Equal(t, want.hash.Hex(), lines[i]["hash"])
		require.InDelta(t, want.nonce, lines[i]["nonce"], 0)
		require.InDelta(t, 40, lines[i]["gas_limit"], 0)
		require.Equal(t, want.reason, lines[i]["reason"])
	}
	require.Equal(t, "block rejected transactions", lines[2]["msg"])
}

func TestLogRejectedTxsIsQuietWithoutRejections(t *testing.T) {
	logs := captureRejectedTxLogs(t, slog.LevelDebug)
	logRejectedTxs(t.Context(), BlockContext{Number: 7}, nil, &BlockResult{Txs: []TxResult{{}}})
	require.Empty(t, logs())
}
