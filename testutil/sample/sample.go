package sample

import (
	"github.com/cosmos/cosmos-sdk/crypto/keys/ed25519"
	seitypes "github.com/sei-protocol/sei-chain/types"
)

// AccAddress returns a sample account address
func AccAddress() string {
	pk := ed25519.GenPrivKey().PubKey()
	addr := pk.Address()
	return seitypes.AccAddress(addr).String()
}
