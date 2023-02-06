package ibctesting

import (
	"bytes"
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"
	channeltypes "github.com/cosmos/ibc-go/v3/modules/core/04-channel/types"
)

// Path contains two endpoints representing two chains connected over IBC
type Path struct {
	EndpointA *Endpoint
	EndpointB *Endpoint
}

// NewPath constructs an endpoint for each chain using the default values
// for the endpoints. Each endpoint is updated to have a pointer to the
// counterparty endpoint.
func NewPath(chainA, chainB *TestChain) *Path {
	endpointA := NewDefaultEndpoint(chainA)
	endpointB := NewDefaultEndpoint(chainB)

	endpointA.Counterparty = endpointB
	endpointB.Counterparty = endpointA

	return &Path{
		EndpointA: endpointA,
		EndpointB: endpointB,
	}
}

// SetChannelOrdered sets the channel order for both endpoints to ORDERED.
func (path *Path) SetChannelOrdered() {
	path.EndpointA.ChannelConfig.Order = channeltypes.ORDERED
	path.EndpointB.ChannelConfig.Order = channeltypes.ORDERED
}

// RelayPacket attempts to relay the packet first on EndpointA and then on EndpointB
// if EndpointA does not contain a packet commitment for that packet. An error is returned
// if a relay step fails or the packet commitment does not exist on either endpoint.
func (path *Path) RelayPacket(packet channeltypes.Packet, ack []byte) error {
	pc := path.EndpointA.Chain.App.GetIBCKeeper().ChannelKeeper.GetPacketCommitment(path.EndpointA.Chain.GetContext(), packet.GetSourcePort(), packet.GetSourceChannel(), packet.GetSequence())
	if bytes.Equal(pc, channeltypes.CommitPacket(path.EndpointA.Chain.App.AppCodec(), packet)) {

		// packet found, relay from A to B
		if err := path.EndpointB.UpdateClient(); err != nil {
			return err
		}

		if err := path.EndpointB.RecvPacket(packet); err != nil {
			return err
		}

		if err := path.EndpointA.AcknowledgePacket(packet, ack); err != nil {
			return err
		}
		return nil

	}

	pc = path.EndpointB.Chain.App.GetIBCKeeper().ChannelKeeper.GetPacketCommitment(path.EndpointB.Chain.GetContext(), packet.GetSourcePort(), packet.GetSourceChannel(), packet.GetSequence())
	if bytes.Equal(pc, channeltypes.CommitPacket(path.EndpointB.Chain.App.AppCodec(), packet)) {

		// packet found, relay B to A
		if err := path.EndpointA.UpdateClient(); err != nil {
			return err
		}

		if err := path.EndpointA.RecvPacket(packet); err != nil {
			return err
		}
		if err := path.EndpointB.AcknowledgePacket(packet, ack); err != nil {
			return err
		}
		return nil
	}

	return fmt.Errorf("packet commitment does not exist on either endpoint for provided packet")
}

// SendMsg delivers the provided messages to the chain. The counterparty
// client is updated with the new source consensus state.
func (path *Path) SendMsg(msgs ...sdk.Msg) error {
	if err := path.EndpointA.Chain.sendMsgs(msgs...); err != nil {
		return err
	}
	if err := path.EndpointA.UpdateClient(); err != nil {
		return err
	}
	return path.EndpointB.UpdateClient()
}

func (path *Path) Invert() *Path {
	return &Path{
		EndpointA: path.EndpointB,
		EndpointB: path.EndpointA,
	}
}
