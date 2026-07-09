package types

import (
	"encoding/hex"
	"fmt"
	"math/big"
	"time"

	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"

	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/pb"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/hashable"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/protoutils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
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

// BlockWithNumber pairs a block with its GlobalBlockNumber. It is used as the
// payload of the utils.Option returned by ReadBlockByHash so that the block
// number is only present when the block itself is present.
type BlockWithNumber struct {
	Block  *Block
	Number GlobalBlockNumber
}

// BlockHeaderHash is the hash of a BlockHeader.
type BlockHeaderHash hashable.Hash[*pb.BlockHeader]

// Bytes converts the BlockHeaderHash to a byte slice.
func (h BlockHeaderHash) Bytes() []byte  { return h[:] }
func (h BlockHeaderHash) String() string { return hex.EncodeToString(h.Bytes()) }

func ParseBlockHeaderHash(bytes []byte) (BlockHeaderHash, error) {
	h, err := hashable.ParseHash[*pb.BlockHeader](bytes)
	return BlockHeaderHash(h), err
}

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
	if !c.HasLane(h.lane) {
		return fmt.Errorf("%q is not a lane", h.lane)
	}
	return nil
}

const standardTxBytes uint64 = 1024

// Maximum number of transactions in a block.
const MaxTxsPerBlock uint64 = 2000

// Maximum total size of all the transactions.
// It can be split arbitrarily across transactions (1 large, 2000 small ones, etc.)
// up to MaxTxsPerBlock limit.
const MaxTxsBytesPerBlock = MaxTxsPerBlock * standardTxBytes

// Upper bound on the block proto encoding.
var MaxBlockProtoSize = func() uint64 {
	// Payload.Txs represents the variable part of the Block size.
	// Proto size is maximized if we distribute data evenly across transactions.
	tx := make([]byte, standardTxBytes)
	txs := make([][]byte, MaxTxsPerBlock)
	for i := range txs {
		txs[i] = tx
	}
	// Crude estimate of all other fields.
	const otherFields = 100 * 1024
	return otherFields + uint64(protoutils.Size(&pb.Block{Payload: &pb.Payload{Txs: txs}}))
}()

// Block .
type Block struct {
	utils.ReadOnly
	header  *BlockHeader
	payload *Payload
}

// GlobalBlock is a finalized block with global block number.
type GlobalBlock struct {
	Header       *BlockHeader
	Timestamp    time.Time
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
	return BlockHeaderHash(hashable.ToHash(BlockHeaderConv.Encode(h)))
}

// PayloadHash is the hash of a Payload.
type PayloadHash hashable.Hash[*pb.Payload]

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
	if uint64(len(b.Txs)) > MaxTxsPerBlock {
		return nil, fmt.Errorf("too many transactions")
	}
	total := uint64(0)
	for _, tx := range b.Txs {
		total += uint64(len(tx))
	}
	if total > MaxTxsBytesPerBlock {
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

// BlockHeaderConv is a protobuf converter for BlockHeader.
var BlockHeaderConv = protoutils.Conv[*BlockHeader, *pb.BlockHeader]{
	Encode: func(h *BlockHeader) *pb.BlockHeader {
		return &pb.BlockHeader{
			Lane:        PublicKeyConv.Encode(h.lane),
			BlockNumber: utils.Alloc(uint64(h.blockNumber)),
			ParentHash:  h.parentHash[:],
			PayloadHash: h.payloadHash[:],
		}
	},
	Decode: func(h *pb.BlockHeader) (*BlockHeader, error) {
		payloadHash, err := hashable.ParseHash[*pb.Payload](h.PayloadHash)
		if err != nil {
			return nil, fmt.Errorf("PayloadHash: %w", err)
		}
		parentHash, err := hashable.ParseHash[*pb.BlockHeader](h.ParentHash)
		if err != nil {
			return nil, fmt.Errorf("ParentHash: %w", err)
		}
		lane, err := PublicKeyConv.DecodeReq(h.Lane)
		if err != nil {
			return nil, fmt.Errorf("lane: %w", err)
		}
		if h.BlockNumber == nil {
			return nil, fmt.Errorf("BlockNumber: missing")
		}
		return &BlockHeader{
			lane:        lane,
			blockNumber: BlockNumber(*h.BlockNumber),
			parentHash:  BlockHeaderHash(parentHash),
			payloadHash: PayloadHash(payloadHash),
		}, nil
	},
}

// PayloadConv is a protobuf converter for Payload.
var PayloadConv = protoutils.Conv[*Payload, *pb.Payload]{
	Encode: func(p *Payload) *pb.Payload {
		return &pb.Payload{
			CreatedAt:         TimeConv.Encode(p.p.CreatedAt),
			TotalGasWanted:    utils.Alloc(p.p.TotalGasWanted),
			TotalGasEstimated: utils.Alloc(p.p.TotalGasEstimated),
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

// BlockConv is a protobuf converter for Block.
var BlockConv = protoutils.Conv[*Block, *pb.Block]{
	Encode: func(b *Block) *pb.Block {
		return &pb.Block{
			Header:  BlockHeaderConv.Encode(b.header),
			Payload: PayloadConv.Encode(b.payload),
		}
	},
	Decode: func(b *pb.Block) (*Block, error) {
		header, err := BlockHeaderConv.DecodeReq(b.Header)
		if err != nil {
			return nil, err
		}
		payload, err := PayloadConv.DecodeReq(b.Payload)
		if err != nil {
			return nil, err
		}
		return &Block{header: header, payload: payload}, nil
	},
}

// CalculateBlockHash calculates the hash of a block.
func (b *GlobalBlock) CalculateBlockHash() common.Hash {
	header := &ethtypes.Header{
		Time:       uint64(b.Payload.CreatedAt().Unix()), //nolint:gosec // block timestamps are always positive post-epoch values
		Number:     big.NewInt(int64(b.GlobalNumber)),    //nolint:gosec // block numbers are within int64 range for all practical chain heights
		GasUsed:    b.Payload.TotalGasWanted(),
		Difficulty: big.NewInt(0),
		BaseFee:    big.NewInt(0),
	}
	return header.Hash()
}
