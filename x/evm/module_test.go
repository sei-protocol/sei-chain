package evm_test

import (
	"math/big"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	testkeeper "github.com/sei-protocol/sei-chain/testutil/keeper"
	"github.com/sei-protocol/sei-chain/x/evm"
	"github.com/sei-protocol/sei-chain/x/evm/state"
	"github.com/sei-protocol/sei-chain/x/evm/types"
	"github.com/stretchr/testify/require"
	abci "github.com/tendermint/tendermint/abci/types"
)

func TestABCI(t *testing.T) {
	k, ctx := testkeeper.MockEVMKeeper()
	_, evmAddr1 := testkeeper.MockAddressPair()
	_, evmAddr2 := testkeeper.MockAddressPair()
	amt := sdk.NewCoins(sdk.NewCoin("usei", sdk.NewInt(10)))
	k.BankKeeper().MintCoins(ctx, types.ModuleName, amt)
	k.BankKeeper().SendCoinsFromModuleToAccount(ctx, types.ModuleName, sdk.AccAddress(evmAddr1[:]), amt)
	m := evm.NewAppModule(nil, k)
	// first block
	m.BeginBlock(ctx, abci.RequestBeginBlock{})
	// 1st tx
	s := state.NewDBImpl(ctx.WithTxIndex(1), k, false)
	s.SubBalance(evmAddr1, big.NewInt(10000000000000))
	s.AddBalance(evmAddr2, big.NewInt(8000000000000))
	feeCollectorAddr, err := k.GetFeeCollectorAddress(ctx)
	require.Nil(t, err)
	s.AddBalance(feeCollectorAddr, big.NewInt(2000000000000))
	require.Nil(t, s.Finalize())
	k.AppendToEvmTxDeferredInfo(ctx.WithTxIndex(1), ethtypes.Bloom{}, common.Hash{})
	// 3rd tx
	s = state.NewDBImpl(ctx.WithTxIndex(3), k, false)
	s.SubBalance(evmAddr2, big.NewInt(5000000000000))
	s.AddBalance(evmAddr1, big.NewInt(5000000000000))
	require.Nil(t, s.Finalize())
	m.EndBlock(ctx, abci.RequestEndBlock{})
	k.AppendToEvmTxDeferredInfo(ctx.WithTxIndex(3), ethtypes.Bloom{}, common.Hash{})
	require.Equal(t, uint64(0), k.BankKeeper().GetBalance(ctx, k.AccountKeeper().GetModuleAddress(types.ModuleName), "usei").Amount.Uint64())
	require.Equal(t, uint64(2), k.BankKeeper().GetBalance(ctx, k.AccountKeeper().GetModuleAddress(authtypes.FeeCollectorName), "usei").Amount.Uint64())

	// second block
	m.BeginBlock(ctx, abci.RequestBeginBlock{})
	// 2nd tx
	s = state.NewDBImpl(ctx.WithTxIndex(2), k, false)
	s.SubBalance(evmAddr2, big.NewInt(3000000000000))
	s.AddBalance(evmAddr1, big.NewInt(2000000000000))
	require.Nil(t, s.Finalize())
	k.AppendToEvmTxDeferredInfo(ctx.WithTxIndex(2), ethtypes.Bloom{}, common.Hash{})
	m.EndBlock(ctx, abci.RequestEndBlock{})
	require.Equal(t, uint64(1), k.BankKeeper().GetBalance(ctx, k.AccountKeeper().GetModuleAddress(types.ModuleName), "usei").Amount.Uint64())
	require.Equal(t, uint64(2), k.BankKeeper().GetBalance(ctx, k.AccountKeeper().GetModuleAddress(authtypes.FeeCollectorName), "usei").Amount.Uint64())
}
