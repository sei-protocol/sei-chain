package littblock

import (
	"bytes"
	"encoding/binary"
	"math"
	"testing"

	blocktypes "github.com/sei-protocol/sei-chain/sei-db/ledger_db/block/types"
	"github.com/stretchr/testify/require"
)

func TestKeyRoundTrip(t *testing.T) {
	for _, n := range []blocktypes.BlockNumber{0, 1, 42, 255, 256, 1 << 32, ^blocktypes.BlockNumber(0)} {
		block := blockKey{Index: n}
		gotBlock, err := parseTestKey(t, block.encode()).decodeBlock()
		require.NoError(t, err)
		require.Equal(t, block, gotBlock)
		qc := qcKey{Index: n}
		gotQC, err := parseTestKey(t, qc.encode()).decodeQC()
		require.NoError(t, err)
		require.Equal(t, qc, gotQC)
		appQC := appQCKey{Index: n}
		gotAppQC, err := parseTestKey(t, appQC.encode()).decodeAppQC()
		require.NoError(t, err)
		require.Equal(t, appQC, gotAppQC)
		proposal := appProposalKey{Index: n}
		gotProposal, err := parseTestKey(t, proposal.encode()).decodeAppProposal()
		require.NoError(t, err)
		require.Equal(t, proposal, gotProposal)
		for _, offset := range []uint64{0, 1, math.MaxUint64} {
			lane := laneBlockKey{Index: n, Offset: offset}
			gotLane, err := parseTestKey(t, lane.encode()).decodeLaneBlock()
			require.NoError(t, err)
			require.Equal(t, lane, gotLane)
		}
	}
	var hash blocktypes.BlockHash
	for i := range hash {
		hash[i] = byte(i)
	}
	key := blockHashKey{Hash: hash}
	got, err := parseTestKey(t, key.encode()).decodeBlockHash()
	require.NoError(t, err)
	require.Equal(t, key, got)
}

func TestKeyBigEndianOrdering(t *testing.T) {
	for _, pair := range [][2]blocktypes.BlockNumber{{0, 1}, {1, 2}, {255, 256}, {1 << 16, 1 << 32}} {
		require.Negative(t, bytes.Compare((blockKey{Index: pair[0]}).encode().bytes(), (blockKey{Index: pair[1]}).encode().bytes()))
	}
	first := (laneBlockKey{Index: 42, Offset: 255}).encode()
	next := (laneBlockKey{Index: 42, Offset: 256}).encode()
	require.Negative(t, bytes.Compare(first.bytes(), next.bytes()))
}

func TestKeyLayouts(t *testing.T) {
	var hash blocktypes.BlockHash
	for i := range hash {
		hash[i] = byte(i)
	}
	cases := []struct {
		key  ledgerKey
		want []byte
	}{
		{(blockKey{Index: 42}).encode(), binary.BigEndian.AppendUint64([]byte{0}, 42)},
		{(appQCKey{Index: 42}).encode(), binary.BigEndian.AppendUint64([]byte{1}, 42)},
		{(laneBlockKey{Index: 42, Offset: 7}).encode(), binary.BigEndian.AppendUint64(binary.BigEndian.AppendUint64([]byte{2}, 42), 7)},
		{(blockHashKey{Hash: hash}).encode(), append([]byte{3}, hash[:]...)},
		{(appProposalKey{Index: 42}).encode(), binary.BigEndian.AppendUint64([]byte{4}, 42)},
		{(qcKey{Index: 42}).encode(), binary.BigEndian.AppendUint64([]byte{5}, 42)},
	}
	for _, tc := range cases {
		require.Equal(t, tc.want, tc.key.bytes())
	}
}

func TestKeyDecodersRejectInvalidKeys(t *testing.T) {
	cases := []struct {
		key    ledgerKey
		decode func(ledgerKey) error
	}{
		{(blockKey{}).encode(), func(k ledgerKey) error { _, err := k.decodeBlock(); return err }},
		{(qcKey{}).encode(), func(k ledgerKey) error { _, err := k.decodeQC(); return err }},
		{(appQCKey{}).encode(), func(k ledgerKey) error { _, err := k.decodeAppQC(); return err }},
		{(appProposalKey{}).encode(), func(k ledgerKey) error { _, err := k.decodeAppProposal(); return err }},
		{(laneBlockKey{}).encode(), func(k ledgerKey) error { _, err := k.decodeLaneBlock(); return err }},
		{(blockHashKey{}).encode(), func(k ledgerKey) error { _, err := k.decodeBlockHash(); return err }},
	}
	for i, tc := range cases {
		for j, other := range cases {
			if i != j {
				require.Error(t, tc.decode(other.key))
			}
		}
		for _, invalid := range []ledgerKey{
			{kind: tc.key.kind},
			{kind: tc.key.kind, payload: tc.key.payload[:len(tc.key.payload)-1]},
			{kind: tc.key.kind, payload: append(append([]byte{}, tc.key.payload...), 0)},
		} {
			require.Error(t, tc.decode(invalid))
		}
	}
}

func TestVarintKindPrefixes(t *testing.T) {
	for _, kind := range []uint64{0, 127, 128, 300, math.MaxUint64} {
		encoded := append(binary.AppendUvarint(nil, kind), 1, 2, 3)
		key, err := decodeLedgerKey(encoded)
		require.NoError(t, err)
		require.Equal(t, recordKind(kind), key.kind)
		require.Equal(t, []byte{1, 2, 3}, key.payload)
		require.Equal(t, encoded, key.bytes())
	}
}

func TestInvalidKindUvarints(t *testing.T) {
	db := &blockDB{}
	for _, value := range [][]byte{
		nil,
		{0x80},
		bytes.Repeat([]byte{0x80}, 10),
		append(bytes.Repeat([]byte{0xff}, 9), 2),
		append(bytes.Repeat([]byte{0x80}, 10), 0),
	} {
		_, err := decodeLedgerKey(value)
		require.Error(t, err)
		reclaim, err := db.gcFilter(value, false)
		require.Error(t, err)
		require.False(t, reclaim)
	}
}

func parseTestKey(t *testing.T, key ledgerKey) ledgerKey {
	t.Helper()
	decoded, err := decodeLedgerKey(key.bytes())
	require.NoError(t, err)
	require.Equal(t, key, decoded)
	return decoded
}
