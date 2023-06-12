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
	app.FundAccount(alice, 10000000)
	bob := app.NewSignableAccount("bob")
	app.FundAccount(bob, 10000000)
	charlie := app.NewSignableAccount("charlie")
	app.FundAccount(charlie, 10000000)
	contract := app.NewContract(admin, "./mars.wasm")

	market := msgs.NewMarket(contract.String(), "SEI", "ATOM")
	newContract := app.NewContract(admin, "./mars.wasm")

	registerTx := app.Sign(admin, market.Register(admin, []string{}, 20000000), 1000000)
	block1 := []signing.Tx{registerTx}
	require.Equal(t, []uint32{0}, app.RunBlock(block1)) // 1st block to register contract/pair

	aliceLimitOrder := market.LongLimitOrder(alice, "10.5", "5")
	m := []sdk.Msg{aliceLimitOrder}
	tx := app.Sign(alice, m, 10000)

	block2 := []signing.Tx{tx}
	blockRunner := func() []uint32 { return app.RunBlock(block2) } // 2nd block to place the first limit order
	blockRunner = verify.DexOrders(t, app, blockRunner, block2)
	blockRunner = verify.Balance(t, app, blockRunner, block2)

	require.Equal(t, []uint32{0}, blockRunner())

	newMarket := msgs.NewMarket(newContract.String(), "USDC", "BTC")
	bobMarketOrder := market.ShortMarketOrder(bob, "10", "2")
	charlieLimitOrder := market.ShortLimitOrder(charlie, "11", "3")
	block3 := []signing.Tx{
		app.Sign(admin, newMarket.Register(admin, []string{}, 20000000), 1000000),
		app.Sign(bob, []sdk.Msg{bobMarketOrder}, 10000),
		app.Sign(charlie, []sdk.Msg{charlieLimitOrder}, 10000),
	}
	blockRunner = func() []uint32 { return app.RunBlock(block3) } // 2nd block to place more orders
	blockRunner = verify.DexOrders(t, app, blockRunner, block3)
	blockRunner = verify.Balance(t, app, blockRunner, block3)

	require.Equal(t, []uint32{0, 0, 0}, blockRunner())

	aliceLimitOrder = newMarket.ShortLimitOrder(alice, "100", "50")
	bobLimitOrder := market.LongLimitOrder(bob, "11", "1")
	charlieMarketOrder := market.LongMarketOrder(charlie, "12", "4")
	block4 := []signing.Tx{
		app.Sign(alice, []sdk.Msg{aliceLimitOrder}, 10000),
		app.Sign(bob, []sdk.Msg{bobLimitOrder}, 10000),
		app.Sign(charlie, []sdk.Msg{charlieMarketOrder}, 10000),
	}
	blockRunner = func() []uint32 { return app.RunBlock(block4) } // 2nd block to place more orders
	blockRunner = verify.DexOrders(t, app, blockRunner, block4)
	blockRunner = verify.Balance(t, app, blockRunner, block4)

	require.Equal(t, []uint32{0, 0, 0}, blockRunner())
}
