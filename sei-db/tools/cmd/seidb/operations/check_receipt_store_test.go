package operations

import (
	"bytes"
	"io"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-db/config"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/pebbledb/mvcc"
	seidbtypes "github.com/sei-protocol/sei-chain/sei-db/db_engine/types"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	evmtypes "github.com/sei-protocol/sei-chain/x/evm/types"
)

// storedReceipt is a receipt for block, stored at version.
type storedReceipt struct {
	version, block int64
}

// atTheirBlocks returns one receipt for every block from first to last, each
// stored at its own block.
func atTheirBlocks(first, last int64) []storedReceipt {
	receipts := make([]storedReceipt, 0, last-first+1)
	for block := first; block <= last; block++ {
		receipts = append(receipts, storedReceipt{version: block, block: block})
	}
	return receipts
}

// newReceiptStore returns the directory of a new receipt store holding
// receipts, written in order, with its earliest version set to earliest
// unless that is 0.
func newReceiptStore(t *testing.T, earliest int64, receipts ...storedReceipt) string {
	t.Helper()
	dir := t.TempDir()
	store, err := mvcc.OpenDB(dir, config.DefaultStateStoreConfig())
	require.NoError(t, err)
	for i, r := range receipts {
		value, err := (&evmtypes.Receipt{BlockNumber: uint64(r.block), GasUsed: 21000}).Marshal() //nolint:gosec // test blocks are positive
		require.NoError(t, err)
		putReceiptStoreEntry(t, store, r.version, evmtypes.ReceiptKey(common.BigToHash(big.NewInt(int64(i+1)))), value)
	}
	if earliest > 0 {
		require.NoError(t, store.SetEarliestVersion(earliest, true))
	}
	require.NoError(t, store.Close())
	return dir
}

func putReceiptStoreEntry(t *testing.T, store seidbtypes.StateStore, version int64, key, value []byte) {
	t.Helper()
	require.NoError(t, store.ApplyChangesetSync(version, []*proto.NamedChangeSet{{
		Name:      receiptStoreKey,
		Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{{Key: key, Value: value}}},
	}}))
}

func runCheckReceiptStore(t *testing.T, args ...string) (string, error) {
	t.Helper()
	cmd := CheckReceiptStoreCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return out.String(), err
}

func TestReceiptKeyLayoutIsTheEVMModules(t *testing.T) {
	key := evmtypes.ReceiptKey(common.Hash{})
	require.Len(t, key, receiptKeyLen)
	require.Equal(t, evmtypes.ReceiptKeyPrefix, key[:1])
	require.Equal(t, byte(receiptKeyPrefix), key[0])
}

func TestReceiptBlockNumberReadsTheEVMField(t *testing.T) {
	value, err := (&evmtypes.Receipt{
		TxType:      2,
		GasUsed:     21000,
		TxHashHex:   "0xabc",
		BlockNumber: 199_551_506,
		Logs:        []*evmtypes.Log{{Address: "0xdef", Data: []byte{1, 2, 3}}},
	}).Marshal()
	require.NoError(t, err)
	block, err := receiptBlockNumber(value)
	require.NoError(t, err)
	require.Equal(t, uint64(199_551_506), block)

	value, err = (&evmtypes.Receipt{GasUsed: 21000}).Marshal()
	require.NoError(t, err)
	block, err = receiptBlockNumber(value)
	require.NoError(t, err)
	require.Zero(t, block)
}

func TestCheckReceiptStorePassesAContiguousStore(t *testing.T) {
	dir := newReceiptStore(t, 0, atTheirBlocks(10, 20)...)

	out, err := runCheckReceiptStore(t, "--dir", dir)
	require.NoError(t, err)
	require.Contains(t, out, "versions 0-20")
	require.Contains(t, out, "receipts: 11 for 11 blocks, blocks 10-20")
	require.Contains(t, out, "blocks 10-20 all hold receipts")
	require.Contains(t, out, "no problems found")
}

// A run of blocks without receipts is listed, and counts as a possible hole
// once it reaches the threshold.
func TestCheckReceiptStoreFindsAHole(t *testing.T) {
	dir := newReceiptStore(t, 0, append(atTheirBlocks(10, 20), atTheirBlocks(40, 50)...)...)

	out, err := runCheckReceiptStore(t, "--dir", dir)
	require.NoError(t, err, "a run shorter than the default threshold is no problem")
	require.Contains(t, out, "runs of blocks without receipts in 10-50: 1\n  21-39: 19 blocks\n")

	out, err = runCheckReceiptStore(t, "--dir", dir, "--hole-threshold", "19")
	require.ErrorContains(t, err, "1 runs of at least 19 blocks hold no receipts")
	require.Contains(t, out, "21-39: 19 blocks, a possible hole")

	out, err = runCheckReceiptStore(t, "--dir", dir, "--hole-threshold", "19", "--longest-runs", "0")
	require.ErrorContains(t, err, "1 runs of at least 19 blocks hold no receipts")
	require.Contains(t, out, "runs of blocks without receipts in 10-50: 1\nruns of at least 19 blocks")
}

// The blocks between an earliest marker and the first receipt are a run like
// any other, since a node serves them as blocks without receipts.
func TestCheckReceiptStoreSearchesFromTheEarliestMarker(t *testing.T) {
	dir := newReceiptStore(t, 5, atTheirBlocks(30, 40)...)

	out, err := runCheckReceiptStore(t, "--dir", dir, "--hole-threshold", "20")
	require.ErrorContains(t, err, "1 runs of at least 20 blocks hold no receipts")
	require.Contains(t, out, "runs of blocks without receipts in 5-40: 1\n  5-29: 25 blocks, a possible hole\n")
}

// Receipts stored above their own block, as v6.3 stored the legacy receipts it
// moved, count for their own blocks.
func TestCheckReceiptStoreCountsMovedLegacyReceipts(t *testing.T) {
	receipts := atTheirBlocks(21, 30)
	for block := int64(10); block <= 20; block++ {
		receipts = append(receipts, storedReceipt{version: 31, block: block})
	}
	dir := newReceiptStore(t, 0, receipts...)

	out, err := runCheckReceiptStore(t, "--dir", dir)
	require.NoError(t, err)
	require.Contains(t, out, "receipts: 21 for 21 blocks, blocks 10-30")
	require.Contains(t, out, "as v6.3 stored the legacy receipts it moved: 11")
}

func TestCheckReceiptStoreFlagsBrokenEntries(t *testing.T) {
	dir := t.TempDir()
	store, err := mvcc.OpenDB(dir, config.DefaultStateStoreConfig())
	require.NoError(t, err)
	value, err := (&evmtypes.Receipt{BlockNumber: 12}).Marshal()
	require.NoError(t, err)
	putReceiptStoreEntry(t, store, 10, evmtypes.ReceiptKey(common.HexToHash("0x1")), value)
	putReceiptStoreEntry(t, store, 11, evmtypes.ReceiptKey(common.HexToHash("0x2")), []byte{0xff})
	require.NoError(t, store.Close())

	_, err = runCheckReceiptStore(t, "--dir", dir)
	require.ErrorContains(t, err, "1 entries do not decode as receipts with a block number")
	require.ErrorContains(t, err, "1 receipts are stored below their own block")
}

func TestCheckReceiptStoreFlagsOtherStores(t *testing.T) {
	dir := newTestReceiptStore(t, "bank", 10, 0, "k", "v")

	_, err := runCheckReceiptStore(t, "--dir", dir)
	require.ErrorContains(t, err, `holds stores other than "receipt": ["bank"]`)
	require.ErrorContains(t, err, "holds no receipts")
}

func TestBlockSetEmptyRuns(t *testing.T) {
	var set blockSet
	for _, block := range []uint64{5, 64, 127, 128, 200, blockChunkBits + 3} {
		set.add(block)
	}
	set.add(64)
	require.Equal(t, int64(6), set.count)
	require.Equal(t, uint64(5), set.first)
	require.Equal(t, uint64(blockChunkBits+3), set.last)
	require.Equal(t, int64(3), set.countBelow(128))
	require.Equal(t, int64(6), set.countBelow(blockChunkBits+4))

	var runs []blockRun
	set.emptyRuns(blockRun{from: 0, to: blockChunkBits + 10}, func(run blockRun) { runs = append(runs, run) })
	require.Equal(t, []blockRun{
		{from: 0, to: 4},
		{from: 6, to: 63},
		{from: 65, to: 126},
		{from: 129, to: 199},
		{from: 201, to: blockChunkBits + 2},
		{from: blockChunkBits + 4, to: blockChunkBits + 10},
	}, runs)
}

func TestKeepLongest(t *testing.T) {
	var longest []blockRun
	for _, run := range []blockRun{{1, 2}, {10, 19}, {30, 31}, {40, 49}, {60, 60}} {
		longest = keepLongest(longest, run, 3)
	}
	require.Equal(t, []blockRun{{10, 19}, {40, 49}, {1, 2}}, longest)
	require.Empty(t, keepLongest(nil, blockRun{1, 2}, 0))
}
