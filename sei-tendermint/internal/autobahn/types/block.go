package types

import (
	"fmt"
	"math/big"
	"time"

	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"

	"github.com/sei-protocol/sei-stream/pkg/utils"
	"github.com/tendermint/tendermint/internal/autobahn/pkg/protocol"
)

// LaneID represents a lane identifier (currently it is the same as NodeID,
// since the producer uniquely identifies the lane).
type LaneID = PublicKey

// NodeID represents a unique identifier for a node in the network.
type NodeID string

// BlockNumber is the number of a block in a lane.
type BlockNumber uint64

// GlobalBlockNumber is the number of a block in the global chain.
type GlobalBlockNumber uint64

// BlockHeaderHash is the hash of a BlockHeader.
type BlockHeaderHash utils.Hash

// Bytes converts the BlockHeaderHash to a byte slice.
func (h BlockHeaderHash) Bytes() []byte { return h[:] }

// BlockHeader .
type BlockHeader struct {
	utils.ReadOnly
	lane        LaneID
	blockNumber BlockNumber
	parentHash  BlockHeaderHash
	payloadHash PayloadHash
}

// Lane .
func (h *BlockHeader) Lane() LaneID { return h.lane }

// BlockNumber .
func (h *BlockHeader) BlockNumber() BlockNumber { return h.blockNumber }

// ParentHash .
func (h *BlockHeader) ParentHash() BlockHeaderHash { return h.parentHash }

// PayloadHash .
func (h *BlockHeader) PayloadHash() PayloadHash { return h.payloadHash }

// Next return the block number of the next header.
func (h *BlockHeader) Next() BlockNumber { return h.blockNumber + 1 }

// Verify verifies the BlockHeader against the committee.
func (h *BlockHeader) Verify(c *Committee) error {
	if !c.Lanes().Has(h.lane) {
		return fmt.Errorf("%q is not a lane", h.lane)
	}
	return nil
}

// Block .
type Block struct {
	utils.ReadOnly
	header  *BlockHeader
	payload *Payload
}

// GlobalBlock is a finalized block with global block number.
type GlobalBlock struct {
	Header       *BlockHeader
	GlobalNumber GlobalBlockNumber
	Payload      *Payload
	// Highest known finalized state.
	FinalAppState utils.Option[*AppProposal]
}

// NewBlock creates a new Block.
func NewBlock(
	lane LaneID,
	blockNumber BlockNumber,
	parentHash BlockHeaderHash,
	payload *Payload,
) *Block {
	return &Block{
		header: &BlockHeader{
			lane:        lane,
			blockNumber: blockNumber,
			parentHash:  parentHash,
			payloadHash: payload.Hash(),
		},
		payload: payload,
	}
}

// Header .
func (b *Block) Header() *BlockHeader { return b.header }

// Payload .
func (b *Block) Payload() *Payload { return b.payload }

// Verify validates the Block.
func (b *Block) Verify(c *Committee) error {
	if err := b.Header().Verify(c); err != nil {
		return fmt.Errorf("header.Verify(): %w", err)
	}
	if got, want := b.Payload().Hash(), b.Header().PayloadHash(); got != want {
		return fmt.Errorf("payload.Hash() = %v, want %v", got, want)
	}
	return nil
}

// Hash of the BlockHeader.
func (h *BlockHeader) Hash() BlockHeaderHash {
	return BlockHeaderHash(utils.ProtoHash(BlockHeaderConv.Encode(h)))
}

// PayloadHash is the hash of a Payload.
type PayloadHash utils.Hash

// PayloadBuilder builds a Payload.
type PayloadBuilder struct {
	CreatedAt time.Time
	TotalGas  uint64
	EdgeCount int64
	Coinbase  []byte
	Basefee   int64
	Txs       [][]byte
}

// Payload .
type Payload struct {
	utils.ReadOnly
	p PayloadBuilder
}

// Build builds the Payload.
func (b PayloadBuilder) Build() *Payload { return &Payload{p: b} }

// ToBuilder converts the Payload to a PayloadBuilder.
func (p *Payload) ToBuilder() PayloadBuilder { return p.p }

// CreatedAt .
func (p *Payload) CreatedAt() time.Time { return p.p.CreatedAt }

// TotalGas .
func (p *Payload) TotalGas() uint64 { return p.p.TotalGas }

// EdgeCount .
func (p *Payload) EdgeCount() int64 { return p.p.EdgeCount }

// Coinbase .
func (p *Payload) Coinbase() []byte { return p.p.Coinbase }

// Basefee .
func (p *Payload) Basefee() int64 { return p.p.Basefee }

// Txs .
func (p *Payload) Txs() [][]byte { return p.p.Txs }

// Hash of the Payload.
func (p *Payload) Hash() PayloadHash {
	return PayloadHash(utils.ProtoHash(PayloadConv.Encode(p)))
}

// BlockHeaderConv is a protobuf converter for BlockHeader.
var BlockHeaderConv = utils.ProtoConv[*BlockHeader, *protocol.BlockHeader]{
	Encode: func(h *BlockHeader) *protocol.BlockHeader {
		return &protocol.BlockHeader{
			Lane:        PublicKeyConv.Encode(h.lane),
			BlockNumber: uint64(h.blockNumber),
			ParentHash:  h.parentHash[:],
			PayloadHash: h.payloadHash[:],
		}
	},
	Decode: func(h *protocol.BlockHeader) (*BlockHeader, error) {
		payloadHash, err := utils.ParseHash(h.PayloadHash)
		if err != nil {
			return nil, fmt.Errorf("payloadHash: %w", err)
		}
		parentHash, err := utils.ParseHash(h.ParentHash)
		if err != nil {
			return nil, fmt.Errorf("parentHash: %w", err)
		}
		lane, err := PublicKeyConv.DecodeReq(h.Lane)
		if err != nil {
			return nil, fmt.Errorf("lane: %w", err)
		}
		return &BlockHeader{
			lane:        lane,
			blockNumber: BlockNumber(h.BlockNumber),
			parentHash:  BlockHeaderHash(parentHash),
			payloadHash: PayloadHash(payloadHash),
		}, nil
	},
}

// PayloadConv is a protobuf converter for Payload.
var PayloadConv = utils.ProtoConv[*Payload, *protocol.Payload]{
	Encode: func(p *Payload) *protocol.Payload {
		return &protocol.Payload{
			CreatedAt: TimeConv.Encode(p.p.CreatedAt),
			TotalGas:  p.p.TotalGas,
			EdgeCount: p.p.EdgeCount,
			Coinbase:  p.p.Coinbase,
			Basefee:   p.p.Basefee,
			Txs:       p.p.Txs,
		}
	},
	Decode: func(p *protocol.Payload) (*Payload, error) {
		createdAt, err := TimeConv.DecodeReq(p.CreatedAt)
		if err != nil {
			return nil, fmt.Errorf("created_at: %w", err)
		}
		return PayloadBuilder{
			CreatedAt: createdAt,
			TotalGas:  p.TotalGas,
			EdgeCount: p.EdgeCount,
			Coinbase:  p.Coinbase,
			Basefee:   p.Basefee,
			Txs:       p.Txs,
		}.Build(), nil
	},
}

// BlockConv is a protobuf converter for Block.
var BlockConv = utils.ProtoConv[*Block, *protocol.Block]{
	Encode: func(b *Block) *protocol.Block {
		return &protocol.Block{
			Header:  BlockHeaderConv.Encode(b.header),
			Payload: PayloadConv.Encode(b.payload),
		}
	},
	Decode: func(b *protocol.Block) (*Block, error) {
		header, err := BlockHeaderConv.Decode(b.Header)
		if err != nil {
			return nil, err
		}
		payload, err := PayloadConv.Decode(b.Payload)
		if err != nil {
			return nil, err
		}
		return &Block{header: header, payload: payload}, nil
	},
}

// CalculateBlockHash calculates the hash of a block.
func (b *GlobalBlock) CalculateBlockHash() common.Hash {
	header := &ethtypes.Header{
		Time:       uint64(b.Payload.CreatedAt().Unix()),
		Number:     big.NewInt(int64(b.GlobalNumber)),
		GasUsed:    b.Payload.TotalGas(),
		Difficulty: big.NewInt(0),
		BaseFee:    big.NewInt(0),
	}
	return header.Hash()
}
