package migration

import (
	"encoding/binary"
	"testing"

	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/stretchr/testify/require"
)

// --- encodeWALRecord / decodeWALRecord round trip ---

func TestWALRecord_RoundTripEmpty(t *testing.T) {
	encoded, err := encodeWALRecord(nil, nil)
	require.NoError(t, err)
	// Two u32 count headers, both zero.
	require.Len(t, encoded, 8)

	oldCS, newCS, err := decodeWALRecord(encoded)
	require.NoError(t, err)
	require.Empty(t, oldCS)
	require.Empty(t, newCS)
}

func TestWALRecord_RoundTripSingleStore(t *testing.T) {
	old := []*proto.NamedChangeSet{
		{Name: "bank", Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{
			{Key: []byte("a"), Delete: true},
			{Key: []byte("b"), Value: []byte("2")},
		}}},
	}
	newCSs := []*proto.NamedChangeSet{
		{Name: "bank", Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{
			{Key: []byte("a"), Value: []byte("1")},
		}}},
	}

	encoded, err := encodeWALRecord(old, newCSs)
	require.NoError(t, err)

	decodedOld, decodedNew, err := decodeWALRecord(encoded)
	require.NoError(t, err)
	requireChangeSetsEqual(t, old, decodedOld)
	requireChangeSetsEqual(t, newCSs, decodedNew)
}

func TestWALRecord_RoundTripMultipleStores(t *testing.T) {
	old := []*proto.NamedChangeSet{
		{Name: "auth", Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{
			{Key: []byte("x"), Delete: true},
		}}},
		{Name: "bank", Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{
			{Key: []byte("y"), Value: []byte("v1")},
			{Key: []byte("z"), Value: []byte("v2")},
		}}},
		{Name: MigrationStore, Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{
			{Key: []byte(OldDBBatchIDKey), Value: u64Bytes(42)},
		}}},
	}
	newCSs := []*proto.NamedChangeSet{
		{Name: "bank", Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{
			{Key: []byte("y"), Value: []byte("v1-new")},
		}}},
		{Name: MigrationStore, Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{
			{Key: []byte(FlatKVMigrationBoundaryKey), Value: MigrationBoundaryComplete.Serialize()},
			{Key: []byte(NewDBBatchIDKey), Value: u64Bytes(42)},
		}}},
	}

	encoded, err := encodeWALRecord(old, newCSs)
	require.NoError(t, err)

	decodedOld, decodedNew, err := decodeWALRecord(encoded)
	require.NoError(t, err)
	requireChangeSetsEqual(t, old, decodedOld)
	requireChangeSetsEqual(t, newCSs, decodedNew)
}

func TestWALRecord_RoundTripPreservesByteValues(t *testing.T) {
	// Values with every possible byte pattern should survive a round trip
	// unchanged. Length-prefix framing must not corrupt arbitrary bytes.
	weirdValue := make([]byte, 256)
	for i := range weirdValue {
		weirdValue[i] = byte(i)
	}
	old := []*proto.NamedChangeSet{
		{Name: "bank", Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{
			{Key: []byte{0x00}, Value: weirdValue},
			{Key: []byte{0xff, 0xff, 0xff}, Value: nil},
		}}},
	}

	encoded, err := encodeWALRecord(old, nil)
	require.NoError(t, err)

	decodedOld, decodedNew, err := decodeWALRecord(encoded)
	require.NoError(t, err)
	requireChangeSetsEqual(t, old, decodedOld)
	require.Empty(t, decodedNew)
}

// --- decodeWALRecord failure modes ---

func TestWALRecord_DecodeTruncatedCountHeader(t *testing.T) {
	// Fewer than 4 bytes for the old-list count header.
	_, _, err := decodeWALRecord([]byte{0x00, 0x00})
	require.Error(t, err)
	require.Contains(t, err.Error(), "count header")
}

func TestWALRecord_DecodeTruncatedBodyLengthHeader(t *testing.T) {
	// count=1 but only 2 bytes of item-length header follow.
	buf := make([]byte, 0, 6)
	buf = binary.BigEndian.AppendUint32(buf, 1)
	buf = append(buf, 0x00, 0x00)
	_, _, err := decodeWALRecord(buf)
	require.Error(t, err)
	require.Contains(t, err.Error(), "length header")
}

func TestWALRecord_DecodeTruncatedBody(t *testing.T) {
	// count=1, item-length=10, but only 3 bytes of body follow.
	buf := make([]byte, 0, 11)
	buf = binary.BigEndian.AppendUint32(buf, 1)
	buf = binary.BigEndian.AppendUint32(buf, 10)
	buf = append(buf, 0x01, 0x02, 0x03)
	_, _, err := decodeWALRecord(buf)
	require.Error(t, err)
	require.Contains(t, err.Error(), "body")
}

func TestWALRecord_DecodeTrailingGarbage(t *testing.T) {
	encoded, err := encodeWALRecord(nil, nil)
	require.NoError(t, err)
	encoded = append(encoded, 0x42)

	_, _, err = decodeWALRecord(encoded)
	require.Error(t, err)
	require.Contains(t, err.Error(), "trailing data")
}

func TestWALRecord_DecodeGarbageProto(t *testing.T) {
	// old-count=1, item-length=5, body = 5 random bytes that aren't a
	// valid NamedChangeSet.
	buf := make([]byte, 0, 13)
	buf = binary.BigEndian.AppendUint32(buf, 1)
	buf = binary.BigEndian.AppendUint32(buf, 5)
	buf = append(buf, 0xde, 0xad, 0xbe, 0xef, 0x01)
	buf = binary.BigEndian.AppendUint32(buf, 0) // empty new list

	_, _, err := decodeWALRecord(buf)
	require.Error(t, err)
	require.Contains(t, err.Error(), "unmarshal")
}

// --- helpers ---

func u64Bytes(v uint64) []byte {
	b := make([]byte, 8)
	binary.BigEndian.PutUint64(b, v)
	return b
}

// requireChangeSetsEqual compares two NamedChangeSet slices pair-by-pair so
// failures pinpoint the mismatched element instead of dumping protobuf
// strings. Treats nil and len==0 as equivalent for both lists and pair
// slices, matching the round-trip semantics (an empty list encodes to a
// zero-count header and decodes back to an empty slice).
func requireChangeSetsEqual(t *testing.T, expected, actual []*proto.NamedChangeSet) {
	t.Helper()
	require.Equal(t, len(expected), len(actual), "change set list length")
	for i := range expected {
		require.Equal(t, expected[i].Name, actual[i].Name, "name at %d", i)
		ePairs := expected[i].Changeset.Pairs
		aPairs := actual[i].Changeset.Pairs
		require.Equal(t, len(ePairs), len(aPairs), "pair count at %d", i)
		for j := range ePairs {
			require.Equal(t, ePairs[j].Key, aPairs[j].Key, "pair key at %d/%d", i, j)
			require.Equal(t, ePairs[j].Value, aPairs[j].Value, "pair value at %d/%d", i, j)
			require.Equal(t, ePairs[j].Delete, aPairs[j].Delete, "pair delete flag at %d/%d", i, j)
		}
	}
}
