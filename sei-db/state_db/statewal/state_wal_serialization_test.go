package statewal

import (
	"encoding/binary"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/seiwal"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/giga/types/mutation"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/ktype"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/vtype"
)

func makeChangeSet(name string, key []byte, value []byte) *proto.NamedChangeSet {
	return &proto.NamedChangeSet{
		Name: name,
		Changeset: proto.ChangeSet{
			Pairs: []*proto.KVPair{{Key: key, Value: value}},
		},
	}
}

// makeMutation returns a mutation that writes value to key in a non-EVM module.
func makeMutation(moduleName string, key []byte, value []byte) mutation.Mutation {
	return mutation.Mutation{
		Key:      ktype.BuildPhysicalKey(moduleName, key),
		NewValue: vtype.SerializeMisc(1, value),
	}
}

// serializeChangesets encodes a changeset list in the changeset format, [version]([uvarint length][marshaled])*,
// which earlier releases wrote and this one only reads.
func serializeChangesets(cs []*proto.NamedChangeSet) ([]byte, error) {
	buf := []byte{changesetFormatVersion}
	var scratch [binary.MaxVarintLen64]byte
	for _, ncs := range cs {
		if ncs == nil {
			return nil, fmt.Errorf("changeset is nil")
		}
		marshaled, err := ncs.Marshal()
		if err != nil {
			return nil, fmt.Errorf("failed to marshal changeset: %w", err)
		}
		n := binary.PutUvarint(scratch[:], uint64(len(marshaled)))
		buf = append(buf, scratch[:n]...)
		buf = append(buf, marshaled...)
	}
	return buf, nil
}

// serializeChangeSetRecord encodes a record holding changesets in the changeset format.
func serializeChangeSetRecord(record stateWALRecord) ([]byte, error) {
	return serializeChangesets(record.changeSets)
}

func TestRecordRoundTrip(t *testing.T) {
	mutations := []mutation.Mutation{
		makeMutation("bank", []byte("a"), []byte("1")),
		makeMutation("staking", []byte("b"), []byte("2")),
	}
	data, err := serializeRecord(stateWALRecord{mutations: mutations})
	require.NoError(t, err)
	require.Equal(t, mutationFormatVersion, data[0], "payload must begin with the format version")

	record, err := deserializeRecord(data)
	require.NoError(t, err)
	require.Equal(t, stateWALRecord{mutations: mutations}, record)
}

func TestDeserializeRecordReadsChangeSetFormat(t *testing.T) {
	changeSets := []*proto.NamedChangeSet{makeChangeSet("bank", []byte("a"), []byte("1"))}
	data, err := serializeChangesets(changeSets)
	require.NoError(t, err)

	record, err := deserializeRecord(data)
	require.NoError(t, err)
	require.Equal(t, stateWALRecord{changeSets: changeSets, isChangeSetFormat: true}, record)
}

func TestDeserializeRecordRejectsInvalidPayload(t *testing.T) {
	_, err := deserializeRecord(nil)
	require.Error(t, err)

	_, err = deserializeRecord([]byte{mutationFormatVersion + 1})
	require.Error(t, err)

	// A version 2 payload whose mutations are truncated.
	mutations := []mutation.Mutation{makeMutation("bank", []byte("a"), nil)}
	data, err := serializeRecord(stateWALRecord{mutations: mutations})
	require.NoError(t, err)
	_, err = deserializeRecord(data[:len(data)-1])
	require.Error(t, err)
}

// A WAL holding blocks written in the changeset format by an earlier release, followed by blocks written as
// mutations, reads every block back as changesets, with the same hashes before and after a reopen.
func TestWALReadsBothRecordFormats(t *testing.T) {
	cfg := testConfig(t.TempDir())

	legacy := map[uint64][]*proto.NamedChangeSet{
		1: {makeChangeSet("bank", []byte("a"), []byte("1"))},
		2: {
			makeChangeSet("bank", []byte("b"), []byte("2")),
			makeChangeSet("staking", []byte("c"), []byte("3")),
		},
	}
	legacyWAL, err := seiwal.NewGenericWAL[stateWALRecord](
		cfg.toSeiwalConfig(), serializeChangeSetRecord, deserializeRecord)
	require.NoError(t, err)
	for block := uint64(1); block <= 2; block++ {
		record := stateWALRecord{changeSets: legacy[block], isChangeSetFormat: true}
		require.NoError(t, legacyWAL.Append(block, record))
	}
	require.NoError(t, legacyWAL.Close())

	written := map[uint64][]mutation.Mutation{
		3: {makeMutation("bank", []byte("a"), []byte("4"))},
		4: {makeMutation("bank", []byte("b"), nil), makeMutation("staking", []byte("c"), []byte("5"))},
	}
	w := openWAL(t, cfg)
	recorder := &hashRecorder{}
	_, _, _, err = w.RegisterHashListener(recorder.listen)
	require.NoError(t, err)
	for block := uint64(3); block <= 4; block++ {
		require.NoError(t, w.Write(block, written[block]))
	}
	require.NoError(t, w.Flush())

	expected := make(map[uint64][]*proto.NamedChangeSet)
	for block, changeSets := range legacy {
		expected[block] = changeSets
	}
	for block, mutations := range written {
		changeSets, err := mutation.MutationsToChangeSets(mutations)
		require.NoError(t, err)
		expected[block] = changeSets
	}
	requireBlocks(t, w, expected)

	hashes := iteratedHashes(t, w, 1, 4)
	for _, hash := range hashes {
		// Blocks stored in the changeset format hash to the zero hash.
		var want [32]byte
		if mutations, ok := written[hash.blockNumber]; ok {
			want = computePlaceholderHash(mutations)
		}
		require.Equal(t, want, hash.hash, "block %d", hash.blockNumber)
	}
	require.Equal(t, hashes[2:], recorder.deliveries, "the delivered hashes must match the iterated ones")
	require.NoError(t, w.Close())

	reopened := openWAL(t, cfg)
	defer func() { require.NoError(t, reopened.Close()) }()
	requireBlocks(t, reopened, expected)
	require.Equal(t, hashes, iteratedHashes(t, reopened, 1, 4))
	ok, last, lastHash, err := reopened.RegisterHashListener(nil)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, uint64(4), last)
	require.Equal(t, hashes[3].hash, lastHash)
}

// requireBlocks requires that w holds exactly the blocks of expected, each reading back as its changesets.
func requireBlocks(t *testing.T, w StateWAL, expected map[uint64][]*proto.NamedChangeSet) {
	t.Helper()
	it, err := w.Iterator(1, uint64(len(expected)))
	require.NoError(t, err)
	defer func() { require.NoError(t, it.Close()) }()

	read := make(map[uint64][]*proto.NamedChangeSet)
	for {
		ok, err := it.Next()
		require.NoError(t, err)
		if !ok {
			break
		}
		block, changeSets := it.Entry()
		read[block] = changeSets
	}
	require.Equal(t, expected, read)
}

func TestChangesetsRoundTrip(t *testing.T) {
	t.Run("multiple named change sets", func(t *testing.T) {
		cs := []*proto.NamedChangeSet{
			makeChangeSet("bank", []byte("a"), []byte("1")),
			makeChangeSet("evm", []byte("b"), []byte("2")),
		}

		data, err := serializeChangesets(cs)
		require.NoError(t, err)
		require.Equal(t, changesetFormatVersion, data[0], "payload must begin with the format version")

		got, err := deserializeChangesets(data)
		require.NoError(t, err)
		require.Equal(t, cs, got)
	})

	t.Run("empty changeset list", func(t *testing.T) {
		data, err := serializeChangesets([]*proto.NamedChangeSet{})
		require.NoError(t, err)
		require.Equal(t, []byte{changesetFormatVersion}, data) // just the version byte

		got, err := deserializeChangesets(data)
		require.NoError(t, err)
		require.Empty(t, got)
	})
}

func TestDeserializeUnknownVersion(t *testing.T) {
	// A payload whose leading version byte is not recognized must be rejected before any decoding.
	_, err := deserializeChangesets([]byte{changesetFormatVersion + 1})
	require.Error(t, err)

	// An empty payload is missing the version byte entirely.
	_, err = deserializeChangesets(nil)
	require.Error(t, err)
}

func TestDeserializeChangesetsTruncated(t *testing.T) {
	cs := []*proto.NamedChangeSet{
		makeChangeSet("bank", []byte("hello"), []byte("world")),
	}
	data, err := serializeChangesets(cs)
	require.NoError(t, err)

	// Every strict prefix that reaches past the version byte is truncated. Because the enclosing record is
	// length-delimited by the underlying WAL, a truncated payload here is corruption and must surface an
	// error, never a silent partial decode. (Length 1 is the bare version byte, a valid empty payload.)
	for length := 2; length < len(data); length++ {
		_, err := deserializeChangesets(data[:length])
		require.Error(t, err)
	}
}

func TestDeserializeCorruptChangeset(t *testing.T) {
	// A length prefix pointing at bytes that are not a valid NamedChangeSet protobuf must surface an error.
	// Layout: [version][len=2][0x08 0xFF] where 0x08 is a varint field tag (field 1, wire type 0) followed by
	// a truncated varint, which the protobuf decoder rejects.
	payload := []byte{changesetFormatVersion, 0x02, 0x08, 0xFF}
	_, err := deserializeChangesets(payload)
	require.Error(t, err)
}
