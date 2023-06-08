package tests

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/x/auth/signing"
	"github.com/sei-protocol/sei-chain/testutil/processblock"
	"github.com/sei-protocol/sei-chain/testutil/processblock/msgs"
	"github.com/sei-protocol/sei-chain/testutil/processblock/verify"
	"github.com/stretchr/testify/require"
)

func TestTemplate(t *testing.T) {
	// Phase One: Initialize application state (aka genesis).
	app := processblock.NewTestApp()
	// initialize state with a preset (aka common default states like validators).
	processblock.CommonPreset(app)

	// initialize all accounts that would need to sign transactions in this test.
	signer1 := app.NewSignableAccount("signer1")
	// signer2 := app.NewSignableAccount("signer2")

	// initialize custom state for the contract with `app` methods defined in
	// testutil/processblock/genesis*.go files.
	app.FundAccount(signer1, 100000000)
	alice := app.NewAccount()
	app.FundAccountWithDenom(alice, 100000, "customtoken")
	bob := app.NewAccount()
	charlie := app.NewAccount()
	// contract := app.NewContract(admin, "./mars.wasm")
	// End Phase One

	// Phase Two: Create and sign transactions
	// create messages with helpers defined in testutil/processblock/msgs, or
	// directly with module's own helper if it's easier.
	sendAliceMsg := msgs.Send(signer1, alice, 1000)
	sendBobMsg := msgs.Send(signer1, bob, 2000)
	sendCharlieMsg := msgs.Send(signer1, charlie, 3000)
	// group messages in transactions and sign
	tx1 := app.Sign(signer1, []sdk.Msg{sendAliceMsg, sendBobMsg}, 20000) // estimate a gas fee
	tx2 := app.Sign(signer1, []sdk.Msg{sendCharlieMsg}, 10000)

	// Phase Three: Run block and verify
	// group transactions in a block.
	block := []signing.Tx{tx1, tx2}
	blockRunner := func() []uint32 { return app.RunBlock(block) } // block runnable with no verifier
	blockRunner = verify.Balance(t, app, blockRunner, block)      // block runnable with balance verifier
	// more verifiers can be chained on blockRunner like how balance verifier is chained above.
	// all verifiers can be found in testutil/processblock/verify.

	require.Equal(t, []uint32{0, 0}, blockRunner()) // actually process the block and verify result code of the transactions

	// Phase Two and Three can be repeated multiple times to emulate processing multiple blocks
}
