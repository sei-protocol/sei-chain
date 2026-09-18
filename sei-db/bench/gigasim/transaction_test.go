package gigasim

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// readsPerTransaction is what a transfer reads: the contract code, both accounts, both storage slots,
// and the fee account.
const readsPerTransaction = 6

// TestExecuteOnlyReads pins where a block's writes come from. They are staged when the block is
// generated, so execution is reads alone — issuing them here instead took time away from the reads,
// which are what is under measurement.
func TestExecuteOnlyReads(t *testing.T) {
	t.Parallel()

	state, view := newTestState()
	txn := &transaction{
		erc20Contract:     []byte("erc20"),
		srcAccount:        testAccountKey(1),
		dstAccount:        testAccountKey(2),
		srcAccountSlot:    testSlotKey(1),
		dstAccountSlot:    testSlotKey(2),
		newSrcBalance:     []byte("src balance"),
		newDstBalance:     []byte("dst balance"),
		newFeeBalance:     []byte("fee balance"),
		newSrcAccountSlot: []byte("src slot value"),
		newDstAccountSlot: []byte("dst slot value"),
	}

	txn.execute(state, testAccountKey(0), nil)

	require.Equal(t, readsPerTransaction, view.reads)
	require.Zero(t, state.setupWrites.count(), "execution must stage no writes")
}
