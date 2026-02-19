package types

import (
	"slices"
	"time"

	tmproto "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"
	version "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/version"
)

// ConsensusVersion duplicates tendermint's version.Consensus without proto baggage.
type ConsensusVersion struct {
	Block uint64
	App   uint64
}

// PartSetHeader duplicates tendermint's PartSetHeader.
type PartSetHeader struct {
	Total uint32
	Hash  []byte
}

// BlockID duplicates tendermint's BlockID.
type BlockID struct {
	Hash          []byte
	PartSetHeader PartSetHeader
}

// Header duplicates tendermint's Header for SDK context usage.
type Header struct {
	Version            ConsensusVersion
	ChainID            string
	Height             int64
	Time               time.Time
	LastBlockId        BlockID
	LastCommitHash     []byte
	DataHash           []byte
	ValidatorsHash     []byte
	NextValidatorsHash []byte
	ConsensusHash      []byte
	AppHash            []byte
	LastResultsHash    []byte
	EvidenceHash       []byte
	ProposerAddress    []byte
}

// Clone returns a deep copy of the header so callers cannot mutate shared state.
func (h Header) Clone() Header {
	clone := h
	clone.Version = h.Version
	clone.Time = h.Time
	clone.LastBlockId = h.LastBlockId.Clone()
	clone.LastCommitHash = slices.Clone(h.LastCommitHash)
	clone.DataHash = slices.Clone(h.DataHash)
	clone.ValidatorsHash = slices.Clone(h.ValidatorsHash)
	clone.NextValidatorsHash = slices.Clone(h.NextValidatorsHash)
	clone.ConsensusHash = slices.Clone(h.ConsensusHash)
	clone.AppHash = slices.Clone(h.AppHash)
	clone.LastResultsHash = slices.Clone(h.LastResultsHash)
	clone.EvidenceHash = slices.Clone(h.EvidenceHash)
	clone.ProposerAddress = slices.Clone(h.ProposerAddress)
	return clone
}

// Clone returns a deep copy of the block ID.
func (b BlockID) Clone() BlockID {
	clone := b
	clone.Hash = slices.Clone(b.Hash)
	clone.PartSetHeader = b.PartSetHeader.Clone()
	return clone
}

// Clone returns a deep copy of the part set header.
func (p PartSetHeader) Clone() PartSetHeader {
	clone := p
	clone.Hash = slices.Clone(p.Hash)
	return clone
}

// HeaderFromProto converts a Tendermint proto header into the SDK Header.
func HeaderFromProto(h tmproto.Header) Header {
	return Header{
		Version:            ConsensusVersion{Block: h.Version.Block, App: h.Version.App},
		ChainID:            h.ChainID,
		Height:             h.Height,
		Time:               h.Time,
		LastBlockId:        BlockIDFromProto(h.LastBlockId),
		LastCommitHash:     slices.Clone(h.LastCommitHash),
		DataHash:           slices.Clone(h.DataHash),
		ValidatorsHash:     slices.Clone(h.ValidatorsHash),
		NextValidatorsHash: slices.Clone(h.NextValidatorsHash),
		ConsensusHash:      slices.Clone(h.ConsensusHash),
		AppHash:            slices.Clone(h.AppHash),
		LastResultsHash:    slices.Clone(h.LastResultsHash),
		EvidenceHash:       slices.Clone(h.EvidenceHash),
		ProposerAddress:    slices.Clone(h.ProposerAddress),
	}
}

// BlockIDFromProto converts the proto block ID into the SDK BlockID.
func BlockIDFromProto(b tmproto.BlockID) BlockID {
	return BlockID{
		Hash: slices.Clone(b.Hash),
		PartSetHeader: PartSetHeader{
			Total: b.PartSetHeader.Total,
			Hash:  slices.Clone(b.PartSetHeader.Hash),
		},
	}
}

// ToProto converts the SDK Header back to the proto representation for Tendermint.
func (h Header) ToProto() tmproto.Header {
	return tmproto.Header{
		Version: version.Consensus{Block: h.Version.Block, App: h.Version.App},
		ChainID: h.ChainID,
		Height:  h.Height,
		Time:    h.Time,
		LastBlockId: tmproto.BlockID{
			Hash: slices.Clone(h.LastBlockId.Hash),
			PartSetHeader: tmproto.PartSetHeader{
				Total: h.LastBlockId.PartSetHeader.Total,
				Hash:  slices.Clone(h.LastBlockId.PartSetHeader.Hash),
			},
		},
		LastCommitHash:     slices.Clone(h.LastCommitHash),
		DataHash:           slices.Clone(h.DataHash),
		ValidatorsHash:     slices.Clone(h.ValidatorsHash),
		NextValidatorsHash: slices.Clone(h.NextValidatorsHash),
		ConsensusHash:      slices.Clone(h.ConsensusHash),
		AppHash:            slices.Clone(h.AppHash),
		LastResultsHash:    slices.Clone(h.LastResultsHash),
		EvidenceHash:       slices.Clone(h.EvidenceHash),
		ProposerAddress:    slices.Clone(h.ProposerAddress),
	}
}
