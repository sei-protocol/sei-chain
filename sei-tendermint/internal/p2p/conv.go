package p2p

import (
	"errors"
	"fmt"
	"net/url"
	"strings"

	atypes "github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/crypto/ed25519"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/p2p/conn"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/p2p/pb"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/protoutils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/types"
)

type NodeSecretKey ed25519.SecretKey
type NodePublicKey ed25519.PublicKey

func (k NodePublicKey) String() string   { return fmt.Sprintf("node:%v", ed25519.PublicKey(k).String()) }
func (k NodePublicKey) GoString() string { return k.String() }

// NodePublicKeyFromString parses a NodePublicKey from its string representation ("node:ed25519:public:hex").
func NodePublicKeyFromString(s string) (NodePublicKey, error) {
	s2 := strings.TrimPrefix(s, "node:")
	if s == s2 {
		return NodePublicKey{}, errors.New("bad prefix")
	}
	k, err := ed25519.PublicKeyFromString(s2)
	if err != nil {
		return NodePublicKey{}, err
	}
	return NodePublicKey(k), nil
}

// MarshalText implements the encoding.TextMarshaler interface.
func (k NodePublicKey) MarshalText() ([]byte, error) {
	return []byte(k.String()), nil
}

// UnmarshalText implements the encoding.TextUnmarshaler interface.
func (k *NodePublicKey) UnmarshalText(b []byte) error {
	x, err := NodePublicKeyFromString(string(b))
	if err != nil {
		return err
	}
	*k = x
	return nil
}

func (k NodeSecretKey) String() string   { return fmt.Sprintf("<secret of %v>", k.Public().String()) }
func (k NodeSecretKey) GoString() string { return k.String() }

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

var nodePublicKeyConv = protoutils.Conv[NodePublicKey, *pb.NodePublicKey]{
	Encode: func(k NodePublicKey) *pb.NodePublicKey {
		return &pb.NodePublicKey{Ed25519: k.Bytes()}
	},
	Decode: func(p *pb.NodePublicKey) (NodePublicKey, error) {
		k, err := ed25519.PublicKeyFromBytes(p.Ed25519)
		if err != nil {
			return NodePublicKey{}, fmt.Errorf("ed25519: %w", err)
		}
		return NodePublicKey(k), nil
	},
}

type handshakeMsg struct {
	NodeAuth NodeChallengeSig
	SelfAddr utils.Option[NodeAddress]
	PexAddrs []NodeAddress
	Giga     utils.Option[gigaHandshake]
}

type gigaHandshake struct {
	Validator utils.Option[validatorHandshake]
}

var gigaHandshakeConv = protoutils.Conv[gigaHandshake, *pb.GigaHandshake]{
	Encode: func(m gigaHandshake) *pb.GigaHandshake {
		return &pb.GigaHandshake{Validator: validatorHandshakeConv.EncodeOpt(m.Validator)}
	},
	Decode: func(p *pb.GigaHandshake) (gigaHandshake, error) {
		validator, err := validatorHandshakeConv.DecodeOpt(p.Validator)
		if err != nil {
			return gigaHandshake{}, fmt.Errorf("validator: %w", err)
		}
		return gigaHandshake{Validator: validator}, nil
	},
}

type validatorHandshake struct {
	utils.ReadOnly
	auth   *atypes.Signed[atypes.Challenge]
	EVMRPC url.URL
}

var validatorHandshakeConv = protoutils.Conv[validatorHandshake, *pb.ValidatorHandshake]{
	Encode: func(m validatorHandshake) *pb.ValidatorHandshake {
		return &pb.ValidatorHandshake{
			Auth:      atypes.SignedChallengeConv.Encode(m.auth),
			EvmRpcUrl: new(m.EVMRPC.String()),
		}
	},
	Decode: func(p *pb.ValidatorHandshake) (validatorHandshake, error) {
		auth, err := atypes.SignedChallengeConv.DecodeReq(p.Auth)
		if err != nil {
			return validatorHandshake{}, fmt.Errorf("auth: %w", err)
		}
		if p.EvmRpcUrl == nil {
			return validatorHandshake{}, fmt.Errorf("evm_rpc_url: missing")
		}
		endpoint, err := url.Parse(*p.EvmRpcUrl)
		if err != nil {
			return validatorHandshake{}, fmt.Errorf("evm_rpc_url: %w", err)
		}
		if err := utils.CheckRPCURL(*endpoint); err != nil {
			return validatorHandshake{}, fmt.Errorf("evm_rpc_url: %w", err)
		}
		return validatorHandshake{auth: auth, EVMRPC: *endpoint}, nil
	},
}

var handshakeMsgConv = protoutils.Conv[*handshakeMsg, *pb.Handshake]{
	Encode: func(m *handshakeMsg) *pb.Handshake {
		var selfAddr *string
		if addr, ok := m.SelfAddr.Get(); ok {
			selfAddr = new(addr.String())
		}
		pexAddrs := make([]string, len(m.PexAddrs))
		for i, addr := range m.PexAddrs {
			pexAddrs[i] = addr.String()
		}
		return &pb.Handshake{
			NodeAuthKey: nodePublicKeyConv.Encode(m.NodeAuth.Key()),
			NodeAuthSig: m.NodeAuth.sig.Bytes(),
			SelfAddr:    selfAddr,
			PexAddrs:    pexAddrs,
			Giga:        gigaHandshakeConv.EncodeOpt(m.Giga),
		}
	},
	Decode: func(p *pb.Handshake) (*handshakeMsg, error) {
		nodeAuthKey, err := nodePublicKeyConv.DecodeReq(p.NodeAuthKey)
		if err != nil {
			return nil, fmt.Errorf("node_auth_key: %w", err)
		}
		nodeAuthSig, err := ed25519.SignatureFromBytes(p.NodeAuthSig)
		if err != nil {
			return nil, fmt.Errorf("node_auth_sig: %w", err)
		}
		var selfAddr utils.Option[NodeAddress]
		if p.SelfAddr != nil {
			addr, err := ParseNodeAddress(*p.SelfAddr)
			if err != nil {
				return nil, fmt.Errorf("self_addr: %w", err)
			}
			selfAddr = utils.Some(addr)
		}
		pexAddrs := make([]NodeAddress, len(p.PexAddrs))
		for i, addrString := range p.PexAddrs {
			addr, err := ParseNodeAddress(addrString)
			if err != nil {
				return nil, fmt.Errorf("pex_addrs[%v]: %w", i, err)
			}
			pexAddrs[i] = addr
		}
		giga, err := gigaHandshakeConv.DecodeOpt(p.Giga)
		if err != nil {
			return nil, fmt.Errorf("giga: %w", err)
		}
		return &handshakeMsg{
			NodeAuth: NodeChallengeSig{key: nodeAuthKey, sig: nodeAuthSig},
			SelfAddr: selfAddr,
			PexAddrs: pexAddrs,
			Giga:     giga,
		}, nil
	},
}
