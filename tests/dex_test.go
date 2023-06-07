package tests

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
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
	registerContractMsg, registerPairMsg := market.Register(admin, []string{}, 20000000)
	registerTx := app.Sign(admin, []sdk.Msg{registerContractMsg, registerPairMsg}, 1000000)
	require.Equal(t, []uint32{0}, app.RunBlock([][]byte{registerTx}))

	aliceLimitOrder := market.LongLimitOrder(alice, "10.5", "5")
	msgs := []sdk.Msg{aliceLimitOrder}
	tx := app.Sign(alice, msgs, 10000)

	blockRunner := func() []uint32 { return app.RunBlock([][]byte{tx}) }
	blockRunner = verify.DexOrders(t, app.Ctx(), &app.DexKeeper, blockRunner, msgs)

	require.Equal(t, []uint32{0}, blockRunner())
}
