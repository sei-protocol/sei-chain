package statesyncproof

import (
	"path/filepath"
	"testing"

	"google.golang.org/protobuf/encoding/protowire"

	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/data"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/pb"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/protoutils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
)

func TestEncodeDecodeRoundTrip(t *testing.T) {
	block, appQC := testProof(t, utils.TestRng())

	encoded, err := Encode(block, appQC)
	require.NoError(t, err)

	decoded, err := Decode(encoded, block.QC().Height())
	require.NoError(t, err)
	require.Equal(t, block.QC().Height(), decoded.Height())
	require.Equal(t, block.Header().Hash(), decoded.RoadBlock().Header().Hash())
	require.Equal(t, len(block.LaneBlocks()), len(decoded.RoadBlock().LaneBlocks()))
	require.Equal(t, appQC.Proposal().Height(), decoded.AppQC().Proposal().Height())
	require.Equal(t, []byte(appQC.Proposal().AppHash()), []byte(decoded.AppQC().Proposal().AppHash()))
}

func TestEncodeForTestingDecodesAtItsHeight(t *testing.T) {
	encoded, err := EncodeForTesting(utils.TestRng(), 7)
	require.NoError(t, err)

	decoded, err := Decode(encoded, 7)
	require.NoError(t, err)
	require.Equal(t, types.RoadHeight(7), decoded.Height())
}

func TestVerifyAcceptsAProofOfTheEpochCommittee(t *testing.T) {
	rng := utils.TestRng()
	committee, keys := types.GenCommittee(rng, 4)
	epoch := types.GenEpochWithCommittee(rng, committee)
	encoded, err := EncodeForEpochForTesting(rng, epoch, keys, []byte{1, 2, 3})
	require.NoError(t, err)
	proof, err := Decode(encoded, epoch.RoadRange().First)
	require.NoError(t, err)

	require.NoError(t, proof.Verify(epoch))
}

func TestVerifyRejectsAProofOfAnotherCommittee(t *testing.T) {
	rng := utils.TestRng()
	committee, keys := types.GenCommittee(rng, 4)
	epoch := types.GenEpochWithCommittee(rng, committee)
	encoded, err := EncodeForEpochForTesting(rng, epoch, keys, []byte{1, 2, 3})
	require.NoError(t, err)
	proof, err := Decode(encoded, epoch.RoadRange().First)
	require.NoError(t, err)
	other, _ := types.GenCommittee(rng, 4)

	require.Error(t, proof.Verify(types.NewEpoch(epoch.EpochIndex(), epoch.RoadRange(), other)))
}

func TestVerifyRejectsAnAppQCBelowQuorum(t *testing.T) {
	rng := utils.TestRng()
	committee, keys := types.GenCommittee(rng, 4)
	epoch := types.GenEpochWithCommittee(rng, committee)
	qc, blocks := data.TestCommitQC(rng, epoch, keys, utils.None[*types.CommitQC]())
	appQC := data.TestAppQC(keys[:1], types.NewAppProposal(qc.QC().View(), []byte{1, 2, 3}))
	encoded, err := Encode(types.NewRoadBlock(qc, blocks), appQC)
	require.NoError(t, err)
	proof, err := Decode(encoded, epoch.RoadRange().First)
	require.NoError(t, err)

	require.Error(t, proof.Verify(epoch))
}

func TestDecodeRejectsMalformedFiles(t *testing.T) {
	cases := map[string][]byte{
		"empty":            {},
		"truncated header": []byte("SEI"),
		"bad magic":        append([]byte("NOTSSP"), formatVersion),
		"unknown version":  append([]byte(fileMagic), 9),
		"truncated proto":  append(append([]byte(fileMagic), formatVersion), 0x0a),
	}
	for name, input := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := Decode(input, 0)
			require.Error(t, err)
		})
	}
}

func TestDecodeAcceptsUnknownFields(t *testing.T) {
	block, appQC := testProof(t, utils.TestRng())
	encoded, err := Encode(block, appQC)
	require.NoError(t, err)
	encoded = protowire.AppendTag(encoded, 99, protowire.BytesType)
	encoded = protowire.AppendBytes(encoded, []byte("future field"))

	decoded, err := Decode(encoded, block.QC().Height())
	require.NoError(t, err)
	require.Equal(t, block.Header().Hash(), decoded.RoadBlock().Header().Hash())
}

func TestDecodeRejectsTooManyLaneBlocks(t *testing.T) {
	laneBlocks := func(n int) *pb.StateSyncProof {
		msg := &pb.StateSyncProof{}
		for range n {
			msg.Blocks = append(msg.Blocks, &pb.LaneBlock{})
		}
		return msg
	}

	_, err := protoutils.Unmarshal[*pb.StateSyncProof](protoutils.Marshal(laneBlocks(1000)))
	require.NoError(t, err)
	_, err = protoutils.Unmarshal[*pb.StateSyncProof](protoutils.Marshal(laneBlocks(1001)))
	require.Error(t, err)
	_, err = Decode(frame(laneBlocks(1001)), 0)
	require.Error(t, err)
}

func TestDecodeRejectsAppQCOfAnotherView(t *testing.T) {
	for name, otherView := range mismatchedViews() {
		t.Run(name, func(t *testing.T) {
			rng := utils.TestRng()
			block, _ := testProof(t, rng)
			appQC := data.TestAppQC(testKeys(t, rng), types.NewAppProposal(otherView(block.QC().QC().View()), []byte{1}))

			encoded := frame(&pb.StateSyncProof{
				CommitQc: types.FullCommitQCConv.Encode(block.QC()),
				Blocks:   types.LaneBlockConv.EncodeSlice(block.LaneBlocks()),
				AppQc:    types.AppQCConv.Encode(appQC),
			})
			_, err := Decode(encoded, block.QC().Height())
			require.Error(t, err)
		})
	}
}

func TestDecodeRejectsUnexpectedHeight(t *testing.T) {
	block, appQC := testProof(t, utils.TestRng())
	encoded, err := Encode(block, appQC)
	require.NoError(t, err)

	_, err = Decode(encoded, block.QC().Height()+1)
	require.Error(t, err)
}

func TestEncodeRejectsAppQCOfAnotherView(t *testing.T) {
	for name, otherView := range mismatchedViews() {
		t.Run(name, func(t *testing.T) {
			rng := utils.TestRng()
			block, _ := testProof(t, rng)
			appQC := data.TestAppQC(testKeys(t, rng), types.NewAppProposal(otherView(block.QC().QC().View()), []byte{1}))

			_, err := Encode(block, appQC)
			require.Error(t, err)
		})
	}
}

// mismatchedViews returns, by case name, functions that change a CommitQC view so an AppQC built
// on it does not belong to that CommitQC.
func mismatchedViews() map[string]func(types.View) types.View {
	return map[string]func(types.View) types.View{
		"another road": func(v types.View) types.View {
			v.Height++
			return v
		},
		"another epoch": func(v types.View) types.View {
			v.EpochIndex++
			return v
		},
	}
}

func TestFileNameRoundTrip(t *testing.T) {
	const index types.RoadHeight = 12345
	name := FileName(index)
	require.Equal(t, "00000000000000012345.bin", name)

	parsed, err := ParseFileName(name)
	require.NoError(t, err)
	require.Equal(t, index, parsed)
}

func TestParseFileNameRejectsNonCanonicalNames(t *testing.T) {
	cases := []string{
		"12345.bin",
		"00000000000000012345.tmp",
		"0000000000000001234a.bin",
		filepath.Join("state_sync_proofs", "00000000000000012345.bin"),
	}
	for _, name := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := ParseFileName(name)
			require.Error(t, err)
		})
	}
}

func testProof(t *testing.T, rng utils.Rng) (*types.RoadBlock, *types.AppQC) {
	t.Helper()
	committee, keys := types.GenCommittee(rng, 4)
	epoch := types.GenEpochWithCommittee(rng, committee)
	qc, blocks := data.TestCommitQC(rng, epoch, keys, utils.None[*types.CommitQC]())
	block := types.NewRoadBlock(qc, blocks)
	proposal := types.NewAppProposal(qc.QC().View(), []byte{1, 2, 3})
	return block, data.TestAppQC(keys, proposal)
}

func testKeys(t *testing.T, rng utils.Rng) []types.SecretKey {
	t.Helper()
	_, keys := types.GenCommittee(rng, 4)
	return keys
}
