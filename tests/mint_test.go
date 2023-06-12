package tests

import (
	"testing"
	"time"

	"github.com/cosmos/cosmos-sdk/x/auth/signing"
	"github.com/sei-protocol/sei-chain/testutil/processblock"
	"github.com/sei-protocol/sei-chain/testutil/processblock/verify"
	"github.com/stretchr/testify/require"
)

func TestMint(t *testing.T) {
	app := processblock.NewTestApp()
	processblock.CommonPreset(app)
	app.NewMinter(1000000)
	app.FastEpoch()

	blockRunner := func() []uint32 { return app.RunBlock([]signing.Tx{}) }
	blockRunner = verify.MintRelease(t, app, blockRunner)

	require.Equal(t, []uint32{}, blockRunner())

	time.Sleep(6 * time.Second)

	blockRunner = func() []uint32 { return app.RunBlock([]signing.Tx{}) }
	blockRunner = verify.MintRelease(t, app, blockRunner)

	require.Equal(t, []uint32{}, blockRunner())
}
