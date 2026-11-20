package littidx

import (
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"

	receipttypes "github.com/sei-protocol/sei-chain/sei-db/receipt_db/types"
)

// TestWriteReceiptsReportsOnlyTheRecordsItWrote covers a conditional record that yields to a stored
// receipt: it is left out of what writeReceipts reports, so the written-receipts metric does not count it.
func TestWriteReceiptsReportsOnlyTheRecordsItWrote(t *testing.T) {
	s := openPipelinedStore(t, t.TempDir())
	addr := common.HexToAddress("0xfc01")
	topic := common.HexToHash("0xfc02")

	storedHash, storedReceipt := littCtxTestReceipt(1, 0, addr, topic, 1)
	written, err := s.writeReceipts(1, []receipttypes.ReceiptRecord{{TxHash: storedHash, Receipt: storedReceipt}})
	require.NoError(t, err)
	require.Len(t, written, 1)

	_, staleReceipt := littCtxTestReceipt(2, 0, addr, topic, 1)
	freshHash, freshReceipt := littCtxTestReceipt(2, 1, addr, topic, 1)
	written, err = s.writeReceipts(2, []receipttypes.ReceiptRecord{
		{TxHash: storedHash, Receipt: staleReceipt, KeepExisting: true},
		{TxHash: freshHash, Receipt: freshReceipt},
	})
	require.NoError(t, err)
	require.Len(t, written, 1)
	require.Equal(t, freshHash, written[0].TxHash)
}
