package types

import (
	"encoding"
	"errors"
	"fmt"
	"strings"

	"github.com/sei-protocol/sei-chain/sei-tendermint/crypto/ed25519"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/pb"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/hashable"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/protoutils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
)

var autobahnTag = utils.OrPanic1(ed25519.NewTag("sei_giga_autobahn"))

// Msg is the interface for all messages signable by a stream node.
type Msg interface{ asMsg() *pb.Msg }

func (m *LaneProposal) asMsg() *pb.Msg {
	return &pb.Msg{T: &pb.Msg_LaneProposal{LaneProposal: LaneProposalConv.Encode(m)}}
}

func (m *LaneVote) asMsg() *pb.Msg {
	return &pb.Msg{T: &pb.Msg_LaneVote{LaneVote: LaneVoteConv.Encode(m)}}
}

func (m *AppVote) asMsg() *pb.Msg {
	return &pb.Msg{T: &pb.Msg_AppVote{AppVote: AppVoteConv.Encode(m)}}
}

func (m *Proposal) asMsg() *pb.Msg {
	return &pb.Msg{T: &pb.Msg_Proposal{Proposal: ProposalConv.Encode(m)}}
}

func (m *PrepareVote) asMsg() *pb.Msg {
	return &pb.Msg{T: &pb.Msg_PrepareVote{PrepareVote: PrepareVoteConv.Encode(m)}}
}

func (m *CommitVote) asMsg() *pb.Msg {
	return &pb.Msg{T: &pb.Msg_CommitVote{CommitVote: CommitVoteConv.Encode(m)}}
}

func (m *TimeoutVote) asMsg() *pb.Msg {
	return &pb.Msg{T: &pb.Msg_TimeoutVote{TimeoutVote: TimeoutVoteConv.Encode(m)}}
}

// Hash is the hash of a message.
type Hash[T Msg] hashable.Hash[*pb.Msg]

// Hashed is a message with its hash.
type Hashed[T Msg] struct {
	utils.ReadOnly
	msg  T
	hash Hash[T]
}

// Msg returns the message.
func (m *Hashed[T]) Msg() T { return m.msg }

// Hash returns the hash.
func (m *Hashed[T]) Hash() Hash[T] { return m.hash }

// NewHashed creates a new Hashed message.
func NewHashed[T Msg](msg T) *Hashed[T] {
	return &Hashed[T]{
		msg:  msg,
		hash: Hash[T](hashable.ToHash(msg.asMsg())),
	}
}

// SecretKey is the secret key of the validator.
type SecretKey struct{ key ed25519.SecretKey }

// Public returns the public key corresponding to the secret key.
func (k SecretKey) Public() PublicKey {
	return PublicKey{key: k.key.Public()}
}

// PublicKey is the public key of the validator.
// nolint:recvcheck
type PublicKey struct {
	utils.ReadOnly
	key ed25519.PublicKey
}

// Compare implements Comparable.
func (k PublicKey) Compare(other PublicKey) int { return k.key.Compare(other.key) }

// Bytes converts the public key to bytes.
func (k PublicKey) Bytes() []byte { return k.key.Bytes() }

// PublicKeyFromBytes constructs a public key from bytes.
func PublicKeyFromBytes(b []byte) (PublicKey, error) {
	k, err := ed25519.PublicKeyFromBytes(b)
	if err != nil {
		return PublicKey{}, err
	}
	return PublicKey{key: k}, nil
}

// String returns a string representation.
func (k PublicKey) String() string { return "validator:" + k.key.String() }

// PublicKeyFromString constructs a public key from a string representation.
func PublicKeyFromString(s string) (PublicKey, error) {
	s2 := strings.TrimPrefix(s, "validator:")
	if s == s2 {
		return PublicKey{}, errors.New("bad prefix")
	}
	k, err := ed25519.PublicKeyFromString(s2)
	if err != nil {
		return PublicKey{}, err
	}
	return PublicKey{key: k}, nil
}

// GoString returns a strings representation.
func (k PublicKey) GoString() string { return k.String() }

// MarshalText implements the encoding.TextMarshaler interface.
func (k PublicKey) MarshalText() ([]byte, error) {
	return []byte(k.String()), nil
}

// UnmarshalText implements the encoding.TextUnmarshaler interface.
func (k *PublicKey) UnmarshalText(b []byte) error {
	x, err := PublicKeyFromString(string(b))
	if err != nil {
		return err
	}
	*k = x
	return nil
}

var _ encoding.TextMarshaler = PublicKey{}
var _ encoding.TextUnmarshaler = (*PublicKey)(nil)

// String returns a log-safe representation of the secret key.
func (k SecretKey) String() string {
	return fmt.Sprintf("<secret of %s>", k.Public().String())
}

// GoString returns a log-safe representation of the secret key.
func (k SecretKey) GoString() string { return k.String() }

// Sign signs a message.
func Sign[T Msg](key SecretKey, msg T) *Signed[T] {
	hMsg := NewHashed(msg)
	return &Signed[T]{
		hashed: hMsg,
		sig: &Signature{
			key: key.Public(),
			sig: key.key.SignWithTag(autobahnTag, hMsg.hash[:]),
		},
	}
}

// Signature represents a signature on the consensus message.
type Signature struct {
	utils.ReadOnly
	key PublicKey
	sig ed25519.Signature
}

// Signed is a hashed message with its signature.
type Signed[T Msg] struct {
	utils.ReadOnly
	hashed *Hashed[T]
	sig    *Signature
}

// Msg returns the message.
func (m *Signed[T]) Msg() T { return m.hashed.msg }

// Hash returns the hash of the message.
func (m *Signed[T]) Hash() Hash[T] { return m.hashed.hash }

// Sig returns the signature of the message.
func (m *Signed[T]) Sig() *Signature { return m.sig }

// Key returns the key whish signed the message.
func (m *Signed[T]) Key() PublicKey { return m.sig.key }

// VerifySig verifies the signature of the message.
func (m *Signed[T]) VerifySig(c *Committee) error {
	if !c.Replicas().Has(m.sig.key) {
		return fmt.Errorf("%q is not a replica", m.sig.key)
	}
	return m.sig.key.key.VerifyWithTag(autobahnTag, m.hashed.hash[:], m.sig.sig)
}

// verifyQC verifies a slice of signatures and checks if they form a quorum.
func (m *Hashed[T]) verifyQC(c *Committee, quorum int, sigs []*Signature) error {
	done := map[PublicKey]struct{}{}
	for _, sig := range sigs {
		if _, ok := done[sig.key]; ok {
			return fmt.Errorf("duplicate signature from %q", sig.key)
		}
		done[sig.key] = struct{}{}
		sm := &Signed[T]{hashed: m, sig: sig}
		if err := sm.VerifySig(c); err != nil {
			return err
		}
	}
	if len(done) < quorum {
		return fmt.Errorf("not enough signatures: got %d, want >= %d", len(done), quorum)
	}
	return nil
}

// PublicKeyConv is a protobuf converter for PublicKey.
var PublicKeyConv = protoutils.Conv[PublicKey, *pb.PublicKey]{
	Encode: func(k PublicKey) *pb.PublicKey {
		return &pb.PublicKey{
			Ed25519: k.Bytes(),
		}
	},
	Decode: func(p *pb.PublicKey) (PublicKey, error) {
		key, err := PublicKeyFromBytes(p.Ed25519)
		if err != nil {
			return PublicKey{}, err
		}
		return key, nil
	},
}

// SignatureConv is a protobuf converter for Signature.
var SignatureConv = protoutils.Conv[*Signature, *pb.Signature]{
	Encode: func(s *Signature) *pb.Signature {
		return &pb.Signature{
			Key: PublicKeyConv.Encode(s.key),
			Sig: s.sig.Bytes(),
		}
	},
	Decode: func(p *pb.Signature) (*Signature, error) {
		key, err := PublicKeyConv.Decode(p.Key)
		if err != nil {
			return nil, fmt.Errorf("key: %w", err)
		}
		sig, err := ed25519.SignatureFromBytes(p.Sig)
		if err != nil {
			return nil, fmt.Errorf("sig: %w", err)
		}
		return &Signature{key: key, sig: sig}, nil
	},
}

// MsgConv is a protobuf converter for Msg.
var MsgConv = protoutils.Conv[Msg, *pb.Msg]{
	Encode: func(m Msg) *pb.Msg {
		return m.asMsg()
	},
	Decode: func(m *pb.Msg) (Msg, error) {
		if m.T == nil {
			return nil, errors.New("empty")
		}
		switch t := m.T.(type) {
		case *pb.Msg_LaneProposal:
			return LaneProposalConv.DecodeReq(t.LaneProposal)
		case *pb.Msg_LaneVote:
			return LaneVoteConv.DecodeReq(t.LaneVote)
		case *pb.Msg_Proposal:
			return ProposalConv.DecodeReq(t.Proposal)
		case *pb.Msg_PrepareVote:
			return PrepareVoteConv.DecodeReq(t.PrepareVote)
		case *pb.Msg_CommitVote:
			return CommitVoteConv.DecodeReq(t.CommitVote)
		case *pb.Msg_TimeoutVote:
			return TimeoutVoteConv.DecodeReq(t.TimeoutVote)
		case *pb.Msg_AppVote:
			return AppVoteConv.DecodeReq(t.AppVote)
		default:
			return nil, fmt.Errorf("unknown Msg type: %T", t)
		}
	},
}

// AsMsg casts a hashed message to Hashed[Msg].
func (m *Hashed[T]) AsMsg() *Hashed[Msg] {
	return &Hashed[Msg]{msg: m.msg, hash: Hash[Msg](hashable.Hash[*pb.Msg](m.hash))}
}

// AsMsg casts a signed message to Signed[Msg].
func (m *Signed[T]) AsMsg() *Signed[Msg] {
	return &Signed[Msg]{hashed: m.hashed.AsMsg(), sig: m.sig}
}

// HashedCast PANICS if msg.Msg() is not of type T.
func HashedCastOrPanic[T Msg](msg *Hashed[Msg]) *Hashed[T] {
	return &Hashed[T]{msg: msg.msg.(T), hash: Hash[T](hashable.Hash[*pb.Msg](msg.hash))}
}

// SignedCast PANICS if msg.Msg() is not of type T.
func SignedCastOrPanic[T Msg](msg *Signed[Msg]) *Signed[T] {
	return &Signed[T]{hashed: HashedCastOrPanic[T](msg.hashed), sig: msg.sig}
}

// SignedMsgConv is a protobuf converter for Signed[Msg].
func SignedMsgConv[T Msg]() *protoutils.Conv[*Signed[T], *pb.SignedMsg] {
	return &protoutils.Conv[*Signed[T], *pb.SignedMsg]{
		Encode: func(m *Signed[T]) *pb.SignedMsg {
			return &pb.SignedMsg{Msg: MsgConv.Encode(m.hashed.msg), Sig: SignatureConv.Encode(m.sig)}
		},
		Decode: func(m *pb.SignedMsg) (*Signed[T], error) {
			msg, err := MsgConv.Decode(m.Msg)
			if err != nil {
				return nil, fmt.Errorf("msg: %w", err)
			}
			sig, err := SignatureConv.Decode(m.Sig)
			if err != nil {
				return nil, fmt.Errorf("sig: %w", err)
			}
			v, ok := msg.(T)
			if !ok {
				return nil, fmt.Errorf("msg: got %T, want %T", msg, v)
			}
			return &Signed[T]{hashed: NewHashed(v), sig: sig}, nil
		},
	}
}
