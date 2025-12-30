package types

import (
	"github.com/tendermint/tendermint/crypto"
	"github.com/tendermint/tendermint/crypto/encoding"
)

func UpdateValidator(pk crypto.PubKey, power int64, keyType string) ValidatorUpdate {
	return ValidatorUpdate{
		PubKey: encoding.PubKeyToProto(pk),
		Power:  power,
	}
}
