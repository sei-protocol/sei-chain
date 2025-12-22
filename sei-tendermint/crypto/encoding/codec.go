package encoding

import (
	"fmt"

	"github.com/tendermint/tendermint/crypto"
	"github.com/tendermint/tendermint/crypto/ed25519"
	"github.com/tendermint/tendermint/internal/jsontypes"
	cryptoproto "github.com/tendermint/tendermint/proto/tendermint/crypto"
)

func init() {
	jsontypes.MustRegister((*cryptoproto.PublicKey)(nil))
	jsontypes.MustRegister((*cryptoproto.PublicKey_Ed25519)(nil))
}

// PubKeyToProto takes crypto.PubKey and transforms it to a protobuf Pubkey
func PubKeyToProto(k crypto.PubKey) cryptoproto.PublicKey {
	return cryptoproto.PublicKey{Sum: &cryptoproto.PublicKey_Ed25519{Ed25519: k.Bytes()}}
}

// PubKeyFromProto takes a protobuf Pubkey and transforms it to a crypto.Pubkey
func PubKeyFromProto(k cryptoproto.PublicKey) (crypto.PubKey, error) {
	switch k := k.Sum.(type) {
	case *cryptoproto.PublicKey_Ed25519:
		return ed25519.PublicKeyFromBytes(k.Ed25519)
	default:
		return crypto.PubKey{}, fmt.Errorf("fromproto: key type %v is not supported", k)
	}
}
