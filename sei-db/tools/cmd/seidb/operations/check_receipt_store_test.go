package operations

import (
	"bytes"
	"io"
	"math"
	"math/big"
	"slices"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	gogodescriptor "github.com/gogo/protobuf/protoc-gen-gogo/descriptor"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/encoding/protowire"

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

	_, err = receiptBlockNumber(value[:len(value)-1])
	require.Error(t, err, "a receipt cut short after its block number does not decode")

	value, err = (&evmtypes.Receipt{GasUsed: 21000}).Marshal()
	require.NoError(t, err)
	block, err = receiptBlockNumber(value)
	require.NoError(t, err)
	require.Zero(t, block)
}

// receiptFieldTypes maps the proto types of x/evm's Receipt and Log fields to
// the fieldType the receipt decoder reads each as.
var receiptFieldTypes = map[gogodescriptor.FieldDescriptorProto_Type]fieldType{
	gogodescriptor.FieldDescriptorProto_TYPE_UINT64:  uint64Field,
	gogodescriptor.FieldDescriptorProto_TYPE_UINT32:  uint32Field,
	gogodescriptor.FieldDescriptorProto_TYPE_BOOL:    boolField,
	gogodescriptor.FieldDescriptorProto_TYPE_STRING:  stringField,
	gogodescriptor.FieldDescriptorProto_TYPE_BYTES:   bytesField,
	gogodescriptor.FieldDescriptorProto_TYPE_MESSAGE: messageField,
}

// describeProtoMessage returns the description of md, a message of file fd,
// that the message's descriptor gives.
func describeProtoMessage(t *testing.T, fd *gogodescriptor.FileDescriptorProto, md *gogodescriptor.DescriptorProto) protoMessage {
	t.Helper()
	msg := make(protoMessage, len(md.GetField()))
	for _, f := range md.GetField() {
		typ, ok := receiptFieldTypes[f.GetType()]
		require.True(t, ok, "%s.%s is a %s, which the receipt decoder does not read", md.GetName(), f.GetName(), f.GetType())
		field := protoField{
			name:     f.GetName(),
			typ:      typ,
			repeated: f.GetLabel() == gogodescriptor.FieldDescriptorProto_LABEL_REPEATED,
		}
		if typ == messageField {
			field.message = describeProtoMessage(t, fd, messageOfFile(t, fd, f.GetTypeName()))
		}
		msg[protowire.Number(f.GetNumber())] = field
	}
	return msg
}

// messageOfFile returns the message of file fd whose fully qualified name is
// typeName.
func messageOfFile(t *testing.T, fd *gogodescriptor.FileDescriptorProto, typeName string) *gogodescriptor.DescriptorProto {
	t.Helper()
	for _, md := range fd.GetMessageType() {
		if typeName == "."+fd.GetPackage()+"."+md.GetName() {
			return md
		}
	}
	require.FailNow(t, typeName+" is not a message of "+fd.GetName())
	return nil
}

// receiptMessage, with the Log message its logs hold, matches x/evm's Receipt
// field for field, so a change to either message fails here until the
// description follows it.
func TestReceiptMessageIsTheEVMModules(t *testing.T) {
	fd, md := gogodescriptor.ForMessage(&evmtypes.Receipt{})
	require.Equal(t, describeProtoMessage(t, fd, md), receiptMessage)
	require.Equal(t, "block_number", receiptMessage[receiptBlockNumberField].name)
	require.Equal(t, "transaction_index", receiptMessage[receiptTxIndexField].name)
}

// withVarint returns message b with a varint field num holding v appended.
func withVarint(b []byte, num protowire.Number, v uint64) []byte {
	return protowire.AppendVarint(protowire.AppendTag(slices.Clip(b), num, protowire.VarintType), v)
}

// withBytes returns message b with a length-delimited field num holding v
// appended.
func withBytes(b []byte, num protowire.Number, v []byte) []byte {
	return protowire.AppendBytes(protowire.AppendTag(slices.Clip(b), num, protowire.BytesType), v)
}

// A receipt decodes only when it and each of its logs hold nothing but fields
// of their message, each in the wire type of its type and holding a value of
// that type.
func TestReceiptBlockNumberChecksEveryField(t *testing.T) {
	log := evmtypes.Log{Address: "0xa", Topics: []string{"0xt"}, Data: []byte{1}, Index: 2, Synthetic: true}
	logValue, err := log.Marshal()
	require.NoError(t, err)
	receipt, err := (&evmtypes.Receipt{
		TxType: math.MaxUint32, CumulativeGasUsed: 100, ContractAddress: "0xc", TxHashHex: "0xh", GasUsed: 21000,
		EffectiveGasPrice: 7, BlockNumber: 9, TransactionIndex: 1, Status: 1, From: "0xf", To: "0xt",
		VmError: "boom", Logs: []*evmtypes.Log{&log}, LogsBloom: []byte{1}, PreExecutionFailure: true,
	}).Marshal()
	require.NoError(t, err)
	block, err := receiptBlockNumber(receipt)
	require.NoError(t, err, "a receipt setting every field decodes")
	require.Equal(t, uint64(9), block)

	for _, tc := range []struct {
		name    string
		receipt []byte
		wantErr string
	}{
		{"unknown field", withVarint(receipt, 16, 1), "unknown field 16"},
		{"varint field in another wire type", withBytes(receipt, 7, []byte{9}), "block_number has wire type 2"},
		{"string field in another wire type", withVarint(receipt, 3, 1), "contract_address has wire type 0"},
		{"uint32 field above 32 bits", withVarint(receipt, 1, math.MaxUint32+1), "tx_type holds 4294967296"},
		{"bool field above 1", withVarint(receipt, 15, 2), "pre_execution_failure holds 2"},
		{"logs in another wire type", withVarint(receipt, 13, 1), "logs has wire type 0"},
		{"log with an unknown field", withBytes(receipt, 13, withVarint(logValue, 6, 1)), "logs: unknown field 6"},
		{"log topic in another wire type", withBytes(receipt, 13, withVarint(logValue, 2, 1)), "logs: topics has wire type 0"},
		{"log index above 32 bits", withBytes(receipt, 13, withVarint(logValue, 4, math.MaxUint32+1)), "logs: index holds 4294967296"},
		{"log cut short", withBytes(receipt, 13, logValue[:len(logValue)-1]), "logs: unexpected EOF"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := receiptBlockNumber(tc.receipt)
			require.ErrorContains(t, err, tc.wantErr)
		})
	}
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

// Receipts stored above their own block, as legacy receipts moved out of x/evm
// state are, count for their own blocks.
func TestCheckReceiptStoreCountsMovedLegacyReceipts(t *testing.T) {
	receipts := atTheirBlocks(21, 30)
	for block := int64(10); block <= 20; block++ {
		receipts = append(receipts, storedReceipt{version: 31, block: block})
	}
	dir := newReceiptStore(t, 0, receipts...)

	out, err := runCheckReceiptStore(t, "--dir", dir)
	require.NoError(t, err)
	require.Contains(t, out, "receipts: 21 for 21 blocks, blocks 10-30")
	require.Contains(t, out, "receipts stored above their own block, as moved legacy receipts are: 11\n")
}

// Entries that do not decode, or that name a block above the version they are
// stored at, are reported and counted for no block, however far off the block
// they name.
func TestCheckReceiptStoreFlagsBrokenEntries(t *testing.T) {
	dir := t.TempDir()
	store, err := mvcc.OpenDB(dir, config.DefaultStateStoreConfig())
	require.NoError(t, err)
	early, err := (&evmtypes.Receipt{BlockNumber: 12}).Marshal()
	require.NoError(t, err)
	farOff, err := (&evmtypes.Receipt{BlockNumber: 1 << 60}).Marshal()
	require.NoError(t, err)
	cutShort, err := (&evmtypes.Receipt{BlockNumber: 13, Logs: []*evmtypes.Log{{Address: "0xa"}}}).Marshal()
	require.NoError(t, err)
	putReceiptStoreEntry(t, store, 10, evmtypes.ReceiptKey(common.HexToHash("0x1")), early)
	putReceiptStoreEntry(t, store, 11, evmtypes.ReceiptKey(common.HexToHash("0x2")), []byte{0xff})
	putReceiptStoreEntry(t, store, 12, evmtypes.ReceiptKey(common.HexToHash("0x3")), farOff)
	putReceiptStoreEntry(t, store, 13, evmtypes.ReceiptKey(common.HexToHash("0x4")), cutShort[:len(cutShort)-1])
	require.NoError(t, store.Close())

	out, err := runCheckReceiptStore(t, "--dir", dir)
	require.ErrorContains(t, err, "2 entries do not decode as receipts with a block number")
	require.ErrorContains(t, err, "2 receipts are stored below their own block")
	require.ErrorContains(t, err, "holds no receipts")
	require.Contains(t, out, "receipts: 0\n")
}

// Receipts stored above the latest version, which no read finds, are reported
// and counted for no block.
func TestCheckReceiptStoreFlagsEntriesAboveTheLatestVersion(t *testing.T) {
	dir := newReceiptStore(t, 0, append(atTheirBlocks(10, 11), storedReceipt{version: 20, block: 20})...)
	store, err := mvcc.OpenDB(dir, config.DefaultStateStoreConfig())
	require.NoError(t, err)
	require.NoError(t, store.SetLatestVersion(15))
	require.NoError(t, store.Close())

	out, err := runCheckReceiptStore(t, "--dir", dir)
	require.ErrorContains(t, err, "1 entries are stored above its latest version 15, where no read finds them")
	require.Contains(t, out, "receipts: 2 for 2 blocks, blocks 10-11\n")
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
