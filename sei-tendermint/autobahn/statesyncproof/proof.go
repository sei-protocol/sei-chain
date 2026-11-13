package statesyncproof

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/pb"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/protoutils"
)

const (
	formatVersion byte = 1
	fileSuffix         = ".bin"
	heightDigits       = 20
	fileMagic          = "SEISSP"
	headerLen          = len(fileMagic) + 1
)

// Proof is a decoded state sync proof file.
type Proof struct {
	roadBlock *types.RoadBlock
	appQC     *types.AppQC
}

// Index returns the road index this state sync proof describes.
func (p *Proof) Index() types.RoadIndex {
	return p.roadBlock.QC().Index()
}

// RoadBlock returns the road block this state sync proof carries.
func (p *Proof) RoadBlock() *types.RoadBlock {
	return p.roadBlock
}

// AppQC returns the AppQC this state sync proof carries.
func (p *Proof) AppQC() *types.AppQC {
	return p.appQC
}

// FileName returns the canonical state sync proof file name for index.
func FileName(index types.RoadIndex) string {
	return fmt.Sprintf("%0*d%s", heightDigits, index, fileSuffix)
}

// ParseFileName parses a canonical state sync proof file name.
func ParseFileName(name string) (types.RoadIndex, error) {
	if filepath.Base(name) != name {
		return 0, fmt.Errorf("state sync proof file name %q must not include a path", name)
	}
	if !strings.HasSuffix(name, fileSuffix) {
		return 0, fmt.Errorf("state sync proof file name %q does not end with %q", name, fileSuffix)
	}
	height := strings.TrimSuffix(name, fileSuffix)
	if len(height) != heightDigits {
		return 0, fmt.Errorf("state sync proof file height %q has %d digits, want %d", height, len(height), heightDigits)
	}
	for _, c := range height {
		if c < '0' || c > '9' {
			return 0, fmt.Errorf("state sync proof file height %q is not decimal", height)
		}
	}
	index, err := strconv.ParseUint(height, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("state sync proof file height %q: %w", height, err)
	}
	return types.RoadIndex(index), nil
}

// Encode encodes roadBlock and appQC as a state sync proof file. It fails when
// appQC is not for the road and epoch of roadBlock.
func Encode(roadBlock *types.RoadBlock, appQC *types.AppQC) ([]byte, error) {
	if roadBlock.QC().Index() != appQC.Proposal().Index() {
		return nil, fmt.Errorf("road block index %d does not match AppQC index %d",
			roadBlock.QC().Index(), appQC.Proposal().Index())
	}
	if err := appQC.Proposal().Verify(roadBlock.QC().QC()); err != nil {
		return nil, fmt.Errorf("AppQC does not match road block: %w", err)
	}
	return frame(&pb.StateSyncProof{
		CommitQc:   types.FullCommitQCConv.Encode(roadBlock.QC()),
		LaneBlocks: types.BlockConv.EncodeSlice(roadBlock.LaneBlocks()),
		AppQc:      types.AppQCConv.Encode(appQC),
	}), nil
}

// frame returns msg as a version 1 state sync proof file.
func frame(msg *pb.StateSyncProof) []byte {
	body := protoutils.Marshal(msg)
	data := make([]byte, 0, headerLen+len(body))
	data = append(data, fileMagic...)
	data = append(data, formatVersion)
	return append(data, body...)
}

// Decode decodes a state sync proof file for road wantIndex. It checks the
// file layout and that the AppQC is for the road and epoch of the CommitQC, but
// not signatures: the caller must verify the RoadBlock and the AppQC against the
// epoch before it trusts the proof.
func Decode(data []byte, wantIndex types.RoadIndex) (*Proof, error) {
	if len(data) < headerLen {
		return nil, fmt.Errorf("truncated state sync proof file header")
	}
	if string(data[:len(fileMagic)]) != fileMagic {
		return nil, fmt.Errorf("invalid state sync proof file magic")
	}
	if data[len(fileMagic)] != formatVersion {
		return nil, fmt.Errorf("unsupported state sync proof file version %d, want %d", data[len(fileMagic)], formatVersion)
	}
	msg, err := protoutils.Unmarshal[*pb.StateSyncProof](data[headerLen:])
	if err != nil {
		return nil, fmt.Errorf("StateSyncProof: %w", err)
	}
	qc, err := types.FullCommitQCConv.DecodeReq(msg.CommitQc)
	if err != nil {
		return nil, fmt.Errorf("CommitQC: %w", err)
	}
	laneBlocks, err := types.BlockConv.DecodeSlice(msg.LaneBlocks)
	if err != nil {
		return nil, fmt.Errorf("lane blocks: %w", err)
	}
	appQC, err := types.AppQCConv.DecodeReq(msg.AppQc)
	if err != nil {
		return nil, fmt.Errorf("AppQC: %w", err)
	}
	if qc.Index() != appQC.Proposal().Index() {
		return nil, fmt.Errorf("CommitQC index %d does not match AppQC index %d", qc.Index(), appQC.Proposal().Index())
	}
	if err := appQC.Proposal().Verify(qc.QC()); err != nil {
		return nil, fmt.Errorf("AppQC does not match CommitQC: %w", err)
	}
	if qc.Index() != wantIndex {
		return nil, fmt.Errorf("state sync proof index %d, want %d", qc.Index(), wantIndex)
	}
	return &Proof{
		roadBlock: types.NewRoadBlock(qc, laneBlocks),
		appQC:     appQC,
	}, nil
}
