package keeper_test

import (
	"testing"

	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/params"
	"github.com/sei-protocol/sei-chain/testutil/keeper"
	"github.com/stretchr/testify/require"
)

// TestGetCodeSizeMatchesGetCodeLength pins the invariant that go-ethereum's
// EIP-7702 CALL-family size gate relies on: GetCodeSize(addr) == len(GetCode(addr)).
// This holds on Sei's keeper without bumping go-ethereum.
func TestGetCodeSizeMatchesGetCodeLength(t *testing.T) {
	k := &keeper.EVMTestApp.EvmKeeper
	ctx := keeper.EVMTestApp.GetContextForDeliverTx([]byte{})
	_, addr := keeper.MockAddressPair()

	assertInvariant := func(t *testing.T) {
		t.Helper()
		code := k.GetCode(ctx, addr)
		require.Equal(t, len(code), k.GetCodeSize(ctx, addr), "GetCodeSize must equal len(GetCode)")
	}

	t.Run("empty account", func(t *testing.T) {
		assertInvariant(t)
		require.Equal(t, 0, k.GetCodeSize(ctx, addr))
	})

	t.Run("nil SetCode", func(t *testing.T) {
		k.SetCode(ctx, addr, nil)
		assertInvariant(t)
		require.Nil(t, k.GetCode(ctx, addr))
		require.Equal(t, 0, k.GetCodeSize(ctx, addr))
		require.Equal(t, ethtypes.EmptyCodeHash, k.GetCodeHash(ctx, addr))
	})

	t.Run("small code", func(t *testing.T) {
		k.SetCode(ctx, addr, []byte{1, 2, 3, 4, 5})
		assertInvariant(t)
	})

	t.Run("eip7702 designator length", func(t *testing.T) {
		designator := ethtypes.AddressToDelegation(common.BytesToAddress([]byte("delegate-target")))
		require.Equal(t, 23, len(designator))
		_, ok := ethtypes.ParseDelegation(designator)
		require.True(t, ok)
		k.SetCode(ctx, addr, designator)
		assertInvariant(t)
		require.Equal(t, 23, k.GetCodeSize(ctx, addr))
	})

	t.Run("max code size", func(t *testing.T) {
		code := make([]byte, params.MaxCodeSize)
		k.SetCode(ctx, addr, code)
		assertInvariant(t)
		require.Equal(t, params.MaxCodeSize, k.GetCodeSize(ctx, addr))
	})

	t.Run("overwrite shrinks size metadata", func(t *testing.T) {
		k.SetCode(ctx, addr, []byte{9})
		assertInvariant(t)
		require.Equal(t, 1, k.GetCodeSize(ctx, addr))
	})

	t.Run("empty slice clears to zero size", func(t *testing.T) {
		k.SetCode(ctx, addr, []byte{})
		assertInvariant(t)
		require.Equal(t, 0, k.GetCodeSize(ctx, addr))
	})
}
