package tmschemas

import (
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/protoutils/wireguard"
	bcproto "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/blocksync"
	tmcons "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/consensus"
	ssproto "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/statesync"
	tmproto "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"
)

// ValidateBlocksyncMessage is the PreDecode hook for the blocksync channel.
func ValidateBlocksyncMessage(bz []byte) error {
	return wireguard.Scan(bz, bcproto.SchemaForMessage)
}

// ValidateConsensusDataChannel is the PreDecode hook for the consensus
// DataChannel.
func ValidateConsensusDataChannel(bz []byte) error {
	return wireguard.Scan(bz, tmcons.SchemaForMessage)
}

// ValidateConsensusAssembledBlock scans the bytes reassembled from
// BlockPart messages before they are unmarshaled into a tmproto.Block.
func ValidateConsensusAssembledBlock(bz []byte) error {
	return wireguard.Scan(bz, tmproto.SchemaForBlock)
}

// ValidateEvidenceMessage is the PreDecode hook for the evidence channel.
func ValidateEvidenceMessage(bz []byte) error {
	return wireguard.Scan(bz, tmproto.SchemaForEvidence)
}

// ValidateStatesyncLightBlockChannel is the PreDecode hook for the
// statesync LightBlock channel.
func ValidateStatesyncLightBlockChannel(bz []byte) error {
	return wireguard.Scan(bz, ssproto.SchemaForMessage)
}
