package statesyncproof

import (
	"bytes"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/sei-protocol/sei-chain/giga/apphash"
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
	block       *types.RoadBlock
	appQC       *types.AppQC
	appHashData *apphash.AppHashData
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

// AppHashData returns the app hash record of the proof road, whose app hash the
// AppQC signs.
func (p *Proof) AppHashData() *apphash.AppHashData {
	return p.appHashData
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

// Encode encodes block, appQC and the app hash record of the road as a state
// sync proof file. It fails when appQC is not for the road and epoch of block,
// or when record is not the record of that road whose app hash appQC signs.
func Encode(block *types.RoadBlock, appQC *types.AppQC, record *apphash.AppHashData) ([]byte, error) {
	if block.QC().Height() != appQC.Proposal().Height() {
		return nil, fmt.Errorf("road block height %d does not match AppQC height %d",
			block.QC().Height(), appQC.Proposal().Height())
	}
	if err := appQC.Proposal().Verify(block.QC().QC()); err != nil {
		return nil, fmt.Errorf("AppQC does not match road block: %w", err)
	}
	if err := checkAppHashData(record, appQC); err != nil {
		return nil, err
	}
	recordBytes, err := record.Serialize()
	if err != nil {
		return nil, fmt.Errorf("app hash data: %w", err)
	}
	return frame(&pb.StateSyncProof{
		CommitQc:    types.FullCommitQCConv.Encode(block.QC()),
		Blocks:      types.LaneBlockConv.EncodeSlice(block.LaneBlocks()),
		AppQc:       types.AppQCConv.Encode(appQC),
		AppHashData: recordBytes,
	}), nil
}

// checkAppHashData checks that record is the app hash record of the road of
// appQC and that its app hash is the one appQC signs.
func checkAppHashData(record *apphash.AppHashData, appQC *types.AppQC) error {
	if got, want := record.BlockHeight(), uint64(appQC.Proposal().Height()); got != want {
		return fmt.Errorf("app hash data height %d does not match AppQC height %d", got, want)
	}
	if got, want := record.AppHash(), appQC.Proposal().AppHash(); !bytes.Equal(got[:], want) {
		return fmt.Errorf("app hash data hash %x does not match AppQC app hash %x", got, want)
	}
	return nil
}

// frame returns msg as a version 1 state sync proof file.
func frame(msg *pb.StateSyncProof) []byte {
	body := protoutils.Marshal(msg)
	data := make([]byte, 0, headerLen+len(body))
	data = append(data, fileMagic...)
	data = append(data, formatVersion)
	return append(data, body...)
}

// Decode decodes a state sync proof file for road wantHeight. It checks the
// file layout, that the AppQC is for the road and epoch of the CommitQC, and
// that the app hash record is for that road and hashes to the app hash the
// AppQC signs. It does not check signatures: the caller must verify the
// RoadBlock and the AppQC against the epoch before it trusts the proof.
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
	if len(msg.AppHashData) == 0 {
		return nil, fmt.Errorf("app hash data: missing")
	}
	record, err := apphash.Deserialize(msg.AppHashData)
	if err != nil {
		return nil, fmt.Errorf("app hash data: %w", err)
	}
	if err := checkAppHashData(record, appQC); err != nil {
		return nil, err
	}
	return &Proof{
		block:       types.NewRoadBlock(qc, blocks),
		appQC:       appQC,
		appHashData: record,
	}, nil
}
