package sample

import (
	"github.com/sei-protocol/sei-chain/sei-cosmos/crypto/keys/ed25519"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
)

// AccAddress returns a sample account address
func AccAddress() string {
	pk := ed25519.GenPrivKey().PubKey()
	addr := pk.Address()
	return sdk.AccAddress(addr).String()
}
