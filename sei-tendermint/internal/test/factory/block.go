package factory

import (
	"time"

	"github.com/sei-protocol/sei-chain/sei-tendermint/crypto"
	"github.com/sei-protocol/sei-chain/sei-tendermint/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/version"
)

const (
	DefaultTestChainID = "test-chain"
)

var (
	DefaultTestTime = time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
)

func RandomAddress() []byte {
	return crypto.CRandBytes(crypto.AddressSize)
}

func RandomHash() []byte {
	return crypto.CRandBytes(crypto.HashSize)
}

func MakeBlockID() types.BlockID {
	return MakeBlockIDWithHash(RandomHash())
}

func MakeBlockIDWithHash(hash []byte) types.BlockID {
	return types.BlockID{
		Hash: hash,
		PartSetHeader: types.PartSetHeader{
			Total: 100,
			Hash:  RandomHash(),
		},
	}
}

// MakeHeader fills the rest of the contents of the header such that it passes
// validate basic
func MakeHeader(h *types.Header) *types.Header {
	if h.Version.Block == 0 {
		h.Version.Block = version.BlockProtocol
	}
	if h.Height == 0 {
		h.Height = 1
	}
	if h.LastBlockID.IsNil() {
		h.LastBlockID = MakeBlockID()
	}
	if h.ChainID == "" {
		h.ChainID = DefaultTestChainID
	}
	if len(h.LastCommitHash) == 0 {
		h.LastCommitHash = RandomHash()
	}
	if len(h.DataHash) == 0 {
		h.DataHash = RandomHash()
	}
	if len(h.ValidatorsHash) == 0 {
		h.ValidatorsHash = RandomHash()
	}
	if len(h.NextValidatorsHash) == 0 {
		h.NextValidatorsHash = RandomHash()
	}
	if len(h.ConsensusHash) == 0 {
		h.ConsensusHash = RandomHash()
	}
	if len(h.AppHash) == 0 {
		h.AppHash = RandomHash()
	}
	if len(h.LastResultsHash) == 0 {
		h.LastResultsHash = RandomHash()
	}
	if len(h.EvidenceHash) == 0 {
		h.EvidenceHash = RandomHash()
	}
	if len(h.ProposerAddress) == 0 {
		h.ProposerAddress = RandomAddress()
	}

	if err := h.ValidateBasic(); err != nil {
		panic(err)
	}
	return h
}
