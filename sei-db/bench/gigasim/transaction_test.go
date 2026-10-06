package gigasim

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// readsPerTransaction is what a transfer reads: the contract code, the sender's account, both balance
// slots, and the fee account.
const readsPerTransaction = 5

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
		newFeeBalance:     []byte("fee balance"),
		newSrcAccountSlot: []byte("src slot value"),
		newDstAccountSlot: []byte("dst slot value"),
	}

	txn.execute(state, testAccountKey(0), nil)

	require.Equal(t, readsPerTransaction, view.reads)
	require.Zero(t, state.setupWrites.count(), "execution must stage no writes")
}

// TestNativeTransferReadsOnlyAccounts pins what a native transfer reads: the sender's and recipient's
// accounts and the fee account, and no contract code or storage.
func TestNativeTransferReadsOnlyAccounts(t *testing.T) {
	t.Parallel()

	state, view := newTestState()
	txn := &transaction{
		kind:          nativeTransfer,
		srcAccount:    testAccountKey(1),
		dstAccount:    testAccountKey(2),
		newSrcBalance: []byte("src balance"),
		newDstBalance: []byte("dst balance"),
		newFeeBalance: []byte("fee balance"),
	}

	txn.execute(state, testAccountKey(0), nil)

	require.Equal(t, 3, view.reads, "a native transfer reads both accounts and the fee account")
	require.Zero(t, state.setupWrites.count(), "execution must stage no writes")
}
