package statesyncproof

import (
	"path/filepath"
	"testing"

	"google.golang.org/protobuf/encoding/protowire"

	"github.com/sei-protocol/sei-chain/giga/apphash"
	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/data"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/pb"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/protoutils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
)

func TestEncodeDecodeRoundTrip(t *testing.T) {
	block, appQC, record := testProof(t, utils.TestRng())

	encoded, err := Encode(block, appQC, record)
	require.NoError(t, err)

	decoded, err := Decode(encoded, block.QC().Height())
	require.NoError(t, err)
	require.Equal(t, block.QC().Height(), decoded.Height())
	require.Equal(t, block.Header().Hash(), decoded.RoadBlock().Header().Hash())
	require.Equal(t, len(block.LaneBlocks()), len(decoded.RoadBlock().LaneBlocks()))
	require.Equal(t, appQC.Proposal().Height(), decoded.AppQC().Proposal().Height())
	require.Equal(t, []byte(appQC.Proposal().AppHash()), []byte(decoded.AppQC().Proposal().AppHash()))
	require.Equal(t, record, decoded.AppHashData())
}

func TestEncodeForTestingDecodesAtItsHeight(t *testing.T) {
	encoded, err := EncodeForTesting(utils.TestRng(), 7)
	require.NoError(t, err)

	decoded, err := Decode(encoded, 7)
	require.NoError(t, err)
	require.Equal(t, types.RoadHeight(7), decoded.Height())
	require.Equal(t, uint64(7), decoded.AppHashData().BlockHeight())
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
	block, appQC, record := testProof(t, utils.TestRng())
	encoded, err := Encode(block, appQC, record)
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
			block, _, _ := testProof(t, rng)
			view := otherView(block.QC().QC().View())
			record := genAppHashData(rng, view.Height)

			_, err := Decode(testFile(t, block, testAppQC(testKeys(t, rng), view, record), record), block.QC().Height())
			require.Error(t, err)
		})
	}
}

func TestDecodeRejectsUnexpectedHeight(t *testing.T) {
	block, appQC, record := testProof(t, utils.TestRng())
	encoded, err := Encode(block, appQC, record)
	require.NoError(t, err)

	_, err = Decode(encoded, block.QC().Height()+1)
	require.Error(t, err)
}

func TestDecodeRejectsMissingAppHashData(t *testing.T) {
	block, appQC, _ := testProof(t, utils.TestRng())
	encoded := frame(&pb.StateSyncProof{
		CommitQc: types.FullCommitQCConv.Encode(block.QC()),
		Blocks:   types.LaneBlockConv.EncodeSlice(block.LaneBlocks()),
		AppQc:    types.AppQCConv.Encode(appQC),
	})

	_, err := Decode(encoded, block.QC().Height())
	require.Error(t, err)
}

func TestDecodeRejectsMalformedAppHashData(t *testing.T) {
	block, appQC, record := testProof(t, utils.TestRng())
	recordBytes, err := record.Serialize()
	require.NoError(t, err)
	cases := map[string][]byte{
		"truncated":       recordBytes[:len(recordBytes)-1],
		"trailing byte":   append(append([]byte{}, recordBytes...), 0),
		"unknown version": append([]byte{9}, recordBytes[1:]...),
	}
	for name, input := range cases {
		t.Run(name, func(t *testing.T) {
			encoded := frame(&pb.StateSyncProof{
				CommitQc:    types.FullCommitQCConv.Encode(block.QC()),
				Blocks:      types.LaneBlockConv.EncodeSlice(block.LaneBlocks()),
				AppQc:       types.AppQCConv.Encode(appQC),
				AppHashData: input,
			})
			_, err := Decode(encoded, block.QC().Height())
			require.Error(t, err)
		})
	}
}

func TestDecodeRejectsAppHashDataTheAppQCDoesNotSign(t *testing.T) {
	rng := utils.TestRng()
	block, _, _ := testProof(t, rng)
	for name, c := range mismatchedAppHashData(t, rng, block) {
		t.Run(name, func(t *testing.T) {
			_, err := Decode(testFile(t, block, c.appQC, c.record), block.QC().Height())
			require.Error(t, err)
		})
	}
}

func TestEncodeRejectsAppQCOfAnotherView(t *testing.T) {
	for name, otherView := range mismatchedViews() {
		t.Run(name, func(t *testing.T) {
			rng := utils.TestRng()
			block, _, _ := testProof(t, rng)
			view := otherView(block.QC().QC().View())
			record := genAppHashData(rng, view.Height)

			_, err := Encode(block, testAppQC(testKeys(t, rng), view, record), record)
			require.Error(t, err)
		})
	}
}

func TestEncodeRejectsAppHashDataTheAppQCDoesNotSign(t *testing.T) {
	rng := utils.TestRng()
	block, _, _ := testProof(t, rng)
	for name, c := range mismatchedAppHashData(t, rng, block) {
		t.Run(name, func(t *testing.T) {
			_, err := Encode(block, c.appQC, c.record)
			require.Error(t, err)
		})
	}
}

func TestEncodeRejectsZeroAppHashData(t *testing.T) {
	rng := utils.TestRng()
	block, _, _ := testProof(t, rng)
	view := block.QC().QC().View()
	record := apphash.NewZeroAppHashData(rng.Uint64(), uint64(view.Height)+1)

	_, err := Encode(block, testAppQC(testKeys(t, rng), view, record), record)
	require.Error(t, err)
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

// signedAppHashData is an AppQC and an app hash record to encode with a road block.
type signedAppHashData struct {
	appQC  *types.AppQC
	record *apphash.AppHashData
}

// mismatchedAppHashData returns, by case name, an AppQC of the road of block and an app hash
// record that is not the record of that road whose app hash the AppQC signs.
func mismatchedAppHashData(t *testing.T, rng utils.Rng, block *types.RoadBlock) map[string]signedAppHashData {
	t.Helper()
	view := block.QC().QC().View()
	keys := testKeys(t, rng)
	anotherRoad := genAppHashData(rng, view.Height+1)
	return map[string]signedAppHashData{
		"record of another road": {testAppQC(keys, view, anotherRoad), anotherRoad},
		"app hash not signed":    {testAppQC(keys, view, genAppHashData(rng, view.Height)), genAppHashData(rng, view.Height)},
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

func testProof(t *testing.T, rng utils.Rng) (*types.RoadBlock, *types.AppQC, *apphash.AppHashData) {
	t.Helper()
	committee, keys := types.GenCommittee(rng, 4)
	epoch := types.GenEpochWithCommittee(rng, committee)
	qc, blocks := data.TestCommitQC(rng, epoch, keys, utils.None[*types.CommitQC]())
	record := genAppHashData(rng, qc.Height())
	return types.NewRoadBlock(qc, blocks), testAppQC(keys, qc.QC().View(), record), record
}

// testAppQC returns an AppQC of view, signed by keys, over the app hash of record.
func testAppQC(keys []types.SecretKey, view types.View, record *apphash.AppHashData) *types.AppQC {
	appHash := record.AppHash()
	return data.TestAppQC(keys, types.NewAppProposal(view, appHash[:]))
}

// testFile returns a state sync proof file of block, appQC and record without the checks of Encode.
func testFile(t *testing.T, block *types.RoadBlock, appQC *types.AppQC, record *apphash.AppHashData) []byte {
	t.Helper()
	recordBytes, err := record.Serialize()
	require.NoError(t, err)
	return frame(&pb.StateSyncProof{
		CommitQc:    types.FullCommitQCConv.Encode(block.QC()),
		Blocks:      types.LaneBlockConv.EncodeSlice(block.LaneBlocks()),
		AppQc:       types.AppQCConv.Encode(appQC),
		AppHashData: recordBytes,
	})
}

func testKeys(t *testing.T, rng utils.Rng) []types.SecretKey {
	t.Helper()
	_, keys := types.GenCommittee(rng, 4)
	return keys
}
