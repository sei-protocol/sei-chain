package types

import (
	"github.com/gogo/protobuf/proto"
)

// DefaultGenesis returns the default genesis state
func DefaultGenesis() *GenesisState {
	return &GenesisState{}
}

// GenesisState defines the mev module's genesis state
type GenesisState struct {
	// Add your genesis state fields here
}

// implement proto.Message interface
func (m *GenesisState) Reset()         { *m = GenesisState{} }
func (m *GenesisState) String() string { return proto.CompactTextString(m) }
func (*GenesisState) ProtoMessage()    {}
