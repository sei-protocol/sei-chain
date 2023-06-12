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

func TestDistribution(t *testing.T) {
	app := processblock.NewTestApp()
	processblock.CommonPreset(app)
	signer1 := app.NewSignableAccount("signer1")
	app.FundAccount(signer1, 100000000)
	alice := app.NewAccount()

	sendAliceMsg := msgs.Send(signer1, alice, 1000)
	tx1 := app.Sign(signer1, []sdk.Msg{sendAliceMsg}, 20000)

	// block T (no distribution yet since this is the first block)
	block := []signing.Tx{tx1}
	blockRunner := func() []uint32 { return app.RunBlock(block) }
	require.Equal(t, []uint32{0}, blockRunner())

	// block T+1 (distribution of fees from T)
	blockRunner = func() []uint32 { return app.RunBlock([]signing.Tx{}) }
	blockRunner = verify.Allocation(t, app, blockRunner)

	require.Equal(t, []uint32{}, blockRunner())
}
