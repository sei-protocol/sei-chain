package tests

import (
	"testing"

	"github.com/cosmos/cosmos-sdk/x/auth/signing"
	"github.com/sei-protocol/sei-chain/testutil/processblock"
	"github.com/sei-protocol/sei-chain/testutil/processblock/msgs"
	"github.com/sei-protocol/sei-chain/testutil/processblock/verify"
)

func TestDistribution(t *testing.T) {
	app := processblock.NewTestApp()
	p := processblock.CommonPreset(app)
	for _, testCase := range []TestCase{
		{
			description: "send to accrue fee for next block",
			input: []signing.Tx{
				p.AdminSign(app, msgs.Send(p.Admin, p.AllAccounts[0], 1000)),
			},
			verifier:      []verify.Verifier{},
			expectedCodes: []uint32{0},
		},
		{
			description: "check distribution",
			input:       []signing.Tx{},
			verifier: []verify.Verifier{
				verify.Allocation,
			},
			expectedCodes: []uint32{},
		},
	} {
		testCase.run(t, app)
	}
}
