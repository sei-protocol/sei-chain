package operations

import (
	"bytes"
	"io"
	"math/big"
	"testing"

	"github.com/cockroachdb/pebble/v2"
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-db/config"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/pebbledb/mvcc"
	evmtypes "github.com/sei-protocol/sei-chain/x/evm/types"
)

// testReceipt is a receipt a test writes at version, under the transaction
// hash txHash(hash).
type testReceipt struct {
	hash    int64
	version int64
	receipt evmtypes.Receipt
}

// receiptOf returns a receipt for block, stored at the block, at txIndex.
func receiptOf(hash, block int64, txIndex uint32) testReceipt {
	return testReceipt{hash: hash, version: block, receipt: evmtypes.Receipt{
		BlockNumber: uint64(block), TransactionIndex: txIndex, GasUsed: 21000, //nolint:gosec // test blocks are positive
	}}
}

// oneReceiptPerBlock returns a receipt for every block from first to last,
// each under a hash equal to its block.
func oneReceiptPerBlock(first, last int64) []testReceipt {
	receipts := make([]testReceipt, 0, last-first+1)
	for block := first; block <= last; block++ {
		receipts = append(receipts, receiptOf(block, block, 0))
	}
	return receipts
}

func txHash(hash int64) common.Hash {
	return common.BigToHash(big.NewInt(hash))
}

// writeReceiptStore returns the directory of a new receipt store holding
// receipts, written in order, in the legacy ascending encoding when legacy is
// set.
func writeReceiptStore(t *testing.T, legacy bool, receipts ...testReceipt) string {
	t.Helper()
	dir := t.TempDir()
	if legacy {
		// A latest version without the descending marker is how OpenDB
		// recognises a legacy ascending DB.
		raw, err := pebble.Open(dir, &pebble.Options{Comparer: mvcc.MVCCComparer})
		require.NoError(t, err)
		require.NoError(t, raw.Set([]byte("s/_latest"), make([]byte, mvcc.VersionSize), pebble.Sync))
		require.NoError(t, raw.Close())
	}
	store, err := mvcc.OpenDB(dir, config.DefaultStateStoreConfig())
	require.NoError(t, err)
	for _, r := range receipts {
		value, err := r.receipt.Marshal()
		require.NoError(t, err)
		putReceiptStoreEntry(t, store, r.version, evmtypes.ReceiptKey(txHash(r.hash)), value)
	}
	require.NoError(t, store.Close())
	return dir
}

func runCompareReceiptStores(t *testing.T, args ...string) (string, error) {
	t.Helper()
	cmd := CompareReceiptStoresCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return out.String(), err
}

// Stores in different encodings that hold the same receipts compare equal.
func TestCompareReceiptStoresFindsNoDifference(t *testing.T) {
	dirA := writeReceiptStore(t, true, oneReceiptPerBlock(10, 20)...)
	dirB := writeReceiptStore(t, false, oneReceiptPerBlock(10, 20)...)

	out, err := runCompareReceiptStores(t, "--dir-a", dirA, "--dir-b", dirB)
	require.NoError(t, err)
	require.Contains(t, out, "ascending encoding, versions 0-20")
	require.Contains(t, out, "descending encoding, versions 0-20")
	require.Contains(t, out, "receipts: 11 the same in both, 0 only in A, 0 only in B, 0 that differ\n")
	require.Contains(t, out, "  10-20: same receipts (11 blocks)\n")
	require.Contains(t, out, "the stores hold the same receipts for every block compared\n")
}

// Blocks in one state are listed as runs, and --from and --to limit the
// blocks compared.
func TestCompareReceiptStoresListsRunsOfBlocks(t *testing.T) {
	dirA := writeReceiptStore(t, false, oneReceiptPerBlock(10, 20)...)
	dirB := writeReceiptStore(t, false, oneReceiptPerBlock(15, 30)...)

	out, err := runCompareReceiptStores(t, "--dir-a", dirA, "--dir-b", dirB)
	require.ErrorContains(t, err, "they differ for 15 blocks")
	require.Contains(t, out, "receipts: 6 the same in both, 5 only in A, 10 only in B, 0 that differ\n")
	require.Contains(t, out, "runs of blocks in one state, not broken by blocks neither store holds receipts for: 3\n"+
		"  10-14: receipts only in A (5 blocks)\n"+
		"  15-20: same receipts (6 blocks)\n"+
		"  21-30: receipts only in B (10 blocks)\n")

	out, err = runCompareReceiptStores(t, "--dir-a", dirA, "--dir-b", dirB, "--runs", "1")
	require.Error(t, err)
	require.Contains(t, out, "  10-14: receipts only in A (5 blocks)\n  and 2 more; narrow --from and --to to list them\n")

	out, err = runCompareReceiptStores(t, "--dir-a", dirA, "--dir-b", dirB, "--from", "15", "--to", "20")
	require.NoError(t, err)
	require.Contains(t, out, "blocks compared: 15 to 20\n")
	require.Contains(t, out, "receipts: 6 the same in both, 0 only in A, 0 only in B, 0 that differ\n")
}

// A receipt missing from one store and a receipt the stores hold in two forms
// each mark their block, and --block lists that block's receipts with the
// fields that differ.
func TestCompareReceiptStoresDescribesDifferingBlocks(t *testing.T) {
	changed := receiptOf(3, 16, 1)
	changed.receipt.GasUsed = 42000
	dirA := writeReceiptStore(t, false,
		receiptOf(1, 15, 0), receiptOf(2, 16, 0), receiptOf(3, 16, 1), receiptOf(4, 17, 0), receiptOf(5, 17, 1),
		receiptOf(6, 18, 0))
	dirB := writeReceiptStore(t, false,
		receiptOf(1, 15, 0), receiptOf(2, 16, 0), changed, receiptOf(4, 17, 0), receiptOf(6, 18, 0))
	hash := func(h int64) string { return txHash(h).Hex() }

	out, err := runCompareReceiptStores(t, "--dir-a", dirA, "--dir-b", dirB, "--block", "16")
	require.ErrorContains(t, err, "they differ for 2 blocks")
	require.Contains(t, out, "receipts: 4 the same in both, 1 only in A, 0 only in B, 1 that differ\n")
	require.Contains(t, out, "  15-15: same receipts (1 block)\n"+
		"  16-16: receipts that differ (1 block)\n"+
		"  17-17: some receipts missing from one store (1 block)\n"+
		"  18-18: same receipts (1 block)\n")
	require.Contains(t, out, "differing receipts, first 2:\n"+
		"  block 16 tx 1 "+hash(3)+": differs: gas_used 21000 vs 42000\n"+
		"  block 17 tx 1 "+hash(5)+": only in A\n")
	require.Contains(t, out, "receipts of block 16:\n"+
		"  tx 0 "+hash(2)+": same\n"+
		"  tx 1 "+hash(3)+": differs: gas_used 21000 vs 42000\n")

	out, err = runCompareReceiptStores(t, "--dir-a", dirA, "--dir-b", dirB, "--block", "17", "--examples", "0")
	require.Error(t, err)
	require.NotContains(t, out, "differing receipts")
	require.Contains(t, out, "receipts of block 17:\n  tx 0 "+hash(4)+": same\n  tx 1 "+hash(5)+": only in A\n")
}

// A receipt both stores hold alike counts as the same wherever each keeps it,
// as a legacy receipt moved out of x/evm state sits at a later version.
func TestCompareReceiptStoresAcceptsAReceiptAtAnotherVersion(t *testing.T) {
	moved := receiptOf(1, 10, 0)
	moved.version = 12
	dirA := writeReceiptStore(t, false, receiptOf(1, 10, 0), receiptOf(2, 11, 0))
	dirB := writeReceiptStore(t, false, receiptOf(2, 11, 0), moved)

	out, err := runCompareReceiptStores(t, "--dir-a", dirA, "--dir-b", dirB)
	require.NoError(t, err)
	require.Contains(t, out, "receipts: 2 the same in both")
	require.Contains(t, out, "receipts both stores hold alike at different versions: 1\n")
}

func TestCompareReceiptStoresFlagsUndecodableEntries(t *testing.T) {
	dirA := t.TempDir()
	store, err := mvcc.OpenDB(dirA, config.DefaultStateStoreConfig())
	require.NoError(t, err)
	putReceiptStoreEntry(t, store, 10, evmtypes.ReceiptKey(txHash(1)), []byte{0xff})
	require.NoError(t, store.Close())
	dirB := writeReceiptStore(t, false, receiptOf(1, 10, 0))

	_, err = runCompareReceiptStores(t, "--dir-a", dirA, "--dir-b", dirB)
	require.ErrorContains(t, err, "1 entries in A do not decode as receipts with a block number")
}

func TestCompareReceiptStoresRefusesOneStoreAsBoth(t *testing.T) {
	dir := writeReceiptStore(t, false, receiptOf(1, 10, 0))

	_, err := runCompareReceiptStores(t, "--dir-a", dir, "--dir-b", dir)
	require.ErrorContains(t, err, "same directory")
}

// Every field of x/evm's Receipt is named, with both values where they are
// short.
func TestDescribeReceiptDiffNamesEveryEVMField(t *testing.T) {
	base := evmtypes.Receipt{
		TxType: 2, CumulativeGasUsed: 100, ContractAddress: "0xc", TxHashHex: "0xh", GasUsed: 21000,
		EffectiveGasPrice: 7, BlockNumber: 9, TransactionIndex: 1, Status: 1, From: "0xf", To: "0xt",
		VmError: "boom", Logs: []*evmtypes.Log{{Address: "0xa"}}, LogsBloom: []byte{1}, PreExecutionFailure: true,
	}
	for _, tc := range []struct {
		want   string
		change func(*evmtypes.Receipt)
	}{
		{`tx_type 2 vs 3`, func(r *evmtypes.Receipt) { r.TxType = 3 }},
		{`cumulative_gas_used 100 vs 200`, func(r *evmtypes.Receipt) { r.CumulativeGasUsed = 200 }},
		{`contract_address "0xc" vs "0xd"`, func(r *evmtypes.Receipt) { r.ContractAddress = "0xd" }},
		{`tx_hash_hex "0xh" vs "0xi"`, func(r *evmtypes.Receipt) { r.TxHashHex = "0xi" }},
		{`gas_used 21000 vs 42000`, func(r *evmtypes.Receipt) { r.GasUsed = 42000 }},
		{`effective_gas_price 7 vs 8`, func(r *evmtypes.Receipt) { r.EffectiveGasPrice = 8 }},
		{`block_number 9 vs 10`, func(r *evmtypes.Receipt) { r.BlockNumber = 10 }},
		{`transaction_index 1 vs 2`, func(r *evmtypes.Receipt) { r.TransactionIndex = 2 }},
		{`status 1 vs 0`, func(r *evmtypes.Receipt) { r.Status = 0 }},
		{`from "0xf" vs "0xg"`, func(r *evmtypes.Receipt) { r.From = "0xg" }},
		{`to "0xt" vs "0xu"`, func(r *evmtypes.Receipt) { r.To = "0xu" }},
		{`vm_error "boom" vs "bang"`, func(r *evmtypes.Receipt) { r.VmError = "bang" }},
		{`logs (1 vs 2)`, func(r *evmtypes.Receipt) { r.Logs = append(r.Logs, &evmtypes.Log{Address: "0xb"}) }},
		{`logs`, func(r *evmtypes.Receipt) { r.Logs = []*evmtypes.Log{{Address: "0xb"}} }},
		{`logsBloom (1 vs 2 bytes)`, func(r *evmtypes.Receipt) { r.LogsBloom = []byte{1, 2} }},
		{`pre_execution_failure 1 vs 0`, func(r *evmtypes.Receipt) { r.PreExecutionFailure = false }},
	} {
		t.Run(tc.want, func(t *testing.T) {
			changed := base
			changed.Logs = append([]*evmtypes.Log(nil), base.Logs...)
			tc.change(&changed)
			a, err := base.Marshal()
			require.NoError(t, err)
			b, err := changed.Marshal()
			require.NoError(t, err)
			require.Equal(t, "differs: "+tc.want, describeReceiptDiff(a, b))
		})
	}
}
