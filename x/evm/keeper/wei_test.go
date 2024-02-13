package keeper_test

import (
	"math/big"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	testkeeper "github.com/sei-protocol/sei-chain/testutil/keeper"
	"github.com/sei-protocol/sei-chain/x/evm/state"
	"github.com/sei-protocol/sei-chain/x/evm/types"
	"github.com/stretchr/testify/require"
	abci "github.com/tendermint/tendermint/abci/types"
)

func TestSettleCommon(t *testing.T) {
	k, ctx := testkeeper.MockEVMKeeper()

	_, addr1 := testkeeper.MockAddressPair()
	_, addr2 := testkeeper.MockAddressPair()
	_, addr3 := testkeeper.MockAddressPair()
	amt := sdk.NewCoins(sdk.NewCoin("usei", sdk.NewInt(10)))
	k.BankKeeper().MintCoins(ctx, types.ModuleName, amt)
	k.BankKeeper().SendCoinsFromModuleToAccount(ctx, types.ModuleName, sdk.AccAddress(addr1[:]), amt)

	// addr1 send one sei and one Wei to addr2
	// escrow 1 would have a one-sei surplus
	s := state.NewDBImpl(ctx.WithTxIndex(1), k, false)
	s.SubBalance(addr1, big.NewInt(1_000_000_000_001))
	s.AddBalance(addr2, big.NewInt(1_000_000_000_001))
	require.Nil(t, s.Finalize())
	k.AppendToEvmTxDeferredInfo(ctx.WithTxIndex(1), ethtypes.Bloom{}, common.Hash{})

	// addr2 send two weis to addr3
	// escrow 2 would have a one-sei surplus
	s = state.NewDBImpl(ctx.WithTxIndex(2), k, false)
	s.SubBalance(addr2, big.NewInt(2))
	s.AddBalance(addr3, big.NewInt(2))
	require.Nil(t, s.Finalize())
	k.AppendToEvmTxDeferredInfo(ctx.WithTxIndex(2), ethtypes.Bloom{}, common.Hash{})

	// addr1 send one wei to addr2
	// escrow 3 would have a one-sei deficit
	s = state.NewDBImpl(ctx.WithTxIndex(3), k, false)
	s.SubBalance(addr1, big.NewInt(1))
	s.AddBalance(addr2, big.NewInt(1))
	require.Nil(t, s.Finalize())
	k.AppendToEvmTxDeferredInfo(ctx.WithTxIndex(3), ethtypes.Bloom{}, common.Hash{})

	globalEscrowBalance := k.BankKeeper().GetBalance(ctx, k.AccountKeeper().GetModuleAddress(banktypes.WeiEscrowName), "usei")
	require.True(t, globalEscrowBalance.Amount.IsZero())

	k.SetTxResults([]*abci.ExecTxResult{{Code: 0}, {Code: 0}, {Code: 0}, {Code: 0}})
	deferredInfo := k.GetEVMTxDeferredInfo(ctx)
	k.SettleWeiEscrowAccounts(ctx, deferredInfo)
	globalEscrowBalance = k.BankKeeper().GetBalance(ctx, k.AccountKeeper().GetModuleAddress(banktypes.WeiEscrowName), "usei")
	require.Equal(t, int64(1), globalEscrowBalance.Amount.Int64())
}

func TestSettleMultiRedeem(t *testing.T) {
	k, ctx := testkeeper.MockEVMKeeper()

	_, addr1 := testkeeper.MockAddressPair()
	_, addr2 := testkeeper.MockAddressPair()
	_, addr3 := testkeeper.MockAddressPair()
	amt := sdk.NewCoins(sdk.NewCoin("usei", sdk.NewInt(1)))
	k.BankKeeper().MintCoins(ctx, types.ModuleName, amt)
	k.BankKeeper().SendCoinsFromModuleToAccount(ctx, types.ModuleName, sdk.AccAddress(addr1[:]), amt)
	k.BankKeeper().MintCoins(ctx, types.ModuleName, amt)
	k.BankKeeper().SendCoinsFromModuleToAccount(ctx, types.ModuleName, sdk.AccAddress(addr2[:]), amt)
	k.BankKeeper().MintCoins(ctx, types.ModuleName, amt)
	k.BankKeeper().SendCoinsFromModuleToAccount(ctx, types.ModuleName, sdk.AccAddress(addr3[:]), amt)

	// addr1 send one Wei to addr3
	// addr2 send one Wei to addr3
	// escrow 1 would have a two-sei surplus
	s := state.NewDBImpl(ctx.WithTxIndex(1), k, false)
	s.SubBalance(addr1, big.NewInt(1))
	s.AddBalance(addr3, big.NewInt(1))
	s.SubBalance(addr2, big.NewInt(1))
	s.AddBalance(addr3, big.NewInt(1))
	require.Nil(t, s.Finalize())
	k.AppendToEvmTxDeferredInfo(ctx.WithTxIndex(1), ethtypes.Bloom{}, common.Hash{})

	// addr3 send one wei to addr1
	// addr3 send one wei to addr2
	// addr3 send one wei to addr1
	// escrow 2 would have a one-sei deficit
	s = state.NewDBImpl(ctx.WithTxIndex(2), k, false)
	s.SubBalance(addr3, big.NewInt(1))
	s.AddBalance(addr1, big.NewInt(1))
	s.SubBalance(addr3, big.NewInt(1))
	s.AddBalance(addr2, big.NewInt(1))
	s.SubBalance(addr3, big.NewInt(1))
	s.AddBalance(addr1, big.NewInt(1))
	require.Nil(t, s.Finalize())
	k.AppendToEvmTxDeferredInfo(ctx.WithTxIndex(2), ethtypes.Bloom{}, common.Hash{})

	globalEscrowBalance := k.BankKeeper().GetBalance(ctx, k.AccountKeeper().GetModuleAddress(banktypes.WeiEscrowName), "usei")
	require.True(t, globalEscrowBalance.Amount.IsZero())

	k.SetTxResults([]*abci.ExecTxResult{{Code: 0}, {Code: 0}, {Code: 0}})
	deferredInfo := k.GetEVMTxDeferredInfo(ctx)
	k.SettleWeiEscrowAccounts(ctx, deferredInfo)
	globalEscrowBalance = k.BankKeeper().GetBalance(ctx, k.AccountKeeper().GetModuleAddress(banktypes.WeiEscrowName), "usei")
	require.Equal(t, int64(1), globalEscrowBalance.Amount.Int64())
}
