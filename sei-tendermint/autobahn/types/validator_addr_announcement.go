package types

import (
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/pb"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/protoutils"
)

// ValidatorAddrAnnouncement is a validator's signed advertisement of where it can be dialed.
type ValidatorAddrAnnouncement struct {
	Version  uint64
	NodeKey  string
	HostPort string
}

func (m ValidatorAddrAnnouncement) asMsg() *pb.Msg {
	return &pb.Msg{T: &pb.Msg_ValidatorAddrAnnouncement{ValidatorAddrAnnouncement: ValidatorAddrAnnouncementConv.Encode(m)}}
}

// ValidatorAddrAnnouncementConv is a protobuf converter for ValidatorAddrAnnouncement.
var ValidatorAddrAnnouncementConv = protoutils.Conv[ValidatorAddrAnnouncement, *pb.ValidatorAddrAnnouncement]{
	Encode: func(m ValidatorAddrAnnouncement) *pb.ValidatorAddrAnnouncement {
		return &pb.ValidatorAddrAnnouncement{
			Version:  new(m.Version),
			NodeKey:  new(m.NodeKey),
			HostPort: new(m.HostPort),
		}
	},
	Decode: func(m *pb.ValidatorAddrAnnouncement) (ValidatorAddrAnnouncement, error) {
		if m.Version == nil {
			return ValidatorAddrAnnouncement{}, fmt.Errorf("version: missing")
		}
		if m.NodeKey == nil {
			return ValidatorAddrAnnouncement{}, fmt.Errorf("node_key: missing")
		}
		if m.HostPort == nil {
			return ValidatorAddrAnnouncement{}, fmt.Errorf("host_port: missing")
		}
		return ValidatorAddrAnnouncement{
			Version:  *m.Version,
			NodeKey:  *m.NodeKey,
			HostPort: *m.HostPort,
		}, nil
	},
}

// SignedValidatorAddrAnnouncementConv is a protobuf converter for Signed[ValidatorAddrAnnouncement].
var SignedValidatorAddrAnnouncementConv = protoutils.Conv[*Signed[ValidatorAddrAnnouncement], *pb.SignedValidatorAddrAnnouncement]{
	Encode: func(m *Signed[ValidatorAddrAnnouncement]) *pb.SignedValidatorAddrAnnouncement {
		return &pb.SignedValidatorAddrAnnouncement{
			Msg: ValidatorAddrAnnouncementConv.Encode(m.Msg()),
			Sig: SignatureConv.Encode(m.Sig()),
		}
	},
	Decode: func(m *pb.SignedValidatorAddrAnnouncement) (*Signed[ValidatorAddrAnnouncement], error) {
		msg, err := ValidatorAddrAnnouncementConv.DecodeReq(m.Msg)
		if err != nil {
			return nil, fmt.Errorf("msg: %w", err)
		}
		sig, err := SignatureConv.DecodeReq(m.Sig)
		if err != nil {
			return nil, fmt.Errorf("sig: %w", err)
		}
		return newSigned(msg, sig), nil
	},
}

// ValidatorAddrAck tells a peer that the sender holds Version of Validator's announcement.
type ValidatorAddrAck struct {
	Validator PublicKey
	Version   uint64
}

// ValidatorAddrAckConv is a protobuf converter for ValidatorAddrAck.
var ValidatorAddrAckConv = protoutils.Conv[ValidatorAddrAck, *pb.ValidatorAddrAck]{
	Encode: func(m ValidatorAddrAck) *pb.ValidatorAddrAck {
		return &pb.ValidatorAddrAck{
			Validator: PublicKeyConv.Encode(m.Validator),
			Version:   new(m.Version),
		}
	},
	Decode: func(m *pb.ValidatorAddrAck) (ValidatorAddrAck, error) {
		validator, err := PublicKeyConv.DecodeReq(m.Validator)
		if err != nil {
			return ValidatorAddrAck{}, fmt.Errorf("validator: %w", err)
		}
		if m.Version == nil {
			return ValidatorAddrAck{}, fmt.Errorf("version: missing")
		}
		return ValidatorAddrAck{Validator: validator, Version: *m.Version}, nil
	},
}
