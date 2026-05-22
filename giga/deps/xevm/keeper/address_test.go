package keeper_test

import (
	"testing"

	"github.com/sei-protocol/sei-chain/giga/deps/testutil/keeper"
	evmkeeper "github.com/sei-protocol/sei-chain/giga/deps/xevm/keeper"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/stretchr/testify/require"
)

func TestSetGetAddressMapping(t *testing.T) {
	k, ctx := keeper.MockEVMKeeper(t)
	seiAddr, evmAddr := keeper.MockAddressPair()

	// Before the mapping is set, neither direction resolves.
	_, ok := k.GetEVMAddress(ctx, seiAddr)
	require.False(t, ok)
	_, ok = k.GetSeiAddress(ctx, evmAddr)
	require.False(t, ok)

	k.SetAddressMapping(ctx, seiAddr, evmAddr)

	// Both directions now resolve, and the underlying Sei account exists.
	foundEVM, ok := k.GetEVMAddress(ctx, seiAddr)
	require.True(t, ok)
	require.Equal(t, evmAddr, foundEVM)
	foundSei, ok := k.GetSeiAddress(ctx, evmAddr)
	require.True(t, ok)
	require.Equal(t, seiAddr, foundSei)
	require.Equal(t, seiAddr, k.AccountKeeper().GetAccount(ctx, seiAddr).GetAddress())
}

func TestSetAddressMappingReplacesExistingIndexes(t *testing.T) {
	// Re-binding either side of the mapping must clear the OLD reverse
	// index, not just overwrite the forward one — otherwise stale entries
	// would let an old address still resolve to its former partner.

	t.Run("rebinding evm address to a new sei address", func(t *testing.T) {
		k, ctx := keeper.MockEVMKeeper(t)
		oldSeiAddr, evmAddr := keeper.MockAddressPair()
		newSeiAddr, _ := keeper.MockAddressPair()

		k.SetAddressMapping(ctx, oldSeiAddr, evmAddr)
		k.SetAddressMapping(ctx, newSeiAddr, evmAddr)

		_, ok := k.GetEVMAddress(ctx, oldSeiAddr)
		require.False(t, ok, "old sei address must no longer map to evm address")
		foundEVM, ok := k.GetEVMAddress(ctx, newSeiAddr)
		require.True(t, ok)
		require.Equal(t, evmAddr, foundEVM)
		foundSei, ok := k.GetSeiAddress(ctx, evmAddr)
		require.True(t, ok)
		require.Equal(t, newSeiAddr, foundSei)
	})

	t.Run("rebinding sei address to a new evm address", func(t *testing.T) {
		k, ctx := keeper.MockEVMKeeper(t)
		seiAddr, oldEvmAddr := keeper.MockAddressPair()
		_, newEvmAddr := keeper.MockAddressPair()

		k.SetAddressMapping(ctx, seiAddr, oldEvmAddr)
		k.SetAddressMapping(ctx, seiAddr, newEvmAddr)

		_, ok := k.GetSeiAddress(ctx, oldEvmAddr)
		require.False(t, ok, "old evm address must no longer map to sei address")
		foundEVM, ok := k.GetEVMAddress(ctx, seiAddr)
		require.True(t, ok)
		require.Equal(t, newEvmAddr, foundEVM)
		foundSei, ok := k.GetSeiAddress(ctx, newEvmAddr)
		require.True(t, ok)
		require.Equal(t, seiAddr, foundSei)
	})
}

func TestDeleteAddressMapping(t *testing.T) {
	k, ctx := keeper.MockEVMKeeper(t)
	seiAddr, evmAddr := keeper.MockAddressPair()

	k.SetAddressMapping(ctx, seiAddr, evmAddr)
	foundEVM, ok := k.GetEVMAddress(ctx, seiAddr)
	require.True(t, ok)
	require.Equal(t, evmAddr, foundEVM)
	foundSei, ok := k.GetSeiAddress(ctx, evmAddr)
	require.True(t, ok)
	require.Equal(t, seiAddr, foundSei)

	// Deletion must clear both directions of the index.
	k.DeleteAddressMapping(ctx, seiAddr, evmAddr)
	_, ok = k.GetEVMAddress(ctx, seiAddr)
	require.False(t, ok)
	_, ok = k.GetSeiAddress(ctx, evmAddr)
	require.False(t, ok)
}

func TestGetAddressOrDefault(t *testing.T) {
	k, ctx := keeper.MockEVMKeeper(t)
	seiAddr, evmAddr := keeper.MockAddressPair()

	// With no mapping set, the defaults are the raw byte cast in each
	// direction: the Sei address bytes become the EVM address, and the
	// EVM address bytes become the Sei address.
	defaultEvmAddr := k.GetEVMAddressOrDefault(ctx, seiAddr)
	require.Equal(t, seiAddr.Bytes(), defaultEvmAddr[:])
	defaultSeiAddr := k.GetSeiAddressOrDefault(ctx, evmAddr)
	require.Equal(t, sdk.AccAddress(evmAddr[:]), defaultSeiAddr)
}

func TestSendingToCastAddress(t *testing.T) {
	a, ctx := keeper.MockApp(t)
	seiAddr, evmAddr := keeper.MockAddressPair()
	castAddr := sdk.AccAddress(evmAddr[:])
	sourceAddr, _ := keeper.MockAddressPair()

	// Fund the evm module and a source account.
	require.NoError(t, a.BankKeeper.MintCoins(ctx, evmModule,
		sdk.NewCoins(sdk.NewCoin(baseDenom, sdk.NewInt(10)))))
	require.NoError(t, a.BankKeeper.SendCoinsFromModuleToAccount(ctx, evmModule, sourceAddr,
		sdk.NewCoins(sdk.NewCoin(baseDenom, sdk.NewInt(5)))))

	amt := sdk.NewCoins(sdk.NewCoin(baseDenom, sdk.NewInt(1)))

	// Before any mapping exists, the bech32 cast of an EVM address is just
	// an unowned account — bank can send to it freely.
	require.NoError(t, a.BankKeeper.SendCoinsFromModuleToAccount(ctx, evmModule, castAddr, amt))
	require.NoError(t, a.BankKeeper.SendCoins(ctx, sourceAddr, castAddr, amt))
	require.NoError(t, a.BankKeeper.SendCoinsAndWei(ctx, sourceAddr, castAddr, sdk.OneInt(), sdk.OneInt()))

	// Once the EVM address is associated with a real Sei address, sending
	// to the cast form MUST be rejected. Otherwise a sender could bypass
	// the mapping by addressing the unmapped cast directly and stranding
	// funds outside the associated Sei account.
	a.EvmKeeper.SetAddressMapping(ctx, seiAddr, evmAddr)
	require.Error(t, a.BankKeeper.SendCoinsFromModuleToAccount(ctx, evmModule, castAddr, amt))
	require.Error(t, a.BankKeeper.SendCoins(ctx, sourceAddr, castAddr, amt))
	require.Error(t, a.BankKeeper.SendCoinsAndWei(ctx, sourceAddr, castAddr, sdk.OneInt(), sdk.OneInt()))
}

func TestEvmAddressHandler_GetSeiAddressFromString(t *testing.T) {
	a, ctx := keeper.MockApp(t)
	seiAddr, evmAddr := keeper.MockAddressPair()
	a.GigaEvmKeeper.SetAddressMapping(ctx, seiAddr, evmAddr)

	_, notAssociatedEvmAddr := keeper.MockAddressPair()
	castAddr := sdk.AccAddress(notAssociatedEvmAddr[:])

	tests := []struct {
		name       string
		address    string
		want       sdk.AccAddress
		wantErrMsg string
	}{
		{
			name:    "valid 0x address, associated → returns mapped Sei address",
			address: evmAddr.String(),
			want:    seiAddr,
		},
		{
			name:    "valid 0x address, not associated → returns cast Sei address",
			address: notAssociatedEvmAddr.String(),
			want:    castAddr,
		},
		{
			name:    "valid bech32 address → returns itself",
			address: seiAddr.String(),
			want:    seiAddr,
		},
		{
			name:       "invalid address",
			address:    "invalid",
			wantErrMsg: "decoding bech32 failed: invalid bech32 string length 7",
		},
		{
			name:       "empty address",
			address:    "",
			wantErrMsg: "empty address string is not allowed",
		},
	}
	h := evmkeeper.NewEvmAddressHandler(&a.GigaEvmKeeper)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := h.GetSeiAddressFromString(ctx, tt.address)
			if tt.wantErrMsg != "" {
				require.EqualError(t, err, tt.wantErrMsg)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}
