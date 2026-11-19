package types

import (
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/pb"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/protoutils"
)

// SessionKey identifies a P2P secret connection.
type SessionKey [32]byte

// Challenge is a validator's authentication challenge for one end of a P2P secret connection.
type Challenge struct {
	SessionKey SessionKey
	IsServer   bool
}

// ChallengeConv is a protobuf converter for Challenge.
var ChallengeConv = protoutils.Conv[Challenge, *pb.Challenge]{
	Encode: func(m Challenge) *pb.Challenge {
		return &pb.Challenge{
			SessionKey: m.SessionKey[:],
			IsServer:   new(m.IsServer),
		}
	},
	Decode: func(m *pb.Challenge) (Challenge, error) {
		if len(m.SessionKey) != len(SessionKey{}) {
			return Challenge{}, fmt.Errorf("session_key: size = %d, want %d", len(m.SessionKey), len(SessionKey{}))
		}
		if m.IsServer == nil {
			return Challenge{}, fmt.Errorf("is_server: missing")
		}
		return Challenge{
			SessionKey: SessionKey(m.SessionKey),
			IsServer:   *m.IsServer,
		}, nil
	},
}

// SignedChallengeConv is a protobuf converter for Signed[Challenge].
var SignedChallengeConv = protoutils.Conv[*Signed[Challenge], *pb.SignedChallenge]{
	Encode: func(m *Signed[Challenge]) *pb.SignedChallenge {
		return &pb.SignedChallenge{
			Msg: ChallengeConv.Encode(m.Msg()),
			Sig: SignatureConv.Encode(m.Sig()),
		}
	},
	Decode: func(m *pb.SignedChallenge) (*Signed[Challenge], error) {
		msg, err := ChallengeConv.DecodeReq(m.Msg)
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
