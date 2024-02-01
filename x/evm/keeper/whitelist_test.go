package keeper_test

import (
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/sei-protocol/sei-chain/testutil/keeper"
	"github.com/stretchr/testify/require"
)

func TestBankSendWhitelist(t *testing.T) {
	k, ctx := keeper.MockEVMKeeper()
	wl := k.GetCodeHashWhitelistedForBankSend(ctx)
	require.Empty(t, wl.Hashes)
	k.AddCodeHashWhitelistedForBankSend(ctx, common.BytesToHash([]byte("a")))
	require.True(t, k.IsCodeHashWhitelistedForBankSend(ctx, common.BytesToHash([]byte("a"))))
	require.False(t, k.IsCodeHashWhitelistedForBankSend(ctx, common.BytesToHash([]byte("b"))))
}

func TestDelegateCallWhitelist(t *testing.T) {
	k, ctx := keeper.MockEVMKeeper()
	wl := k.GetCodeHashWhitelistedForDelegateCall(ctx)
	require.Empty(t, wl.Hashes)
	k.AddCodeHashWhitelistedForDelegateCall(ctx, common.BytesToHash([]byte("a")))
	require.True(t, k.IsCodeHashWhitelistedForDelegateCall(ctx, common.BytesToHash([]byte("a"))))
	require.False(t, k.IsCodeHashWhitelistedForDelegateCall(ctx, common.BytesToHash([]byte("b"))))
}
