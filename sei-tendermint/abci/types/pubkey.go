package types

import (
	fmt "fmt"

	"github.com/tendermint/tendermint/crypto/ed25519"
	"github.com/tendermint/tendermint/crypto/encoding"
)

func Ed25519ValidatorUpdate(pk []byte, power int64) ValidatorUpdate {
	pke := ed25519.PubKey(pk)

	pkp, err := encoding.PubKeyToProto(pke)
	if err != nil {
		panic(err)
	}

	return ValidatorUpdate{
		PubKey: pkp,
		Power:  power,
	}
}

func UpdateValidator(pk []byte, power int64, keyType string) ValidatorUpdate {
	if keyType != "" && keyType != ed25519.KeyType {
		panic(fmt.Sprintf("key type %s not supported", keyType))
	}
	return Ed25519ValidatorUpdate(pk, power)
}
