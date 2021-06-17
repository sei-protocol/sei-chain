package v100

import (
	ics23 "github.com/confio/ics23/go"
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	cryptotypes "github.com/cosmos/cosmos-sdk/crypto/types"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/cosmos/ibc-go/modules/core/exported"
)

// NOTE: this is a mock implmentation for exported.ClientState. This implementation
// should only be registered on the InterfaceRegistry during cli command genesis migration.
// This implementation is only used to successfully unmarshal the previous solo machine
// client state and consensus state and migrate them to the new implementations. When the proto
// codec unmarshals, it calls UnpackInterfaces() to create a cached value of the any. The
// UnpackInterfaces function for IdenitifiedClientState will attempt to unpack the any to
// exported.ClientState. If the solomachine v1 type is not registered against the exported.ClientState
// the unmarshal will fail. This implementation will panic on every interface function.
// The same is done for the ConsensusState.

// Interface implementation checks.
var (
	_, _ codectypes.UnpackInterfacesMessage = &ClientState{}, &ConsensusState{}
	_    exported.ClientState               = (*ClientState)(nil)
	_    exported.ConsensusState            = &ConsensusState{}
)

func RegisterInterfaces(registry codectypes.InterfaceRegistry) {
	registry.RegisterImplementations(
		(*exported.ClientState)(nil),
		&ClientState{},
	)
	registry.RegisterImplementations(
		(*exported.ConsensusState)(nil),
		&ConsensusState{},
	)
}

// UnpackInterfaces implements the UnpackInterfaceMessages.UnpackInterfaces method
func (cs ClientState) UnpackInterfaces(unpacker codectypes.AnyUnpacker) error {
	return cs.ConsensusState.UnpackInterfaces(unpacker)
}

// UnpackInterfaces implements the UnpackInterfaceMessages.UnpackInterfaces method
func (cs ConsensusState) UnpackInterfaces(unpacker codectypes.AnyUnpacker) error {
	return unpacker.UnpackAny(cs.PublicKey, new(cryptotypes.PubKey))
}

// ClientType panics!
func (cs ClientState) ClientType() string {
	panic("legacy solo machine is deprecated!")
}

// GetLatestHeight panics!
func (cs ClientState) GetLatestHeight() exported.Height {
	panic("legacy solo machine is deprecated!")
}

// Status panics!
func (cs ClientState) Status(_ sdk.Context, _ sdk.KVStore, _ codec.BinaryCodec) exported.Status {
	panic("legacy solo machine is deprecated!")
}

// Validate panics!
func (cs ClientState) Validate() error {
	panic("legacy solo machine is deprecated!")
}

// GetProofSpecs panics!
func (cs ClientState) GetProofSpecs() []*ics23.ProofSpec {
	panic("legacy solo machine is deprecated!")
}

// ZeroCustomFields panics!
func (cs ClientState) ZeroCustomFields() exported.ClientState {
	panic("legacy solo machine is deprecated!")
}

// Initialize panics!
func (cs ClientState) Initialize(_ sdk.Context, _ codec.BinaryCodec, _ sdk.KVStore, consState exported.ConsensusState) error {
	panic("legacy solo machine is deprecated!")
}

// ExportMetadata panics!
func (cs ClientState) ExportMetadata(_ sdk.KVStore) []exported.GenesisMetadata {
	panic("legacy solo machine is deprecated!")
}

// CheckHeaderAndUpdateState panics!
func (cs *ClientState) CheckHeaderAndUpdateState(
	_ sdk.Context, _ codec.BinaryCodec, _ sdk.KVStore, _ exported.Header,
) (exported.ClientState, exported.ConsensusState, error) {
	panic("legacy solo machine is deprecated!")
}

// CheckMisbehaviourAndUpdateState panics!
func (cs ClientState) CheckMisbehaviourAndUpdateState(
	_ sdk.Context, _ codec.BinaryCodec, _ sdk.KVStore, _ exported.Misbehaviour,
) (exported.ClientState, error) {
	panic("legacy solo machine is deprecated!")
}

// CheckSubstituteAndUpdateState panics!
func (cs ClientState) CheckSubstituteAndUpdateState(
	ctx sdk.Context, _ codec.BinaryCodec, _, _ sdk.KVStore,
	_ exported.ClientState,
) (exported.ClientState, error) {
	panic("legacy solo machine is deprecated!")
}

// VerifyUpgradeAndUpdateState panics!
func (cs ClientState) VerifyUpgradeAndUpdateState(
	_ sdk.Context, _ codec.BinaryCodec, _ sdk.KVStore,
	_ exported.ClientState, _ exported.ConsensusState, _, _ []byte,
) (exported.ClientState, exported.ConsensusState, error) {
	panic("legacy solo machine is deprecated!")
}

// VerifyClientState panics!
func (cs ClientState) VerifyClientState(
	store sdk.KVStore, cdc codec.BinaryCodec,
	_ exported.Height, _ exported.Prefix, _ string, _ []byte, clientState exported.ClientState,
) error {
	panic("legacy solo machine is deprecated!")
}

// VerifyClientConsensusState panics!
func (cs ClientState) VerifyClientConsensusState(
	sdk.KVStore, codec.BinaryCodec,
	exported.Height, string, exported.Height, exported.Prefix,
	[]byte, exported.ConsensusState,
) error {
	panic("legacy solo machine is deprecated!")
}

// VerifyConnectionState panics!
func (cs ClientState) VerifyConnectionState(
	sdk.KVStore, codec.BinaryCodec, exported.Height,
	exported.Prefix, []byte, string, exported.ConnectionI,
) error {
	panic("legacy solo machine is deprecated!")
}

// VerifyChannelState panics!
func (cs ClientState) VerifyChannelState(
	sdk.KVStore, codec.BinaryCodec, exported.Height, exported.Prefix,
	[]byte, string, string, exported.ChannelI,
) error {
	panic("legacy solo machine is deprecated!")
}

// VerifyPacketCommitment panics!
func (cs ClientState) VerifyPacketCommitment(
	sdk.Context, sdk.KVStore, codec.BinaryCodec, exported.Height,
	uint64, uint64, exported.Prefix, []byte,
	string, string, uint64, []byte,
) error {
	panic("legacy solo machine is deprecated!")
}

// VerifyPacketAcknowledgement panics!
func (cs ClientState) VerifyPacketAcknowledgement(
	sdk.Context, sdk.KVStore, codec.BinaryCodec, exported.Height,
	uint64, uint64, exported.Prefix, []byte,
	string, string, uint64, []byte,
) error {
	panic("legacy solo machine is deprecated!")
}

// VerifyPacketReceiptAbsence panics!
func (cs ClientState) VerifyPacketReceiptAbsence(
	sdk.Context, sdk.KVStore, codec.BinaryCodec, exported.Height,
	uint64, uint64, exported.Prefix, []byte,
	string, string, uint64,
) error {
	panic("legacy solo machine is deprecated!")
}

// VerifyNextSequenceRecv panics!
func (cs ClientState) VerifyNextSequenceRecv(
	sdk.Context, sdk.KVStore, codec.BinaryCodec, exported.Height,
	uint64, uint64, exported.Prefix, []byte,
	string, string, uint64,
) error {
	panic("legacy solo machine is deprecated!")
}

// ClientType panics!
func (ConsensusState) ClientType() string {
	panic("legacy solo machine is deprecated!")
}

// GetTimestamp panics!
func (cs ConsensusState) GetTimestamp() uint64 {
	panic("legacy solo machine is deprecated!")
}

// GetRoot panics!
func (cs ConsensusState) GetRoot() exported.Root {
	panic("legacy solo machine is deprecated!")
}

// ValidateBasic panics!
func (cs ConsensusState) ValidateBasic() error {
	panic("legacy solo machine is deprecated!")
}
