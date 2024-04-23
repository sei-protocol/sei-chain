package keeper_test

import (
	"testing"

	keepertest "github.com/sei-protocol/sei-chain/testutil/keeper"
	"github.com/stretchr/testify/require"
)

func TestWhitelist(t *testing.T) {
	k, ctx := keepertest.MockEVMKeeper()
	require.True(t, k.IsCWCodeHashWhitelistedForEVMDelegateCall(ctx, k.WhitelistedCwCodeHashesForDelegateCall(ctx)[0]))
	require.False(t, k.IsCWCodeHashWhitelistedForEVMDelegateCall(ctx, []byte("1")))
}
