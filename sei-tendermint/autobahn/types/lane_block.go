package types

import (
	"encoding/hex"
	"fmt"
	"time"

	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/pb"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/hashable"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/protoutils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
)

// ExecutedBlocksWindow is the number of committed blocks ExecutedBlocks retains.
const ExecutedBlocksWindow = 128

// NodeID represents a unique identifier for a node in the network.
type NodeID string

// LaneHeight is the height of a block in a lane.
type LaneHeight uint64

// LaneBlockPos identifies a lane block by its CommitQC road index and header offset.
type LaneBlockPos struct {
	Height RoadHeight
	Offset uint64
}

// ExecutedBlock is a block committed to the app together with the gas its
// transactions used.
type ExecutedBlock struct {
	Height  RoadHeight
	GasUsed uint64
}

// ExecutedBlocks is an immutable window of the most recently committed blocks,
// consecutive by number and newest last. The zero value holds no blocks.
type ExecutedBlocks struct {
	blocks []ExecutedBlock
}

// NewExecutedBlocks returns a window holding only b.
func NewExecutedBlocks(b ExecutedBlock) ExecutedBlocks {
	return ExecutedBlocks{blocks: []ExecutedBlock{b}}
}

// Push returns a copy of w with b appended, dropping the oldest block once the
// window exceeds ExecutedBlocksWindow.
func (w ExecutedBlocks) Push(b ExecutedBlock) ExecutedBlocks {
	start := 0
	if len(w.blocks) >= ExecutedBlocksWindow {
		start = len(w.blocks) - ExecutedBlocksWindow + 1
	}
	blocks := make([]ExecutedBlock, 0, len(w.blocks)-start+1)
	blocks = append(blocks, w.blocks[start:]...)
	return ExecutedBlocks{blocks: append(blocks, b)}
}

// Latest returns the newest block in the window, or the zero ExecutedBlock if
// the window is empty.
func (w ExecutedBlocks) Latest() ExecutedBlock {
	if len(w.blocks) == 0 {
		return ExecutedBlock{}
	}
	return w.blocks[len(w.blocks)-1]
}

// Get returns the block numbered n if the window still holds it.
func (w ExecutedBlocks) Get(n RoadHeight) (ExecutedBlock, bool) {
	if len(w.blocks) == 0 {
		return ExecutedBlock{}, false
	}
	first := w.blocks[0].Height
	if n < first || n > w.Latest().Height {
		return ExecutedBlock{}, false
	}
	return w.blocks[n-first], true
}

// LaneBlockHeaderHash is the hash of a LaneBlockHeader.
type LaneBlockHeaderHash hashable.Hash[*pb.LaneBlockHeader]

// Bytes converts the LaneBlockHeaderHash to a byte slice.
func (h LaneBlockHeaderHash) Bytes() []byte  { return h[:] }
func (h LaneBlockHeaderHash) String() string { return hex.EncodeToString(h.Bytes()) }

func ParseLaneBlockHeaderHash(bytes []byte) (LaneBlockHeaderHash, error) {
	h, err := hashable.ParseHash[*pb.LaneBlockHeader](bytes)
	return LaneBlockHeaderHash(h), err
}

// LaneBlockHeader identifies a lane block and its parent and payload hashes.
type LaneBlockHeader struct {
	utils.ReadOnly
	lane        LaneID
	height      LaneHeight
	parentHash  LaneBlockHeaderHash
	payloadHash PayloadHash
}

// Lane .
func (h *LaneBlockHeader) Lane() LaneID { return h.lane }

// Height returns the height of the lane block.
func (h *LaneBlockHeader) Height() LaneHeight { return h.height }

// ParentHash .
func (h *LaneBlockHeader) ParentHash() LaneBlockHeaderHash { return h.parentHash }

// PayloadHash .
func (h *LaneBlockHeader) PayloadHash() PayloadHash { return h.payloadHash }

// Next returns the height of the next lane block.
func (h *LaneBlockHeader) Next() LaneHeight { return h.height + 1 }

// Verify verifies the LaneBlockHeader against the committee.
func (h *LaneBlockHeader) Verify(c *Committee) error {
	if !c.HasLane(h.lane) {
		return fmt.Errorf("%q is not a lane", h.lane)
	}
	return nil
}

const standardTxBytes uint64 = 1024

// Maximum number of transactions in a block.
const MaxTxsPerLaneBlock uint64 = 2000

// Maximum total size of all the transactions.
// It can be split arbitrarily across transactions (1 large, 2000 small ones, etc.)
// up to MaxTxsPerLaneBlock limit.
const MaxTxsBytesPerLaneBlock = MaxTxsPerLaneBlock * standardTxBytes

// Upper bound on the block proto encoding.
var MaxLaneBlockProtoSize = func() uint64 {
	// Payload.Txs represents the variable part of the LaneBlock size.
	// Proto size is maximized if we distribute data evenly across transactions.
	tx := make([]byte, standardTxBytes)
	txs := make([][]byte, MaxTxsPerLaneBlock)
	for i := range txs {
		txs[i] = tx
	}
	// Crude estimate of all other fields.
	const otherFields = 100 * 1024
	return otherFields + uint64(protoutils.Size(&pb.LaneBlock{Payload: &pb.Payload{Txs: txs}}))
}()

// LaneBlock contains a lane block header and payload.
type LaneBlock struct {
	utils.ReadOnly
	header  *LaneBlockHeader
	payload *Payload
}

// NewLaneBlock creates a new LaneBlock.
func NewLaneBlock(
	lane LaneID,
	height LaneHeight,
	parentHash LaneBlockHeaderHash,
	payload *Payload,
) *LaneBlock {
	return &LaneBlock{
		header: &LaneBlockHeader{
			lane:        lane,
			height:      height,
			parentHash:  parentHash,
			payloadHash: payload.Hash(),
		},
		payload: payload,
	}
}

// Header .
func (b *LaneBlock) Header() *LaneBlockHeader { return b.header }

// Payload .
func (b *LaneBlock) Payload() *Payload { return b.payload }

// Verify checks the block's internal integrity.
func (b *LaneBlock) Verify() error {
	if got, want := b.payload.Hash(), b.header.payloadHash; got != want {
		return fmt.Errorf("payload.Hash() = %v, want %v", got, want)
	}
	return nil
}

// Hash of the LaneBlockHeader.
func (h *LaneBlockHeader) Hash() LaneBlockHeaderHash {
	return LaneBlockHeaderHash(hashable.ToHash(LaneBlockHeaderConv.Encode(h)))
}

// PayloadHash is the hash of a Payload.
type PayloadHash hashable.Hash[*pb.Payload]

// ParsePayloadHash constructs a PayloadHash from its raw bytes.
func ParsePayloadHash(bytes []byte) (PayloadHash, error) {
	h, err := hashable.ParseHash[*pb.Payload](bytes)
	return PayloadHash(h), err
}

// PayloadBuilder builds a Payload.
type PayloadBuilder struct {
	CreatedAt         time.Time
	TotalGasWanted    uint64
	TotalGasEstimated uint64
	Txs               [][]byte
}

// Payload .
type Payload struct {
	utils.ReadOnly
	p PayloadBuilder
}

// Build builds the Payload.
func (b PayloadBuilder) Build() (*Payload, error) {
	if uint64(len(b.Txs)) > MaxTxsPerLaneBlock {
		return nil, fmt.Errorf("too many transactions")
	}
	total := uint64(0)
	for _, tx := range b.Txs {
		total += uint64(len(tx))
	}
	if total > MaxTxsBytesPerLaneBlock {
		return nil, fmt.Errorf("total txs bytes too large")
	}
	return &Payload{p: b}, nil
}

// ToBuilder converts the Payload to a PayloadBuilder.
func (p *Payload) ToBuilder() PayloadBuilder { return p.p }

// CreatedAt .
func (p *Payload) CreatedAt() time.Time { return p.p.CreatedAt }

// TotalGasWanted .
func (p *Payload) TotalGasWanted() uint64 { return p.p.TotalGasWanted }

// TotalGasEstimated .
func (p *Payload) TotalGasEstimated() uint64 { return p.p.TotalGasEstimated }

// Txs .
func (p *Payload) Txs() [][]byte { return p.p.Txs }

// Hash of the Payload.
func (p *Payload) Hash() PayloadHash {
	return PayloadHash(hashable.ToHash(PayloadConv.Encode(p)))
}

// LaneBlockHeaderConv is a protobuf converter for LaneBlockHeader.
var LaneBlockHeaderConv = protoutils.Conv[*LaneBlockHeader, *pb.LaneBlockHeader]{
	Encode: func(h *LaneBlockHeader) *pb.LaneBlockHeader {
		return &pb.LaneBlockHeader{
			LaneId:      LaneIDConv.Encode(h.lane),
			Height:      new(uint64(h.height)),
			ParentHash:  h.parentHash[:],
			PayloadHash: h.payloadHash[:],
		}
	},
	Decode: func(h *pb.LaneBlockHeader) (*LaneBlockHeader, error) {
		payloadHash, err := hashable.ParseHash[*pb.Payload](h.PayloadHash)
		if err != nil {
			return nil, fmt.Errorf("PayloadHash: %w", err)
		}
		parentHash, err := hashable.ParseHash[*pb.LaneBlockHeader](h.ParentHash)
		if err != nil {
			return nil, fmt.Errorf("ParentHash: %w", err)
		}
		lane, err := LaneIDConv.DecodeReq(h.LaneId)
		if err != nil {
			return nil, fmt.Errorf("lane: %w", err)
		}
		if h.Height == nil {
			return nil, fmt.Errorf("height: missing")
		}
		return &LaneBlockHeader{
			lane:        lane,
			height:      LaneHeight(*h.Height),
			parentHash:  LaneBlockHeaderHash(parentHash),
			payloadHash: PayloadHash(payloadHash),
		}, nil
	},
}

// PayloadConv is a protobuf converter for Payload.
var PayloadConv = protoutils.Conv[*Payload, *pb.Payload]{
	Encode: func(p *Payload) *pb.Payload {
		return &pb.Payload{
			CreatedAt:         TimeConv.Encode(p.p.CreatedAt),
			TotalGasWanted:    new(p.p.TotalGasWanted),
			TotalGasEstimated: new(p.p.TotalGasEstimated),
			Txs:               p.p.Txs,
		}
	},
	Decode: func(p *pb.Payload) (*Payload, error) {
		createdAt, err := TimeConv.DecodeReq(p.CreatedAt)
		if err != nil {
			return nil, fmt.Errorf("created_at: %w", err)
		}
		if p.TotalGasWanted == nil {
			return nil, fmt.Errorf("TotalGasWanted: missing")
		}
		if p.TotalGasEstimated == nil {
			return nil, fmt.Errorf("TotalGasEstimated: missing")
		}
		return PayloadBuilder{
			CreatedAt:         createdAt,
			TotalGasWanted:    *p.TotalGasWanted,
			TotalGasEstimated: *p.TotalGasEstimated,
			Txs:               p.Txs,
		}.Build()
	},
}

// LaneBlockConv is a protobuf converter for LaneBlock.
var LaneBlockConv = protoutils.Conv[*LaneBlock, *pb.LaneBlock]{
	Encode: func(b *LaneBlock) *pb.LaneBlock {
		return &pb.LaneBlock{
			Header:  LaneBlockHeaderConv.Encode(b.header),
			Payload: PayloadConv.Encode(b.payload),
		}
	},
	Decode: func(b *pb.LaneBlock) (*LaneBlock, error) {
		header, err := LaneBlockHeaderConv.DecodeReq(b.Header)
		if err != nil {
			return nil, err
		}
		payload, err := PayloadConv.DecodeReq(b.Payload)
		if err != nil {
			return nil, err
		}
		return &LaneBlock{header: header, payload: payload}, nil
	},
}

// LaneBlockPosConv is the protobuf converter for LaneBlockPos.
var LaneBlockPosConv = protoutils.Conv[LaneBlockPos, *pb.LaneBlockPos]{
	Encode: func(p LaneBlockPos) *pb.LaneBlockPos {
		return &pb.LaneBlockPos{Height: new(uint64(p.Height)), Offset: new(p.Offset)}
	},
	Decode: func(p *pb.LaneBlockPos) (LaneBlockPos, error) {
		if p.Height == nil || p.Offset == nil {
			return LaneBlockPos{}, fmt.Errorf("missing lane block position")
		}
		return LaneBlockPos{Height: RoadHeight(*p.Height), Offset: *p.Offset}, nil
	},
}
