package evm_test

import (
	"math"
	"math/big"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	"github.com/cosmos/cosmos-sdk/x/auth/vesting"
	vestingtypes "github.com/cosmos/cosmos-sdk/x/auth/vesting/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/tracing"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	testkeeper "github.com/sei-protocol/sei-chain/testutil/keeper"
	"github.com/sei-protocol/sei-chain/x/evm"
	"github.com/sei-protocol/sei-chain/x/evm/state"
	"github.com/sei-protocol/sei-chain/x/evm/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	abci "github.com/tendermint/tendermint/abci/types"
)

func TestModuleName(t *testing.T) {
	k, _ := testkeeper.MockEVMKeeper()
	module := evm.NewAppModule(nil, k)
	assert.Equal(t, "evm", module.Name())
}

func TestModuleRoute(t *testing.T) {
	k, _ := testkeeper.MockEVMKeeper()
	module := evm.NewAppModule(nil, k)
	assert.Equal(t, "evm", module.Route().Path())
	assert.Equal(t, false, module.Route().Empty())
}

func TestQuerierRoute(t *testing.T) {
	k, _ := testkeeper.MockEVMKeeper()
	module := evm.NewAppModule(nil, k)
	assert.Equal(t, "evm", module.QuerierRoute())
}

func TestModuleExportGenesis(t *testing.T) {
	k, ctx := testkeeper.MockEVMKeeper()
	module := evm.NewAppModule(nil, k)
	jsonMsg := module.ExportGenesis(ctx, types.ModuleCdc)
	jsonStr := string(jsonMsg)
	assert.Equal(t, "{\"params\":{\"priority_normalizer\":\"1.000000000000000000\",\"base_fee_per_gas\":\"0.000000000000000000\",\"minimum_fee_per_gas\":\"1000000000.000000000000000000\",\"whitelisted_cw_code_hashes_for_delegate_call\":[]},\"address_associations\":[{\"sei_address\":\"sei17xpfvakm2amg962yls6f84z3kell8c5la4jkdu\",\"eth_address\":\"0x27F7B8B8B5A4e71E8E9aA671f4e4031E3773303F\"}],\"codes\":[],\"states\":[],\"nonces\":[],\"serialized\":[{\"prefix\":\"Fg==\",\"key\":\"AwAB\",\"value\":\"AAAAAAAAAAM=\"},{\"prefix\":\"Fg==\",\"key\":\"BAAF\",\"value\":\"AAAAAAAAAAQ=\"}]}", jsonStr)
}

func TestConsensusVersion(t *testing.T) {
	k, _ := testkeeper.MockEVMKeeper()
	module := evm.NewAppModule(nil, k)
	assert.Equal(t, uint64(8), module.ConsensusVersion())
}

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
	s.SubBalance(evmAddr1, big.NewInt(10000000000000), tracing.BalanceChangeUnspecified)
	s.AddBalance(evmAddr2, big.NewInt(8000000000000), tracing.BalanceChangeUnspecified)
	feeCollectorAddr, err := k.GetFeeCollectorAddress(ctx)
	require.Nil(t, err)
	s.AddBalance(feeCollectorAddr, big.NewInt(2000000000000), tracing.BalanceChangeUnspecified)
	surplus, err := s.Finalize()
	require.Nil(t, err)
	require.Equal(t, sdk.ZeroInt(), surplus)
	k.AppendToEvmTxDeferredInfo(ctx.WithTxIndex(1), ethtypes.Bloom{}, common.Hash{4}, surplus)
	// 3rd tx
	s = state.NewDBImpl(ctx.WithTxIndex(3), k, false)
	s.SubBalance(evmAddr2, big.NewInt(5000000000000), tracing.BalanceChangeUnspecified)
	s.AddBalance(evmAddr1, big.NewInt(5000000000000), tracing.BalanceChangeUnspecified)
	surplus, err = s.Finalize()
	require.Nil(t, err)
	require.Equal(t, sdk.ZeroInt(), surplus)
	k.AppendToEvmTxDeferredInfo(ctx.WithTxIndex(3), ethtypes.Bloom{}, common.Hash{3}, surplus)
	k.SetTxResults([]*abci.ExecTxResult{{Code: 0}, {Code: 0}, {Code: 0}, {Code: 0}})
	m.EndBlock(ctx, abci.RequestEndBlock{})
	require.Equal(t, uint64(0), k.BankKeeper().GetBalance(ctx, k.AccountKeeper().GetModuleAddress(types.ModuleName), "usei").Amount.Uint64())
	require.Equal(t, uint64(2), k.BankKeeper().GetBalance(ctx, k.AccountKeeper().GetModuleAddress(authtypes.FeeCollectorName), "usei").Amount.Uint64())

	// second block
	m.BeginBlock(ctx, abci.RequestBeginBlock{})
	// 2nd tx
	s = state.NewDBImpl(ctx.WithTxIndex(2), k, false)
	s.SubBalance(evmAddr2, big.NewInt(3000000000000), tracing.BalanceChangeUnspecified)
	s.AddBalance(evmAddr1, big.NewInt(2000000000000), tracing.BalanceChangeUnspecified)
	surplus, err = s.Finalize()
	require.Nil(t, err)
	require.Equal(t, sdk.NewInt(1000000000000), surplus)
	k.AppendToEvmTxDeferredInfo(ctx.WithTxIndex(2), ethtypes.Bloom{}, common.Hash{2}, surplus)
	k.SetTxResults([]*abci.ExecTxResult{{Code: 0}, {Code: 0}, {Code: 0}})
	m.EndBlock(ctx, abci.RequestEndBlock{})
	require.Equal(t, uint64(1), k.BankKeeper().GetBalance(ctx, k.AccountKeeper().GetModuleAddress(types.ModuleName), "usei").Amount.Uint64())
	require.Equal(t, uint64(2), k.BankKeeper().GetBalance(ctx, k.AccountKeeper().GetModuleAddress(authtypes.FeeCollectorName), "usei").Amount.Uint64())

	// third block
	m.BeginBlock(ctx, abci.RequestBeginBlock{})
	k.AppendErrorToEvmTxDeferredInfo(ctx.WithTxIndex(0), common.Hash{1}, "test error")
	k.SetTxResults([]*abci.ExecTxResult{{Code: 1}})
	m.EndBlock(ctx, abci.RequestEndBlock{})
	receipt, err := k.GetReceipt(ctx, common.Hash{1})
	require.Nil(t, err)
	require.Equal(t, receipt.BlockNumber, uint64(ctx.BlockHeight()))
	require.Equal(t, receipt.VmError, "test error")

	// fourth block with locked tokens in coinbase address
	m.BeginBlock(ctx, abci.RequestBeginBlock{})
	coinbase := state.GetCoinbaseAddress(2)
	vms := vesting.NewMsgServerImpl(*k.AccountKeeper(), k.BankKeeper())
	_, err = vms.CreateVestingAccount(sdk.WrapSDKContext(ctx), &vestingtypes.MsgCreateVestingAccount{
		FromAddress: sdk.AccAddress(evmAddr1[:]).String(),
		ToAddress:   coinbase.String(),
		Amount:      sdk.NewCoins(sdk.NewCoin("usei", sdk.OneInt())),
		EndTime:     math.MaxInt64,
	})
	require.Nil(t, err)
	s = state.NewDBImpl(ctx.WithTxIndex(2), k, false)
	s.SubBalance(evmAddr1, big.NewInt(2000000000000), tracing.BalanceChangeUnspecified)
	s.AddBalance(evmAddr2, big.NewInt(1000000000000), tracing.BalanceChangeUnspecified)
	s.AddBalance(feeCollectorAddr, big.NewInt(1000000000000), tracing.BalanceChangeUnspecified)
	surplus, err = s.Finalize()
	require.Nil(t, err)
	k.AppendToEvmTxDeferredInfo(ctx.WithTxIndex(2), ethtypes.Bloom{}, common.Hash{}, surplus)
	k.SetTxResults([]*abci.ExecTxResult{{Code: 0}, {Code: 0}, {Code: 0}})
	require.Equal(t, sdk.OneInt(), k.BankKeeper().SpendableCoins(ctx, coinbase).AmountOf("usei"))
	m.EndBlock(ctx, abci.RequestEndBlock{}) // should not crash
	require.Equal(t, sdk.OneInt(), k.BankKeeper().GetBalance(ctx, coinbase, "usei").Amount)
	require.Equal(t, sdk.ZeroInt(), k.BankKeeper().SpendableCoins(ctx, coinbase).AmountOf("usei"))
}

func TestAnteSurplus(t *testing.T) {
	k, ctx := testkeeper.MockEVMKeeper()
	m := evm.NewAppModule(nil, k)
	// first block
	m.BeginBlock(ctx, abci.RequestBeginBlock{})
	k.AddAnteSurplus(ctx, common.BytesToHash([]byte("1234")), sdk.NewInt(1_000_000_000_001))
	m.EndBlock(ctx, abci.RequestEndBlock{})
	require.Equal(t, uint64(1), k.BankKeeper().GetBalance(ctx, k.AccountKeeper().GetModuleAddress(types.ModuleName), "usei").Amount.Uint64())
	require.Equal(t, uint64(1), k.BankKeeper().GetWeiBalance(ctx, k.AccountKeeper().GetModuleAddress(types.ModuleName)).Uint64())
	// ante surplus should be cleared
	require.Equal(t, uint64(0), k.GetAnteSurplusSum(ctx).Uint64())
}
