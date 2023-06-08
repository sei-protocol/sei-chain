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

func TestOrders(t *testing.T) {
	app := processblock.NewTestApp()
	processblock.CommonPreset(app)
	admin := app.NewSignableAccount("admin")
	app.FundAccount(admin, 100000000)
	alice := app.NewSignableAccount("alice")
	app.FundAccount(alice, 1000000)
	contract := app.NewContract(admin, "./mars.wasm")

	market := msgs.NewMarket(contract.String(), "SEI", "ATOM")

	registerTx := app.Sign(admin, market.Register(admin, []string{}, 20000000), 1000000)
	block1 := []signing.Tx{registerTx}
	require.Equal(t, []uint32{0}, app.RunBlock(block1)) // 1st block to register contract/pair

	aliceLimitOrder := market.LongLimitOrder(alice, "10.5", "5")
	msgs := []sdk.Msg{aliceLimitOrder}
	tx := app.Sign(alice, msgs, 10000)

	block2 := []signing.Tx{tx}
	blockRunner := func() []uint32 { return app.RunBlock(block2) } // 2nd block to place order
	blockRunner = verify.DexOrders(t, app, blockRunner, block2)
	blockRunner = verify.Balance(t, app, blockRunner, block2)

	require.Equal(t, []uint32{0}, blockRunner())
}
