package keeper_test

import (
	"bytes"
	"testing"

	testkeeper "github.com/sei-protocol/sei-chain/giga/deps/testutil/keeper"
	"github.com/sei-protocol/sei-chain/giga/deps/xevm/keeper"
	"github.com/stretchr/testify/require"
)

func TestInitGenesis(t *testing.T) {
	k := &testkeeper.EVMTestApp.GigaEvmKeeper
	ctx := testkeeper.EVMTestApp.GetContextForDeliverTx([]byte{})
	// coinbase address must be associated
	coinbaseSeiAddr, associated := k.GetSeiAddress(ctx, keeper.GetCoinbaseAddress())
	require.True(t, associated)
	require.True(t, bytes.Equal(coinbaseSeiAddr, k.AccountKeeper().GetModuleAddress("fee_collector")))
}
