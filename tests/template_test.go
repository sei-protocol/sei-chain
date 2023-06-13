package tests

import (
	"testing"

	"github.com/cosmos/cosmos-sdk/x/auth/signing"
	"github.com/sei-protocol/sei-chain/testutil/processblock"
	"github.com/sei-protocol/sei-chain/testutil/processblock/msgs"
	"github.com/sei-protocol/sei-chain/testutil/processblock/verify"
	"github.com/stretchr/testify/require"
)

type TestCase struct {
	description   string
	input         []signing.Tx
	verifier      []verify.Verifier
	expectedCodes []uint32
}

func (c *TestCase) run(t *testing.T, app *processblock.App) {
	blockRunner := func() []uint32 { return app.RunBlock(c.input) }
	for _, v := range c.verifier {
		blockRunner = v(t, app, blockRunner, c.input)
	}
	require.Equal(t, c.expectedCodes, blockRunner(), c.description)
}

func TestTemplate(t *testing.T) {
	app := processblock.NewTestApp()
	p := processblock.CommonPreset(app) // choose a preset
	for _, testCase := range []TestCase{
		{
			description: "simple send 1",
			input: []signing.Tx{
				p.AdminSign(app, msgs.Send(p.Admin, p.AllAccounts[0], 1000)),
			},
			verifier: []verify.Verifier{
				verify.Balance,
			},
			expectedCodes: []uint32{0},
		},
		{
			description: "simple send 2",
			input: []signing.Tx{
				p.AdminSign(app, msgs.Send(p.Admin, p.AllAccounts[1], 2000)),
				p.AdminSign(app, msgs.Send(p.Admin, p.AllAccounts[2], 3000)),
			},
			verifier: []verify.Verifier{
				verify.Balance,
			},
			expectedCodes: []uint32{0, 0},
		},
	} {
		testCase.run(t, app)
	}
}
