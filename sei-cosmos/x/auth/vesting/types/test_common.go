package types

import (
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	cryptotypes "github.com/cosmos/cosmos-sdk/crypto/types"
	"github.com/cosmos/cosmos-sdk/testutil/testdata"

	sdk "github.com/cosmos/cosmos-sdk/types"
	seitypes "github.com/sei-protocol/sei-chain/types"
)

// NewTestMsg generates a test message
func NewTestMsg(addrs ...seitypes.AccAddress) *testdata.TestMsg {
	return testdata.NewTestMsg(addrs...)
}

// NewTestCoins coins to more than cover the fee
func NewTestCoins() sdk.Coins {
	return sdk.Coins{
		sdk.NewInt64Coin("atom", 10000000),
	}
}

// KeyTestPubAddr generates a test key pair
func KeyTestPubAddr() (cryptotypes.PrivKey, cryptotypes.PubKey, seitypes.AccAddress) {
	key := secp256k1.GenPrivKey()
	pub := key.PubKey()
	addr := seitypes.AccAddress(pub.Address())
	return key, pub, addr
}
