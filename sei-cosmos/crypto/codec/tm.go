package codec

import (
	"github.com/sei-protocol/sei-chain/sei-tendermint/crypto"
	pb "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/crypto"

	"github.com/sei-protocol/sei-chain/sei-cosmos/crypto/keys/ed25519"
	"github.com/sei-protocol/sei-chain/sei-cosmos/crypto/keys/secp256k1"
	cryptotypes "github.com/sei-protocol/sei-chain/sei-cosmos/crypto/types"
	sdkerrors "github.com/sei-protocol/sei-chain/sei-cosmos/types/errors"
)

// FromTmProtoPublicKey converts a TM's pb.PublicKey into our own PubKey.
func FromTmProtoPublicKey(protoPk pb.PublicKey) (cryptotypes.PubKey, error) {
	switch protoPk := protoPk.Sum.(type) {
	case *pb.PublicKey_Ed25519:
		return &ed25519.PubKey{
			Key: protoPk.Ed25519,
		}, nil
	default:
		return nil, sdkerrors.Wrapf(sdkerrors.ErrInvalidType, "cannot convert %v from Tendermint public key", protoPk)
	}
}

// ToTmProtoPublicKey converts our own PubKey to TM's pb.PublicKey.
func ToTmProtoPublicKey(pk cryptotypes.PubKey) (pb.PublicKey, error) {
	switch pk := pk.(type) {
	case *ed25519.PubKey:
		return pb.PublicKey{
			Sum: &pb.PublicKey_Ed25519{
				Ed25519: pk.Key,
			},
		}, nil
	case *secp256k1.PubKey:
		return pb.PublicKey{}, sdkerrors.Wrapf(sdkerrors.ErrNotSupported, "secp256k1 consensus keys are not supported")
	default:
		return pb.PublicKey{}, sdkerrors.Wrapf(sdkerrors.ErrInvalidType, "cannot convert %v to Tendermint public key", pk)
	}
}

// FromTmPubKeyInterface converts TM's crypto.PubKey to our own PubKey.
func FromTmPubKeyInterface(tmPk crypto.PubKey) (cryptotypes.PubKey, error) {
	return FromTmProtoPublicKey(crypto.PubKeyToProto(tmPk))
}

// ToTmPubKeyInterface converts our own PubKey to TM's crypto.PubKey.
func ToTmPubKeyInterface(pk cryptotypes.PubKey) (crypto.PubKey, error) {
	tmProtoPk, err := ToTmProtoPublicKey(pk)
	if err != nil {
		return crypto.PubKey{}, err
	}

	return crypto.PubKeyFromProto(tmProtoPk)
}
