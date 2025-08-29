package tests

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/x/auth/signing"
	"github.com/sei-protocol/sei-chain/testutil/processblock"
	"github.com/sei-protocol/sei-chain/testutil/processblock/verify"
	tokenfactorytypes "github.com/sei-protocol/sei-chain/x/tokenfactory/types"
	"github.com/stretchr/testify/require"
)

func TestTokenFactoryMintBurnBalance(t *testing.T) {
	app := processblock.NewTestApp()
	p := processblock.CommonPreset(app)

	denom, err := tokenfactorytypes.GetTokenDenom(p.Admin.String(), "tf")
	require.NoError(t, err)

	txs := []signing.Tx{
		p.AdminSign(app, tokenfactorytypes.NewMsgCreateDenom(p.Admin.String(), "tf")),
		p.AdminSign(app, tokenfactorytypes.NewMsgMint(p.Admin.String(), sdk.NewCoin(denom, sdk.NewInt(1000)))),
		p.AdminSign(app, tokenfactorytypes.NewMsgBurn(p.Admin.String(), sdk.NewCoin(denom, sdk.NewInt(400)))),
	}

	blockRunner := func() []uint32 { return app.RunBlock(txs) }
	blockRunner = verify.Balance(t, app, blockRunner, txs)
	require.Equal(t, []uint32{0, 0, 0}, blockRunner())
}
