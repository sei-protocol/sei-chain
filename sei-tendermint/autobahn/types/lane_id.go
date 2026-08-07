package types

import (
	"cmp"
	"crypto/ed25519"
	"encoding/binary"
	"encoding/hex"
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/pb"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/protoutils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
)

// LaneID identifies a validator's lane for a continuous committee membership
// streak. e_join is the epoch in which the validator most recently joined.
type LaneID struct {
	utils.ReadOnly
	validator PublicKey
	eJoin     EpochIndex
}

// NewLaneID constructs a LaneID.
func NewLaneID(validator PublicKey, eJoin EpochIndex) LaneID {
	return LaneID{validator: validator, eJoin: eJoin}
}

// Validator returns the lane producer.
func (l LaneID) Validator() PublicKey { return l.validator }

// EJoin returns the epoch in which this lane's membership streak started.
func (l LaneID) EJoin() EpochIndex { return l.eJoin }

// Compare implements a total order: validator first, then e_join.
func (l LaneID) Compare(other LaneID) int {
	if c := l.validator.Compare(other.validator); c != 0 {
		return c
	}
	return cmp.Compare(l.eJoin, other.eJoin)
}

// Bytes returns a stable encoding: pubkey bytes || big-endian e_join.
func (l LaneID) Bytes() []byte {
	vb := l.validator.Bytes()
	b := make([]byte, 0, len(vb)+8)
	b = append(b, vb...)
	return binary.BigEndian.AppendUint64(b, uint64(l.eJoin))
}

// LaneIDFromBytes parses Bytes() encoding (exactly ed25519 pubkey || u64be e_join).
func LaneIDFromBytes(b []byte) (LaneID, error) {
	want := ed25519.PublicKeySize + 8
	if len(b) != want {
		return LaneID{}, fmt.Errorf("LaneID: got %d bytes, want %d", len(b), want)
	}
	eJoin := EpochIndex(binary.BigEndian.Uint64(b[ed25519.PublicKeySize:]))
	validator, err := PublicKeyFromBytes(b[:ed25519.PublicKeySize])
	if err != nil {
		return LaneID{}, fmt.Errorf("LaneID validator: %w", err)
	}
	return NewLaneID(validator, eJoin), nil
}

func (l LaneID) String() string {
	return fmt.Sprintf("%s@e%d", l.validator.String(), l.eJoin)
}

// HexString encodes Bytes() as hex (WAL directory names).
func (l LaneID) HexString() string { return hex.EncodeToString(l.Bytes()) }

// LaneIDConv is a protobuf converter for LaneID.
var LaneIDConv = protoutils.Conv[LaneID, *pb.LaneID]{
	Encode: func(l LaneID) *pb.LaneID {
		return &pb.LaneID{
			Validator: PublicKeyConv.Encode(l.validator),
			EJoin:     utils.Alloc(uint64(l.eJoin)),
		}
	},
	Decode: func(p *pb.LaneID) (LaneID, error) {
		validator, err := PublicKeyConv.DecodeReq(p.Validator)
		if err != nil {
			return LaneID{}, fmt.Errorf("validator: %w", err)
		}
		if p.EJoin == nil {
			return LaneID{}, fmt.Errorf("e_join: missing")
		}
		return NewLaneID(validator, EpochIndex(*p.EJoin)), nil
	},
}
