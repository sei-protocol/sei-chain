package types_test

import (
	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"

	"github.com/cosmos/ibc-go/v3/modules/apps/27-interchain-accounts/types"
	"github.com/cosmos/ibc-go/v3/testing/simapp"
)

// caseRawBytes defines a helper struct, used for testing codec operations
type caseRawBytes struct {
	name    string
	bz      []byte
	expPass bool
}

// mockSdkMsg defines a mock struct, used for testing codec error scenarios
type mockSdkMsg struct{}

// Reset implements sdk.Msg
func (mockSdkMsg) Reset() {
}

// String implements sdk.Msg
func (mockSdkMsg) String() string {
	return ""
}

// ProtoMessage implements sdk.Msg
func (mockSdkMsg) ProtoMessage() {
}

// ValidateBasic implements sdk.Msg
func (mockSdkMsg) ValidateBasic() error {
	return nil
}

// GetSigners implements sdk.Msg
func (mockSdkMsg) GetSigners() []sdk.AccAddress {
	return []sdk.AccAddress{}
}

func (suite *TypesTestSuite) TestSerializeAndDeserializeCosmosTx() {
	testCases := []struct {
		name    string
		msgs    []sdk.Msg
		expPass bool
	}{
		{
			"single msg",
			[]sdk.Msg{
				&banktypes.MsgSend{
					FromAddress: TestOwnerAddress,
					ToAddress:   TestOwnerAddress,
					Amount:      sdk.NewCoins(sdk.NewCoin("bananas", sdk.NewInt(100))),
				},
			},
			true,
		},
		{
			"multiple msgs, same types",
			[]sdk.Msg{
				&banktypes.MsgSend{
					FromAddress: TestOwnerAddress,
					ToAddress:   TestOwnerAddress,
					Amount:      sdk.NewCoins(sdk.NewCoin("bananas", sdk.NewInt(100))),
				},
				&banktypes.MsgSend{
					FromAddress: TestOwnerAddress,
					ToAddress:   TestOwnerAddress,
					Amount:      sdk.NewCoins(sdk.NewCoin("bananas", sdk.NewInt(200))),
				},
			},
			true,
		},
		{
			"multiple msgs, different types",
			[]sdk.Msg{
				&banktypes.MsgSend{
					FromAddress: TestOwnerAddress,
					ToAddress:   TestOwnerAddress,
					Amount:      sdk.NewCoins(sdk.NewCoin("bananas", sdk.NewInt(100))),
				},
				&govtypes.MsgSubmitProposal{
					InitialDeposit: sdk.NewCoins(sdk.NewCoin("bananas", sdk.NewInt(100))),
					Proposer:       TestOwnerAddress,
				},
			},
			true,
		},
		{
			"unregistered msg type",
			[]sdk.Msg{
				&mockSdkMsg{},
			},
			false,
		},
		{
			"multiple unregistered msg types",
			[]sdk.Msg{
				&mockSdkMsg{},
				&mockSdkMsg{},
				&mockSdkMsg{},
			},
			false,
		},
	}

	testCasesAny := []caseRawBytes{}

	for _, tc := range testCases {
		bz, err := types.SerializeCosmosTx(simapp.MakeTestEncodingConfig().Marshaler, tc.msgs)
		suite.Require().NoError(err, tc.name)

		testCasesAny = append(testCasesAny, caseRawBytes{tc.name, bz, tc.expPass})
	}

	for i, tc := range testCasesAny {
		msgs, err := types.DeserializeCosmosTx(simapp.MakeTestEncodingConfig().Marshaler, tc.bz)
		if tc.expPass {
			suite.Require().NoError(err, tc.name)
			suite.Require().Equal(testCases[i].msgs, msgs, tc.name)
		} else {
			suite.Require().Error(err, tc.name)
		}
	}

	// test deserializing unknown bytes
	msgs, err := types.DeserializeCosmosTx(simapp.MakeTestEncodingConfig().Marshaler, []byte("invalid"))
	suite.Require().Error(err)
	suite.Require().Empty(msgs)
}

// unregistered bytes causes amino to panic.
// test that DeserializeCosmosTx gracefully returns an error on
// unsupported amino codec.
func (suite *TypesTestSuite) TestDeserializeAndSerializeCosmosTxWithAmino() {
	cdc := codec.NewLegacyAmino()
	marshaler := codec.NewAminoCodec(cdc)

	msgs, err := types.SerializeCosmosTx(marshaler, []sdk.Msg{&banktypes.MsgSend{}})
	suite.Require().Error(err)
	suite.Require().Empty(msgs)

	bz, err := types.DeserializeCosmosTx(marshaler, []byte{0x10, 0})
	suite.Require().Error(err)
	suite.Require().Empty(bz)

}
