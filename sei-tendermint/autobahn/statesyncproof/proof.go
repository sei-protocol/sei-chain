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
	block *types.RoadBlock
	appQC *types.AppQC
}

// Height returns the road height this state sync proof describes.
func (p *Proof) Height() types.RoadHeight {
	return p.block.QC().Height()
}

// RoadBlock returns the road block this state sync proof carries.
func (p *Proof) RoadBlock() *types.RoadBlock {
	return p.block
}

// AppQC returns the AppQC this state sync proof carries.
func (p *Proof) AppQC() *types.AppQC {
	return p.appQC
}

// FileName returns the canonical state sync proof file name for index.
func FileName(height types.RoadHeight) string {
	return fmt.Sprintf("%0*d%s", heightDigits, height, fileSuffix)
}

// ParseFileName parses a canonical state sync proof file name.
func ParseFileName(name string) (types.RoadHeight, error) {
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
	parsedHeight, err := strconv.ParseUint(height, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("state sync proof file height %q: %w", height, err)
	}
	return types.RoadHeight(parsedHeight), nil
}

// Encode encodes roadBlock and appQC as a state sync proof file. It fails when
// appQC is not for the road and epoch of roadBlock.
func Encode(block *types.RoadBlock, appQC *types.AppQC) ([]byte, error) {
	if block.QC().Height() != appQC.Proposal().Height() {
		return nil, fmt.Errorf("road block height %d does not match AppQC height %d",
			block.QC().Height(), appQC.Proposal().Height())
	}
	if err := appQC.Proposal().Verify(block.QC().QC()); err != nil {
		return nil, fmt.Errorf("AppQC does not match road block: %w", err)
	}
	return frame(&pb.StateSyncProof{
		CommitQc: types.FullCommitQCConv.Encode(block.QC()),
		Blocks:   types.LaneBlockConv.EncodeSlice(block.LaneBlocks()),
		AppQc:    types.AppQCConv.Encode(appQC),
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
func Decode(data []byte, wantHeight types.RoadHeight) (*Proof, error) {
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
	blocks, err := types.LaneBlockConv.DecodeSlice(msg.Blocks)
	if err != nil {
		return nil, fmt.Errorf("lane blocks: %w", err)
	}
	appQC, err := types.AppQCConv.DecodeReq(msg.AppQc)
	if err != nil {
		return nil, fmt.Errorf("AppQC: %w", err)
	}
	if qc.Height() != appQC.Proposal().Height() {
		return nil, fmt.Errorf("CommitQC height %d does not match AppQC height %d", qc.Height(), appQC.Proposal().Height())
	}
	if err := appQC.Proposal().Verify(qc.QC()); err != nil {
		return nil, fmt.Errorf("AppQC does not match CommitQC: %w", err)
	}
	if qc.Height() != wantHeight {
		return nil, fmt.Errorf("state sync proof index %d, want %d", qc.Height(), wantHeight)
	}
	return &Proof{
		block: types.NewRoadBlock(qc, blocks),
		appQC: appQC,
	}, nil
}
