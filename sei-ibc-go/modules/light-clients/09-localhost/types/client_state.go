package types

import (
	"bytes"
	"encoding/binary"
	"reflect"
	"strings"

	ics23 "github.com/confio/ics23/go"

	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	clienttypes "github.com/cosmos/ibc-go/modules/core/02-client/types"
	connectiontypes "github.com/cosmos/ibc-go/modules/core/03-connection/types"
	channeltypes "github.com/cosmos/ibc-go/modules/core/04-channel/types"
	host "github.com/cosmos/ibc-go/modules/core/24-host"
	"github.com/cosmos/ibc-go/modules/core/exported"
)

var _ exported.ClientState = (*ClientState)(nil)

// NewClientState creates a new ClientState instance
func NewClientState(chainID string, height clienttypes.Height) *ClientState {
	return &ClientState{
		ChainId: chainID,
		Height:  height,
	}
}

// GetChainID returns an empty string
func (cs ClientState) GetChainID() string {
	return cs.ChainId
}

// ClientType is localhost.
func (cs ClientState) ClientType() string {
	return exported.Localhost
}

// GetLatestHeight returns the latest height stored.
func (cs ClientState) GetLatestHeight() exported.Height {
	return cs.Height
}

// Status always returns Active. The localhost status cannot be changed.
func (cs ClientState) Status(_ sdk.Context, _ sdk.KVStore, _ codec.BinaryCodec,
) exported.Status {
	return exported.Active
}

// Validate performs a basic validation of the client state fields.
func (cs ClientState) Validate() error {
	if strings.TrimSpace(cs.ChainId) == "" {
		return sdkerrors.Wrap(sdkerrors.ErrInvalidChainID, "chain id cannot be blank")
	}
	if cs.Height.RevisionHeight == 0 {
		return sdkerrors.Wrapf(sdkerrors.ErrInvalidHeight, "local revision height cannot be zero")
	}
	return nil
}

// GetProofSpecs returns nil since localhost does not have to verify proofs
func (cs ClientState) GetProofSpecs() []*ics23.ProofSpec {
	return nil
}

// ZeroCustomFields returns the same client state since there are no custom fields in localhost
func (cs ClientState) ZeroCustomFields() exported.ClientState {
	return &cs
}

// Initialize ensures that initial consensus state for localhost is nil
func (cs ClientState) Initialize(_ sdk.Context, _ codec.BinaryCodec, _ sdk.KVStore, consState exported.ConsensusState) error {
	if consState != nil {
		return sdkerrors.Wrap(clienttypes.ErrInvalidConsensus, "initial consensus state for localhost must be nil.")
	}
	return nil
}

// ExportMetadata is a no-op for localhost client
func (cs ClientState) ExportMetadata(_ sdk.KVStore) []exported.GenesisMetadata {
	return nil
}

// CheckHeaderAndUpdateState updates the localhost client. It only needs access to the context
func (cs *ClientState) CheckHeaderAndUpdateState(
	ctx sdk.Context, _ codec.BinaryCodec, _ sdk.KVStore, _ exported.Header,
) (exported.ClientState, exported.ConsensusState, error) {
	// use the chain ID from context since the localhost client is from the running chain (i.e self).
	cs.ChainId = ctx.ChainID()
	revision := clienttypes.ParseChainID(cs.ChainId)
	cs.Height = clienttypes.NewHeight(revision, uint64(ctx.BlockHeight()))
	return cs, nil, nil
}

// CheckMisbehaviourAndUpdateState implements ClientState
// Since localhost is the client of the running chain, misbehaviour cannot be submitted to it
// Thus, CheckMisbehaviourAndUpdateState returns an error for localhost
func (cs ClientState) CheckMisbehaviourAndUpdateState(
	_ sdk.Context, _ codec.BinaryCodec, _ sdk.KVStore, _ exported.Misbehaviour,
) (exported.ClientState, error) {
	return nil, sdkerrors.Wrap(clienttypes.ErrInvalidMisbehaviour, "cannot submit misbehaviour to localhost client")
}

// CheckSubstituteAndUpdateState returns an error. The localhost cannot be modified by
// proposals.
func (cs ClientState) CheckSubstituteAndUpdateState(
	ctx sdk.Context, _ codec.BinaryCodec, _, _ sdk.KVStore,
	_ exported.ClientState,
) (exported.ClientState, error) {
	return nil, sdkerrors.Wrap(clienttypes.ErrUpdateClientFailed, "cannot update localhost client with a proposal")
}

// VerifyUpgradeAndUpdateState returns an error since localhost cannot be upgraded
func (cs ClientState) VerifyUpgradeAndUpdateState(
	_ sdk.Context, _ codec.BinaryCodec, _ sdk.KVStore,
	_ exported.ClientState, _ exported.ConsensusState, _, _ []byte,
) (exported.ClientState, exported.ConsensusState, error) {
	return nil, nil, sdkerrors.Wrap(clienttypes.ErrInvalidUpgradeClient, "cannot upgrade localhost client")
}

// VerifyClientState verifies that the localhost client state is stored locally
func (cs ClientState) VerifyClientState(
	store sdk.KVStore, cdc codec.BinaryCodec,
	_ exported.Height, _ exported.Prefix, _ string, _ []byte, clientState exported.ClientState,
) error {
	path := host.KeyClientState
	bz := store.Get([]byte(path))
	if bz == nil {
		return sdkerrors.Wrapf(clienttypes.ErrFailedClientStateVerification,
			"not found for path: %s", path)
	}

	selfClient := clienttypes.MustUnmarshalClientState(cdc, bz)

	if !reflect.DeepEqual(selfClient, clientState) {
		return sdkerrors.Wrapf(clienttypes.ErrFailedClientStateVerification,
			"stored clientState != provided clientState: \n%v\n≠\n%v",
			selfClient, clientState,
		)
	}
	return nil
}

// VerifyClientConsensusState returns nil since a local host client does not store consensus
// states.
func (cs ClientState) VerifyClientConsensusState(
	sdk.KVStore, codec.BinaryCodec,
	exported.Height, string, exported.Height, exported.Prefix,
	[]byte, exported.ConsensusState,
) error {
	return nil
}

// VerifyConnectionState verifies a proof of the connection state of the
// specified connection end stored locally.
func (cs ClientState) VerifyConnectionState(
	store sdk.KVStore,
	cdc codec.BinaryCodec,
	_ exported.Height,
	_ exported.Prefix,
	_ []byte,
	connectionID string,
	connectionEnd exported.ConnectionI,
) error {
	path := host.ConnectionKey(connectionID)
	bz := store.Get(path)
	if bz == nil {
		return sdkerrors.Wrapf(clienttypes.ErrFailedConnectionStateVerification, "not found for path %s", path)
	}

	var prevConnection connectiontypes.ConnectionEnd
	err := cdc.Unmarshal(bz, &prevConnection)
	if err != nil {
		return err
	}

	if !reflect.DeepEqual(&prevConnection, connectionEnd) {
		return sdkerrors.Wrapf(
			clienttypes.ErrFailedConnectionStateVerification,
			"connection end ≠ previous stored connection: \n%v\n≠\n%v", connectionEnd, prevConnection,
		)
	}

	return nil
}

// VerifyChannelState verifies a proof of the channel state of the specified
// channel end, under the specified port, stored on the local machine.
func (cs ClientState) VerifyChannelState(
	store sdk.KVStore,
	cdc codec.BinaryCodec,
	_ exported.Height,
	prefix exported.Prefix,
	_ []byte,
	portID,
	channelID string,
	channel exported.ChannelI,
) error {
	path := host.ChannelKey(portID, channelID)
	bz := store.Get(path)
	if bz == nil {
		return sdkerrors.Wrapf(clienttypes.ErrFailedChannelStateVerification, "not found for path %s", path)
	}

	var prevChannel channeltypes.Channel
	err := cdc.Unmarshal(bz, &prevChannel)
	if err != nil {
		return err
	}

	if !reflect.DeepEqual(&prevChannel, channel) {
		return sdkerrors.Wrapf(
			clienttypes.ErrFailedChannelStateVerification,
			"channel end ≠ previous stored channel: \n%v\n≠\n%v", channel, prevChannel,
		)
	}

	return nil
}

// VerifyPacketCommitment verifies a proof of an outgoing packet commitment at
// the specified port, specified channel, and specified sequence.
func (cs ClientState) VerifyPacketCommitment(
	ctx sdk.Context,
	store sdk.KVStore,
	_ codec.BinaryCodec,
	_ exported.Height,
	_ uint64,
	_ uint64,
	_ exported.Prefix,
	_ []byte,
	portID,
	channelID string,
	sequence uint64,
	commitmentBytes []byte,
) error {
	path := host.PacketCommitmentKey(portID, channelID, sequence)

	data := store.Get(path)
	if len(data) == 0 {
		return sdkerrors.Wrapf(clienttypes.ErrFailedPacketCommitmentVerification, "not found for path %s", path)
	}

	if !bytes.Equal(data, commitmentBytes) {
		return sdkerrors.Wrapf(
			clienttypes.ErrFailedPacketCommitmentVerification,
			"commitment ≠ previous commitment: \n%X\n≠\n%X", commitmentBytes, data,
		)
	}

	return nil
}

// VerifyPacketAcknowledgement verifies a proof of an incoming packet
// acknowledgement at the specified port, specified channel, and specified sequence.
func (cs ClientState) VerifyPacketAcknowledgement(
	ctx sdk.Context,
	store sdk.KVStore,
	_ codec.BinaryCodec,
	_ exported.Height,
	_ uint64,
	_ uint64,
	_ exported.Prefix,
	_ []byte,
	portID,
	channelID string,
	sequence uint64,
	acknowledgement []byte,
) error {
	path := host.PacketAcknowledgementKey(portID, channelID, sequence)

	data := store.Get(path)
	if len(data) == 0 {
		return sdkerrors.Wrapf(clienttypes.ErrFailedPacketAckVerification, "not found for path %s", path)
	}

	if !bytes.Equal(data, acknowledgement) {
		return sdkerrors.Wrapf(
			clienttypes.ErrFailedPacketAckVerification,
			"ak bytes ≠ previous ack: \n%X\n≠\n%X", acknowledgement, data,
		)
	}

	return nil
}

// VerifyPacketReceiptAbsence verifies a proof of the absence of an
// incoming packet receipt at the specified port, specified channel, and
// specified sequence.
func (cs ClientState) VerifyPacketReceiptAbsence(
	ctx sdk.Context,
	store sdk.KVStore,
	_ codec.BinaryCodec,
	_ exported.Height,
	_ uint64,
	_ uint64,
	_ exported.Prefix,
	_ []byte,
	portID,
	channelID string,
	sequence uint64,
) error {
	path := host.PacketReceiptKey(portID, channelID, sequence)

	data := store.Get(path)
	if data != nil {
		return sdkerrors.Wrap(clienttypes.ErrFailedPacketReceiptVerification, "expected no packet receipt")
	}

	return nil
}

// VerifyNextSequenceRecv verifies a proof of the next sequence number to be
// received of the specified channel at the specified port.
func (cs ClientState) VerifyNextSequenceRecv(
	ctx sdk.Context,
	store sdk.KVStore,
	_ codec.BinaryCodec,
	_ exported.Height,
	_ uint64,
	_ uint64,
	_ exported.Prefix,
	_ []byte,
	portID,
	channelID string,
	nextSequenceRecv uint64,
) error {
	path := host.NextSequenceRecvKey(portID, channelID)

	data := store.Get(path)
	if len(data) == 0 {
		return sdkerrors.Wrapf(clienttypes.ErrFailedNextSeqRecvVerification, "not found for path %s", path)
	}

	prevSequenceRecv := binary.BigEndian.Uint64(data)
	if prevSequenceRecv != nextSequenceRecv {
		return sdkerrors.Wrapf(
			clienttypes.ErrFailedNextSeqRecvVerification,
			"next sequence receive ≠ previous stored sequence (%d ≠ %d)", nextSequenceRecv, prevSequenceRecv,
		)
	}

	return nil
}
