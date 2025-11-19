package testdata

import (
	"encoding/json"

	seitypes "github.com/sei-protocol/sei-chain/types"
	"github.com/stretchr/testify/require"

	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256r1"
	cryptotypes "github.com/cosmos/cosmos-sdk/crypto/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// KeyTestPubAddr generates a new secp256k1 keypair.
func KeyTestPubAddr() (cryptotypes.PrivKey, cryptotypes.PubKey, seitypes.AccAddress) {
	key := secp256k1.GenPrivKey()
	pub := key.PubKey()
	addr := seitypes.AccAddress(pub.Address())
	return key, pub, addr
}

// KeyTestPubAddr generates a new secp256r1 keypair.
func KeyTestPubAddrSecp256R1(require *require.Assertions) (cryptotypes.PrivKey, cryptotypes.PubKey, seitypes.AccAddress) {
	key, err := secp256r1.GenPrivKey()
	require.NoError(err)
	pub := key.PubKey()
	addr := seitypes.AccAddress(pub.Address())
	return key, pub, addr
}

// NewTestFeeAmount is a test fee amount.
func NewTestFeeAmount() sdk.Coins {
	return sdk.NewCoins(sdk.NewInt64Coin("usei", 150))
}

// NewTestGasLimit is a test fee gas limit.
func NewTestGasLimit() uint64 {
	return 200000
}

// NewTestMsg creates a message for testing with the given signers.
func NewTestMsg(addrs ...seitypes.AccAddress) *TestMsg {
	var accAddresses []string

	for _, addr := range addrs {
		accAddresses = append(accAddresses, addr.String())
	}

	return &TestMsg{
		Signers: accAddresses,
	}
}

var _ seitypes.Msg = (*TestMsg)(nil)

func (msg *TestMsg) Route() string { return "TestMsg" }
func (msg *TestMsg) Type() string  { return "Test message" }
func (msg *TestMsg) GetSignBytes() []byte {
	bz, err := json.Marshal(msg.Signers)
	if err != nil {
		panic(err)
	}
	return sdk.MustSortJSON(bz)
}
func (msg *TestMsg) GetSigners() []seitypes.AccAddress {
	addrs := make([]seitypes.AccAddress, len(msg.Signers))
	for i, in := range msg.Signers {
		addr, err := seitypes.AccAddressFromBech32(in)
		if err != nil {
			panic(err)
		}

		addrs[i] = addr
	}

	return addrs
}
func (msg *TestMsg) ValidateBasic() error { return nil }

var _ seitypes.Msg = &MsgCreateDog{}

func (msg *MsgCreateDog) GetSigners() []seitypes.AccAddress { return []seitypes.AccAddress{} }
func (msg *MsgCreateDog) ValidateBasic() error              { return nil }
