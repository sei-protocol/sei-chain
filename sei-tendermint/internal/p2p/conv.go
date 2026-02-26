package p2p

import (
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-tendermint/crypto/ed25519"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/p2p/conn"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/p2p/pb"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/protoutils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/types"
)

type NodeSecretKey ed25519.SecretKey
type NodePublicKey ed25519.PublicKey

type NodeChallengeSig struct {
	utils.ReadOnly
	key NodePublicKey
	sig ed25519.Signature
}

func (k NodePublicKey) Bytes() []byte         { return ed25519.PublicKey(k).Bytes() }
func (k NodeSecretKey) Public() NodePublicKey { return NodePublicKey(ed25519.SecretKey(k).Public()) }
func (k NodeSecretKey) SignChallenge(challenge conn.Challenge) NodeChallengeSig {
	return NodeChallengeSig{key: k.Public(), sig: ed25519.SecretKey(k).Sign(challenge[:])}
}

func (s NodeChallengeSig) Key() NodePublicKey { return s.key }
func (s NodeChallengeSig) Verify(challenge conn.Challenge) error {
	return ed25519.PublicKey(s.key).Verify(challenge[:], s.sig)
}

func (k NodePublicKey) NodeID() types.NodeID {
	return types.NodeIDFromPubKey(ed25519.PublicKey(k))
}

var nodePublicKeyConv = utils.ProtoConv[NodePublicKey, *pb.NodePublicKey]{
	Encode: func(k NodePublicKey) *pb.NodePublicKey {
		return &pb.NodePublicKey{Ed25519: k.Bytes()}
	},
	Decode: func(p *pb.NodePublicKey) (NodePublicKey, error) {
		k, err := ed25519.PublicKeyFromBytes(p.Ed25519)
		if err != nil {
			return NodePublicKey{}, fmt.Errorf("Ed25519: %w", err)
		}
		return NodePublicKey(k), nil
	},
}

type handshakeMsg struct {
	NodeAuth          NodeChallengeSig
	SelfAddr          utils.Option[NodeAddress]
	SeiGigaConnection bool
}

var handshakeMsgConv = protoutils.Conv[*handshakeMsg, *pb.Handshake]{
	Encode: func(m *handshakeMsg) *pb.Handshake {
		var selfAddr *string
		if a, ok := m.SelfAddr.Get(); ok {
			selfAddr = utils.Alloc(a.String())
		}

		return &pb.Handshake{
			NodeAuthKey:       nodePublicKeyConv.Encode(m.NodeAuth.Key()),
			NodeAuthSig:       m.NodeAuth.sig.Bytes(),
			SelfAddr:          selfAddr,
			SeiGigaConnection: m.SeiGigaConnection,
		}
	},
	Decode: func(p *pb.Handshake) (*handshakeMsg, error) {
		nodeAuthKey, err := nodePublicKeyConv.DecodeReq(p.NodeAuthKey)
		if err != nil {
			return nil, fmt.Errorf("NodeAuthKey: %w", err)
		}
		nodeAuthSig, err := ed25519.SignatureFromBytes(p.NodeAuthSig)
		if err != nil {
			return nil, fmt.Errorf("NodeAuthSig: %w", err)
		}
		var selfAddr utils.Option[NodeAddress]
		if p.SelfAddr != nil {
			addr, err := ParseNodeAddress(*p.SelfAddr)
			if err != nil {
				return nil, fmt.Errorf("SelfAddr: %w", err)
			}
			selfAddr = utils.Some(addr)
		}
		return &handshakeMsg{
			NodeAuth:          NodeChallengeSig{key: nodeAuthKey, sig: nodeAuthSig},
			SelfAddr:          selfAddr,
			SeiGigaConnection: p.SeiGigaConnection,
		}, nil
	},
}
