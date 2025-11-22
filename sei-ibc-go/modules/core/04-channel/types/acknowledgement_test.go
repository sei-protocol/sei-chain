package types_test

import "github.com/cosmos/ibc-go/v3/modules/core/04-channel/types"

// tests acknowledgement.ValidateBasic and acknowledgement.GetBytes
func (suite TypesTestSuite) TestAcknowledgement() {
	testCases := []struct {
		name       string
		ack        types.Acknowledgement
		expSuccess bool // indicate if this is a success or failed ack
		expPass    bool
	}{
		{
			"valid successful ack",
			types.NewResultAcknowledgement([]byte("success")),
			true,
			true,
		},
		{
			"valid failed ack",
			types.NewErrorAcknowledgement("error"),
			false,
			true,
		},
		{
			"empty successful ack",
			types.NewResultAcknowledgement([]byte{}),
			true,
			false,
		},
		{
			"empty faied ack",
			types.NewErrorAcknowledgement("  "),
			false,
			false,
		},
		{
			"nil response",
			types.Acknowledgement{
				Response: nil,
			},
			false,
			false,
		},
	}

	for _, tc := range testCases {
		tc := tc

		suite.Run(tc.name, func() {
			suite.SetupTest()

			err := tc.ack.ValidateBasic()

			if tc.expPass {
				suite.Require().NoError(err)
			} else {
				suite.Require().Error(err)
			}

			// expect all acks to be able to be marshaled
			suite.NotPanics(func() {
				bz := tc.ack.Acknowledgement()
				suite.Require().NotNil(bz)
			})

			suite.Require().Equal(tc.expSuccess, tc.ack.Success())
		})
	}
}
