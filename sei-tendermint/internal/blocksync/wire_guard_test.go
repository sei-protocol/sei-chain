package blocksync

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/encoding/protowire"

	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/protoutils/wireguard"
	bcproto "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/blocksync"
	tmproto "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/types"
)

// commitWireBytes builds the wire-format bytes for a Commit containing
// sigCount empty Signatures entries — enough to exercise the cap without
// constructing any meaningful payload.
func commitWireBytes(sigCount int) []byte {
	var commit []byte
	for i := 0; i < sigCount; i++ {
		commit = protowire.AppendTag(commit, fieldCommitSignatures, protowire.BytesType)
		commit = protowire.AppendVarint(commit, 0)
	}
	return commit
}

// blocksyncWireBytes wraps commit bytes in the BlockResponse → Block → Commit
// framing that validateBlocksyncWire walks.
func blocksyncWireBytes(commit []byte) []byte {
	block := protowire.AppendTag(nil, fieldBlockLastCommit, protowire.BytesType)
	block = protowire.AppendVarint(block, uint64(len(commit)))
	block = append(block, commit...)

	blockResp := protowire.AppendTag(nil, fieldBlockResponseBlock, protowire.BytesType)
	blockResp = protowire.AppendVarint(blockResp, uint64(len(block)))
	blockResp = append(blockResp, block...)

	msg := protowire.AppendTag(nil, fieldMessageBlockResponse, protowire.BytesType)
	msg = protowire.AppendVarint(msg, uint64(len(blockResp)))
	msg = append(msg, blockResp...)
	return msg
}

func TestValidateBlocksyncWire_AcceptsLegitimate(t *testing.T) {
	// Exactly at the cap is allowed; only strictly greater is rejected.
	bz := blocksyncWireBytes(commitWireBytes(types.MaxVotesCount))
	require.NoError(t, validateBlocksyncWire(bz))
}

func TestValidateBlocksyncWire_AcceptsEmpty(t *testing.T) {
	require.NoError(t, validateBlocksyncWire(nil))
	require.NoError(t, validateBlocksyncWire(blocksyncWireBytes(nil)))
}

func TestValidateBlocksyncWire_RejectsOverCap(t *testing.T) {
	bz := blocksyncWireBytes(commitWireBytes(MaxCommitSignatures + 1))
	err := validateBlocksyncWire(bz)
	require.Error(t, err)
	require.True(t, strings.Contains(err.Error(), "exceeds max"), "got %v", err)
}

func TestValidateBlocksyncWire_IgnoresUnrelatedFields(t *testing.T) {
	// A BlockRequest carrying many CommitSig-shaped tags should not trigger
	// the check: the scanner only descends into BlockResponse.
	var blockRequest []byte
	for i := 0; i < MaxCommitSignatures+1; i++ {
		blockRequest = protowire.AppendTag(blockRequest, fieldCommitSignatures, protowire.BytesType)
		blockRequest = protowire.AppendVarint(blockRequest, 0)
	}
	msg := protowire.AppendTag(nil, fieldMessageBlockRequest, protowire.BytesType)
	msg = protowire.AppendVarint(msg, uint64(len(blockRequest)))
	msg = append(msg, blockRequest...)
	require.NoError(t, validateBlocksyncWire(msg))
}

func TestValidateBlocksyncWire_RejectsMalformed(t *testing.T) {
	// Length-delimited field whose declared length runs past the buffer end.
	bz := protowire.AppendTag(nil, fieldMessageBlockResponse, protowire.BytesType)
	bz = protowire.AppendVarint(bz, 100) // claims 100 bytes that don't exist
	require.Error(t, validateBlocksyncWire(bz))
}

func TestValidateBlocksyncWire_RejectsWellOverCap(t *testing.T) {
	// Far past the cap to confirm the check short-circuits rather than
	// scanning the whole buffer.
	bz := blocksyncWireBytes(commitWireBytes(MaxCommitSignatures * 4))
	require.Error(t, validateBlocksyncWire(bz))
}

func TestFieldNumbersMatchProto(t *testing.T) {
	// Documents the resolved field numbers and catches any regression in the
	// tag parser. If proto regen renames a field, init() panics before this
	// test runs — that is the louder failure mode by design.
	require.Equal(t, protowire.Number(1), fieldMessageBlockRequest)
	require.Equal(t, protowire.Number(3), fieldMessageBlockResponse)
	require.Equal(t, protowire.Number(1), fieldBlockResponseBlock)
	require.Equal(t, protowire.Number(4), fieldBlockLastCommit)
	require.Equal(t, protowire.Number(4), fieldCommitSignatures)
}

func TestMustFieldNum_UnknownFieldPanics(t *testing.T) {
	require.PanicsWithValue(t,
		`wireguard: proto field "definitely_not_a_field" not found on Commit`,
		func() { wireguard.MustFieldNum((*tmproto.Commit)(nil), "definitely_not_a_field") })
}

func TestMustFieldNum_KnownField(t *testing.T) {
	// Sanity-check that wireguard.MustFieldNum returns the proto-declared number for a
	// non-trivial field we don't otherwise read in production.
	require.Equal(t, protowire.Number(1), wireguard.MustFieldNum((*bcproto.BlockRequest)(nil), "height"))
}
