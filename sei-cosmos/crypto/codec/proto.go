package codec

import (
	codectypes "github.com/sei-protocol/sei-chain/sei-cosmos/codec/types"
	"github.com/sei-protocol/sei-chain/sei-cosmos/crypto/keys/ed25519"
	"github.com/sei-protocol/sei-chain/sei-cosmos/crypto/keys/multisig"
	"github.com/sei-protocol/sei-chain/sei-cosmos/crypto/keys/secp256k1"
	"github.com/sei-protocol/sei-chain/sei-cosmos/crypto/keys/secp256r1"
	"github.com/sei-protocol/sei-chain/sei-cosmos/crypto/keys/sr25519"
	cryptotypes "github.com/sei-protocol/sei-chain/sei-cosmos/crypto/types"
)

// RegisterInterfaces registers the sdk.Tx interface.
func RegisterInterfaces(registry codectypes.InterfaceRegistry) {
	var pk *cryptotypes.PubKey
	registry.RegisterInterface("cosmos.crypto.PubKey", pk)
	registry.RegisterImplementations(pk, &ed25519.PubKey{})
	registry.RegisterImplementations(pk, &secp256k1.PubKey{})
	registry.RegisterImplementations(pk, &multisig.LegacyAminoPubKey{})
	registry.RegisterImplementations(pk, &sr25519.PubKey{})
	secp256r1.RegisterInterfaces(registry)
}
