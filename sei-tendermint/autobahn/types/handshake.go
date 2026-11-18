package types

import (
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-tendermint/crypto/ed25519"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/pb"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/hashable"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/protoutils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
)

var handshakeTag = utils.OrPanic1(ed25519.NewTag("SEI_GIGA_VALIDATOR_HANDSHAKE_V1"))

// Handshake is the payload authenticated by a validator during a P2P handshake.
type Handshake struct {
	Challenge Challenge
	NodeKey   ed25519.PublicKey
	// SelfAddr and EVMRPC are the exact advertised strings covered by the signature.
	SelfAddr string
	EVMRPC   string
}

// Hash returns the hash of the canonical protobuf handshake payload.
func (m Handshake) Hash() hashable.Hash[*pb.Handshake] {
	return hashable.ToHash(HandshakeConv.Encode(m))
}

// SignHandshake signs a validator's P2P handshake payload.
func (k SecretKey) SignHandshake(msg Handshake) HandshakeSig {
	hash := msg.Hash()
	return HandshakeSig{sig: k.key.SignWithTag(handshakeTag, hash[:])}
}

// HandshakeSig is a validator's signature on a P2P handshake.
type HandshakeSig struct {
	utils.ReadOnly
	sig ed25519.Signature
}

// Bytes returns the encoded signature.
func (s HandshakeSig) Bytes() []byte { return s.sig.Bytes() }

// HandshakeSigFromBytes decodes a handshake signature.
func HandshakeSigFromBytes(b []byte) (HandshakeSig, error) {
	sig, err := ed25519.SignatureFromBytes(b)
	if err != nil {
		return HandshakeSig{}, err
	}
	return HandshakeSig{sig: sig}, nil
}

// VerifyHandshake verifies a validator's P2P handshake signature.
func (k PublicKey) VerifyHandshake(msg Handshake, sig HandshakeSig) error {
	hash := msg.Hash()
	return k.key.VerifyWithTag(handshakeTag, hash[:], sig.sig)
}

// HandshakeConv is a protobuf converter for Handshake.
var HandshakeConv = protoutils.Conv[Handshake, *pb.Handshake]{
	Encode: func(m Handshake) *pb.Handshake {
		return &pb.Handshake{
			Challenge: m.Challenge[:],
			NodeKey:   m.NodeKey.Bytes(),
			SelfAddr:  new(m.SelfAddr),
			EvmRpc:    new(m.EVMRPC),
		}
	},
	Decode: func(p *pb.Handshake) (Handshake, error) {
		if len(p.Challenge) != len(Challenge{}) {
			return Handshake{}, fmt.Errorf("Challenge: size = %d, want %d", len(p.Challenge), len(Challenge{}))
		}
		nodeKey, err := ed25519.PublicKeyFromBytes(p.NodeKey)
		if err != nil {
			return Handshake{}, fmt.Errorf("NodeKey: %w", err)
		}
		if p.SelfAddr == nil {
			return Handshake{}, fmt.Errorf("SelfAddr: missing")
		}
		if p.EvmRpc == nil {
			return Handshake{}, fmt.Errorf("EvmRpc: missing")
		}
		return Handshake{
			Challenge: Challenge(p.Challenge),
			NodeKey:   nodeKey,
			SelfAddr:  *p.SelfAddr,
			EVMRPC:    *p.EvmRpc,
		}, nil
	},
}
