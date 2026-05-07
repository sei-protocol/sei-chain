package p2p

import (
	"encoding/binary"
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/block"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/pb"
	atypes "github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/protoutils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
)

// encodeBinaryBlock packs a finalized GlobalBlock into BlockDB's BinaryBlock
// shape. Layout of BlockData: [4 LE timestamp-proto length][timestamp proto]
// [block proto]. The block proto carries header + payload (txs included),
// matching the WAL format. Transactions is left nil — BlockData is fully
// self-describing for today's BlockByHash read path; we'll switch to indexed
// per-tx storage when GetTransactionByHash gets a real consumer.
func encodeBinaryBlock(gb *atypes.GlobalBlock) *block.BinaryBlock {
	tsBytes := atypes.TimeConv.Marshal(gb.Timestamp)
	blkBytes := protoutils.Marshal(&pb.Block{
		Header:  atypes.BlockHeaderConv.Encode(gb.Header),
		Payload: atypes.PayloadConv.Encode(gb.Payload),
	})
	out := make([]byte, 4+len(tsBytes)+len(blkBytes))
	binary.LittleEndian.PutUint32(out[:4], uint32(len(tsBytes))) //nolint:gosec // tsBytes is a small proto Timestamp.
	copy(out[4:], tsBytes)
	copy(out[4+len(tsBytes):], blkBytes)
	hash := gb.Header.Hash()
	return &block.BinaryBlock{
		Height:    uint64(gb.GlobalNumber),
		Hash:      hash.Bytes(),
		BlockData: out,
	}
}

// decodeBinaryBlock reconstructs a GlobalBlock from BlockDB's BinaryBlock
// shape produced by encodeBinaryBlock. FinalAppState is left None — the
// BlockByHash read path doesn't read it (translateGlobalBlock ignores it),
// and we don't want to wire AppProposal serialization through BlockDB until
// there's a consumer.
func decodeBinaryBlock(bb *block.BinaryBlock) (*atypes.GlobalBlock, error) {
	if len(bb.BlockData) < 4 {
		return nil, fmt.Errorf("block data too short: %d bytes", len(bb.BlockData))
	}
	tsLen := binary.LittleEndian.Uint32(bb.BlockData[:4])
	if uint64(len(bb.BlockData)) < uint64(4+tsLen) {
		return nil, fmt.Errorf("block data truncated: have %d, need >=%d", len(bb.BlockData), 4+tsLen)
	}
	ts, err := atypes.TimeConv.Unmarshal(bb.BlockData[4 : 4+tsLen])
	if err != nil {
		return nil, fmt.Errorf("decode timestamp: %w", err)
	}
	b, err := atypes.BlockConv.Unmarshal(bb.BlockData[4+tsLen:])
	if err != nil {
		return nil, fmt.Errorf("decode block: %w", err)
	}
	return &atypes.GlobalBlock{
		Header:        b.Header(),
		Timestamp:     ts,
		GlobalNumber:  atypes.GlobalBlockNumber(bb.Height),
		Payload:       b.Payload(),
		FinalAppState: utils.None[*atypes.AppProposal](),
	}, nil
}
