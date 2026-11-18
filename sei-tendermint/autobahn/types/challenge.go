package types

import (
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/pb"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/protoutils"
)

// Challenge is the authentication challenge of a P2P secret connection.
type Challenge [32]byte

// ChallengeConv is a protobuf converter for Challenge.
var ChallengeConv = protoutils.Conv[Challenge, *pb.Challenge]{
	Encode: func(m Challenge) *pb.Challenge {
		return &pb.Challenge{Data: m[:]}
	},
	Decode: func(m *pb.Challenge) (Challenge, error) {
		if len(m.Data) != len(Challenge{}) {
			return Challenge{}, fmt.Errorf("challenge size = %d, want %d", len(m.Data), len(Challenge{}))
		}
		return Challenge(m.Data), nil
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
