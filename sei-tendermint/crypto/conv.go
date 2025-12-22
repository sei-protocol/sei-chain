package crypto 

import (
	"fmt"

	"github.com/tendermint/tendermint/libs/utils"
	"github.com/tendermint/tendermint/crypto/ed25519"
	"github.com/tendermint/tendermint/internal/jsontypes"
	pb "github.com/tendermint/tendermint/proto/tendermint/crypto"
)

func init() {
	jsontypes.MustRegister((*pb.PublicKey)(nil))
	jsontypes.MustRegister((*pb.PublicKey_Ed25519)(nil))
}

var PubKeyConv = utils.ProtoConv[PubKey,*pb.PublicKey]{
	Encode: func(k PubKey) *pb.PublicKey { return utils.Alloc(PubKeyToProto(k)) },
	Decode: func(x *pb.PublicKey) (PubKey,error) { return PubKeyFromProto(*x) },
}

// PubKeyToProto takes crypto.PubKey and transforms it to a protobuf Pubkey
func PubKeyToProto(k PubKey) pb.PublicKey {
	return pb.PublicKey{Sum: &pb.PublicKey_Ed25519{Ed25519: k.Bytes()}}
}

// PubKeyFromProto takes a protobuf Pubkey and transforms it to a crypto.Pubkey
func PubKeyFromProto(k pb.PublicKey) (PubKey, error) {
	switch k := k.Sum.(type) {
	case *pb.PublicKey_Ed25519:
		return ed25519.PublicKeyFromBytes(k.Ed25519)
	default:
		return PubKey{}, fmt.Errorf("fromproto: key type %v is not supported", k)
	}
}
