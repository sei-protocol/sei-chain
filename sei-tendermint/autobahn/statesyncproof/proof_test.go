package statesyncproof

import (
	"path/filepath"
	"slices"
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
	roadBlock, appQC := testProof(t, utils.TestRng())

	encoded, err := Encode(roadBlock, appQC)
	require.NoError(t, err)

	decoded, err := Decode(encoded, roadBlock.QC().Index())
	require.NoError(t, err)
	require.Equal(t, roadBlock.QC().Index(), decoded.Index())
	require.Equal(t, roadBlock.Header().Hash(), decoded.RoadBlock().Header().Hash())
	require.Equal(t, len(roadBlock.LaneBlocks()), len(decoded.RoadBlock().LaneBlocks()))
	require.Equal(t, appQC.Proposal().Index(), decoded.AppQC().Proposal().Index())
	require.Equal(t, []byte(appQC.Proposal().AppHash()), []byte(decoded.AppQC().Proposal().AppHash()))
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
	roadBlock, appQC := testProof(t, utils.TestRng())
	encoded, err := Encode(roadBlock, appQC)
	require.NoError(t, err)
	encoded = protowire.AppendTag(encoded, 99, protowire.BytesType)
	encoded = protowire.AppendBytes(encoded, []byte("future field"))

	decoded, err := Decode(encoded, roadBlock.QC().Index())
	require.NoError(t, err)
	require.Equal(t, roadBlock.Header().Hash(), decoded.RoadBlock().Header().Hash())
}

func TestDecodeRejectsTooManyLaneBlocks(t *testing.T) {
	laneBlocks := func(n int) *pb.StateSyncProof {
		msg := &pb.StateSyncProof{}
		for range n {
			msg.LaneBlocks = append(msg.LaneBlocks, &pb.Block{})
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
			roadBlock, _ := testProof(t, rng)
			appQC := data.TestAppQC(testKeys(t, rng), types.NewAppProposal(otherView(roadBlock.QC().QC().View()), []byte{1}))

			encoded := frame(&pb.StateSyncProof{
				CommitQc:   types.FullCommitQCConv.Encode(roadBlock.QC()),
				LaneBlocks: types.BlockConv.EncodeSlice(roadBlock.LaneBlocks()),
				AppQc:      types.AppQCConv.Encode(appQC),
			})
			_, err := Decode(encoded, roadBlock.QC().Index())
			require.Error(t, err)
		})
	}
}

func TestDecodeRejectsUnexpectedIndex(t *testing.T) {
	roadBlock, appQC := testProof(t, utils.TestRng())
	encoded, err := Encode(roadBlock, appQC)
	require.NoError(t, err)

	_, err = Decode(encoded, roadBlock.QC().Index()+1)
	require.Error(t, err)
}

func TestEncodeRejectsAppQCOfAnotherView(t *testing.T) {
	for name, otherView := range mismatchedViews() {
		t.Run(name, func(t *testing.T) {
			rng := utils.TestRng()
			roadBlock, _ := testProof(t, rng)
			appQC := data.TestAppQC(testKeys(t, rng), types.NewAppProposal(otherView(roadBlock.QC().QC().View()), []byte{1}))

			_, err := Encode(roadBlock, appQC)
			require.Error(t, err)
		})
	}
}

// mismatchedViews returns, by case name, functions that change a CommitQC view so an AppQC built
// on it does not belong to that CommitQC.
func mismatchedViews() map[string]func(types.View) types.View {
	return map[string]func(types.View) types.View{
		"another road": func(v types.View) types.View {
			v.Index++
			return v
		},
		"another epoch": func(v types.View) types.View {
			v.EpochIndex++
			return v
		},
	}
}

func TestFileNameRoundTrip(t *testing.T) {
	const index types.RoadIndex = 12345
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

func TestVerifyAcceptsTheEpochThatSignedIt(t *testing.T) {
	epoch, roadBlock, appQC := testSignedProof(t, utils.TestRng())
	proof := decodeProof(t, roadBlock, appQC)

	require.NoError(t, proof.Verify(epoch))
}

func TestVerifyRejectsAnotherCommittee(t *testing.T) {
	rng := utils.TestRng()
	epoch, roadBlock, appQC := testSignedProof(t, rng)
	proof := decodeProof(t, roadBlock, appQC)
	other, _ := types.GenCommittee(rng, 4)

	require.Error(t, proof.Verify(types.NewEpoch(epoch.EpochIndex(), epoch.RoadRange(), other)))
}

func TestVerifyRejectsAnAppQCOfAnotherCommittee(t *testing.T) {
	rng := utils.TestRng()
	epoch, roadBlock, appQC := testSignedProof(t, rng)
	proof := decodeProof(t, roadBlock, data.TestAppQC(testKeys(t, rng), appQC.Proposal()))

	require.Error(t, proof.Verify(epoch))
}

func TestVerifyRejectsLaneBlocksThatDoNotMatchTheQC(t *testing.T) {
	rng := utils.TestRng()
	epoch, roadBlock, appQC := testSignedProof(t, rng)
	laneBlocks := slices.Clone(roadBlock.LaneBlocks())
	laneBlocks[0] = types.GenBlock(rng)
	encoded := frame(&pb.StateSyncProof{
		CommitQc:   types.FullCommitQCConv.Encode(roadBlock.QC()),
		LaneBlocks: types.BlockConv.EncodeSlice(laneBlocks),
		AppQc:      types.AppQCConv.Encode(appQC),
	})
	proof, err := Decode(encoded, roadBlock.QC().Index())
	require.NoError(t, err)

	require.Error(t, proof.Verify(epoch))
}

// decodeProof encodes roadBlock and appQC as a state sync proof file and decodes it.
func decodeProof(t *testing.T, roadBlock *types.RoadBlock, appQC *types.AppQC) *Proof {
	t.Helper()
	encoded, err := Encode(roadBlock, appQC)
	require.NoError(t, err)
	proof, err := Decode(encoded, roadBlock.QC().Index())
	require.NoError(t, err)
	return proof
}

func testProof(t *testing.T, rng utils.Rng) (*types.RoadBlock, *types.AppQC) {
	t.Helper()
	_, roadBlock, appQC := testSignedProof(t, rng)
	return roadBlock, appQC
}

// testSignedProof returns a road block and an AppQC of it, both signed by the committee of the
// returned epoch.
func testSignedProof(t *testing.T, rng utils.Rng) (*types.Epoch, *types.RoadBlock, *types.AppQC) {
	t.Helper()
	committee, keys := types.GenCommittee(rng, 4)
	epoch := types.GenEpochWithCommittee(rng, committee)
	qc, blocks := data.TestCommitQC(rng, epoch, keys, utils.None[*types.CommitQC]())
	roadBlock := types.NewRoadBlock(qc, blocks)
	proposal := types.NewAppProposal(qc.QC().View(), []byte{1, 2, 3})
	return epoch, roadBlock, data.TestAppQC(keys, proposal)
}

func testKeys(t *testing.T, rng utils.Rng) []types.SecretKey {
	t.Helper()
	_, keys := types.GenCommittee(rng, 4)
	return keys
}
