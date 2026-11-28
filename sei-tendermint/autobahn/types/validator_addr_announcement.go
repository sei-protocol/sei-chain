package types

import (
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-tendermint/crypto/ed25519"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/pb"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/hashable"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/protoutils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
)

// ValidatorAddrAnnouncement is a validator's signed advertisement of where it can be dialed.
type ValidatorAddrAnnouncement struct {
	Version   uint64
	NodeKey   string
	HostPort  string
	Endpoints []ValidatorEndpoint
}

// ValidatorEndpoint is a named endpoint a validator advertises alongside its dial address.
type ValidatorEndpoint struct {
	Name     string
	HostPort string
}

// ValidatorEndpointConv is a protobuf converter for ValidatorEndpoint.
var ValidatorEndpointConv = protoutils.Conv[ValidatorEndpoint, *pb.ValidatorEndpoint]{
	Encode: func(m ValidatorEndpoint) *pb.ValidatorEndpoint {
		return &pb.ValidatorEndpoint{
			Name:     new(m.Name),
			HostPort: new(m.HostPort),
		}
	},
	Decode: func(m *pb.ValidatorEndpoint) (ValidatorEndpoint, error) {
		if m.Name == nil {
			return ValidatorEndpoint{}, fmt.Errorf("name: missing")
		}
		if m.HostPort == nil {
			return ValidatorEndpoint{}, fmt.Errorf("host_port: missing")
		}
		return ValidatorEndpoint{Name: *m.Name, HostPort: *m.HostPort}, nil
	},
}

// validatorAddrTag domain-separates announcement signatures from the signatures on Msg.
var validatorAddrTag = utils.OrPanic1(ed25519.NewTag("sei_giga_validator_addr"))

// SignedValidatorAddrAnnouncement is an announcement with the signature of the validator that made it.
type SignedValidatorAddrAnnouncement struct {
	utils.ReadOnly
	msg  ValidatorAddrAnnouncement
	hash hashable.Hash[*pb.ValidatorAddrAnnouncement]
	sig  *Signature
}

// Msg returns the announcement.
func (m *SignedValidatorAddrAnnouncement) Msg() ValidatorAddrAnnouncement { return m.msg }

// Hash returns the hash of the announcement's canonical encoding.
func (m *SignedValidatorAddrAnnouncement) Hash() hashable.Hash[*pb.ValidatorAddrAnnouncement] {
	return m.hash
}

// Sig returns the signature of the announcement.
func (m *SignedValidatorAddrAnnouncement) Sig() *Signature { return m.sig }

// Key returns the key that signed the announcement.
func (m *SignedValidatorAddrAnnouncement) Key() PublicKey { return m.sig.key }

// VerifySig verifies the signature over the announcement.
func (m *SignedValidatorAddrAnnouncement) VerifySig() error {
	return m.sig.key.key.VerifyWithTag(validatorAddrTag, m.hash[:], m.sig.sig)
}

// SignValidatorAddr signs an announcement.
func (k SecretKey) SignValidatorAddr(msg ValidatorAddrAnnouncement) *SignedValidatorAddrAnnouncement {
	hash := hashValidatorAddr(msg)
	return &SignedValidatorAddrAnnouncement{
		msg:  msg,
		hash: hash,
		sig: &Signature{
			key: k.Public(),
			sig: k.key.SignWithTag(validatorAddrTag, hash[:]),
		},
	}
}

// hashValidatorAddr hashes the canonical encoding of msg.
func hashValidatorAddr(msg ValidatorAddrAnnouncement) hashable.Hash[*pb.ValidatorAddrAnnouncement] {
	return hashable.ToHash(ValidatorAddrAnnouncementConv.Encode(msg))
}

// newSignedValidatorAddr pairs msg with sig, hashing msg as it is re-encoded.
func newSignedValidatorAddr(msg ValidatorAddrAnnouncement, sig *Signature) *SignedValidatorAddrAnnouncement {
	return &SignedValidatorAddrAnnouncement{msg: msg, hash: hashValidatorAddr(msg), sig: sig}
}

// ValidatorAddrAnnouncementConv is a protobuf converter for ValidatorAddrAnnouncement.
var ValidatorAddrAnnouncementConv = protoutils.Conv[ValidatorAddrAnnouncement, *pb.ValidatorAddrAnnouncement]{
	Encode: func(m ValidatorAddrAnnouncement) *pb.ValidatorAddrAnnouncement {
		return &pb.ValidatorAddrAnnouncement{
			Version:   new(m.Version),
			NodeKey:   new(m.NodeKey),
			HostPort:  new(m.HostPort),
			Endpoints: ValidatorEndpointConv.EncodeSlice(m.Endpoints),
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
		endpoints, err := ValidatorEndpointConv.DecodeSlice(m.Endpoints)
		if err != nil {
			return ValidatorAddrAnnouncement{}, fmt.Errorf("endpoints: %w", err)
		}
		return ValidatorAddrAnnouncement{
			Version:   *m.Version,
			NodeKey:   *m.NodeKey,
			HostPort:  *m.HostPort,
			Endpoints: endpoints,
		}, nil
	},
}

// SignedValidatorAddrAnnouncementConv is a protobuf converter for SignedValidatorAddrAnnouncement.
var SignedValidatorAddrAnnouncementConv = protoutils.Conv[*SignedValidatorAddrAnnouncement, *pb.SignedValidatorAddrAnnouncement]{
	Encode: func(m *SignedValidatorAddrAnnouncement) *pb.SignedValidatorAddrAnnouncement {
		return &pb.SignedValidatorAddrAnnouncement{
			Msg: ValidatorAddrAnnouncementConv.Encode(m.msg),
			Sig: SignatureConv.Encode(m.sig),
		}
	},
	Decode: func(m *pb.SignedValidatorAddrAnnouncement) (*SignedValidatorAddrAnnouncement, error) {
		msg, err := ValidatorAddrAnnouncementConv.DecodeReq(m.Msg)
		if err != nil {
			return nil, fmt.Errorf("msg: %w", err)
		}
		sig, err := SignatureConv.DecodeReq(m.Sig)
		if err != nil {
			return nil, fmt.Errorf("sig: %w", err)
		}
		return newSignedValidatorAddr(msg, sig), nil
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

// ValidatorAddrMsg is one message on the validator address stream: a ValidatorAddrAnnouncementMsg
// or a ValidatorAddrAck.
type ValidatorAddrMsg interface {
	asValidatorAddrMsg() *pb.ValidatorAddrMsg
}

// ValidatorAddrAnnouncementMsg is a signed announcement as a ValidatorAddrMsg.
type ValidatorAddrAnnouncementMsg struct {
	Ann *SignedValidatorAddrAnnouncement
}

func (m ValidatorAddrAnnouncementMsg) asValidatorAddrMsg() *pb.ValidatorAddrMsg {
	return &pb.ValidatorAddrMsg{T: &pb.ValidatorAddrMsg_Announcement{Announcement: SignedValidatorAddrAnnouncementConv.Encode(m.Ann)}}
}

func (m ValidatorAddrAck) asValidatorAddrMsg() *pb.ValidatorAddrMsg {
	return &pb.ValidatorAddrMsg{T: &pb.ValidatorAddrMsg_Ack{Ack: ValidatorAddrAckConv.Encode(m)}}
}

// ValidatorAddrMsgConv is a protobuf converter for ValidatorAddrMsg. Decode rejects a message with
// no variant set, and any variant whose contents do not decode.
var ValidatorAddrMsgConv = protoutils.Conv[ValidatorAddrMsg, *pb.ValidatorAddrMsg]{
	Encode: func(m ValidatorAddrMsg) *pb.ValidatorAddrMsg {
		return m.asValidatorAddrMsg()
	},
	Decode: func(m *pb.ValidatorAddrMsg) (ValidatorAddrMsg, error) {
		switch t := m.T.(type) {
		case *pb.ValidatorAddrMsg_Announcement:
			ann, err := SignedValidatorAddrAnnouncementConv.DecodeReq(t.Announcement)
			if err != nil {
				return nil, fmt.Errorf("announcement: %w", err)
			}
			return ValidatorAddrAnnouncementMsg{Ann: ann}, nil
		case *pb.ValidatorAddrMsg_Ack:
			ack, err := ValidatorAddrAckConv.DecodeReq(t.Ack)
			if err != nil {
				return nil, fmt.Errorf("ack: %w", err)
			}
			return ack, nil
		case nil:
			return nil, fmt.Errorf("empty")
		default:
			return nil, fmt.Errorf("unknown ValidatorAddrMsg type: %T", t)
		}
	},
}
