package evm_test

import (
	"context"
	"encoding/hex"
	"math"
	"math/big"
	"testing"

	"github.com/grpc-ecosystem/grpc-gateway/runtime"
	"github.com/holiman/uint256"
	"github.com/sei-protocol/sei-chain/sei-cosmos/client"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/tracing"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/sei-protocol/sei-chain/app"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	authtypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/auth/types"
	"github.com/sei-protocol/sei-chain/sei-cosmos/x/auth/vesting"
	vestingtypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/auth/vesting/types"
	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	testkeeper "github.com/sei-protocol/sei-chain/testutil/keeper"
	"github.com/sei-protocol/sei-chain/x/evm"
	"github.com/sei-protocol/sei-chain/x/evm/state"
	"github.com/sei-protocol/sei-chain/x/evm/types"
	"github.com/sei-protocol/sei-chain/x/evm/types/ethtx"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestModuleName(t *testing.T) {
	k, _ := testkeeper.MockEVMKeeper(t)
	module := evm.NewAppModule(nil, k)
	assert.Equal(t, "evm", module.Name())
}

func TestModuleRoute(t *testing.T) {
	k, _ := testkeeper.MockEVMKeeper(t)
	module := evm.NewAppModule(nil, k)
	assert.Equal(t, "evm", module.Route().Path())
	assert.Equal(t, false, module.Route().Empty())
}

func TestQuerierRoute(t *testing.T) {
	k, _ := testkeeper.MockEVMKeeper(t)
	module := evm.NewAppModule(nil, k)
	assert.Equal(t, "evm", module.QuerierRoute())
}

func TestModuleExportGenesis(t *testing.T) {
	k, ctx := testkeeper.MockEVMKeeper(t)
	module := evm.NewAppModule(nil, k)
	cdc := app.MakeEncodingConfig().Marshaler
	jsonMsg := module.ExportGenesis(ctx, cdc)
	jsonStr := string(jsonMsg)
	assert.Equal(t, `{"params":{"priority_normalizer":"1.000000000000000000","base_fee_per_gas":"0.000000000000000000","minimum_fee_per_gas":"1000000000.000000000000000000","whitelisted_cw_code_hashes_for_delegate_call":[],"deliver_tx_hook_wasm_gas_limit":"300000","max_dynamic_base_fee_upward_adjustment":"0.018900000000000000","max_dynamic_base_fee_downward_adjustment":"0.003900000000000000","target_gas_used_per_block":"250000","maximum_fee_per_gas":"1000000000000.000000000000000000","register_pointer_disabled":false,"sei_sstore_set_gas_eip2200":"20000"},"address_associations":[{"sei_address":"sei17xpfvakm2amg962yls6f84z3kell8c5la4jkdu","eth_address":"0x27F7B8B8B5A4e71E8E9aA671f4e4031E3773303F"}],"codes":[],"states":[],"nonces":[],"serialized":[{"prefix":"Fg==","key":"AwAC","value":"AAAAAAAAAAQ="},{"prefix":"Fg==","key":"BAAG","value":"AAAAAAAAAAU="},{"prefix":"Fg==","key":"BgAB","value":"AAAAAAAAAAY="}]}`, jsonStr)
}

func TestConsensusVersion(t *testing.T) {
	k, _ := testkeeper.MockEVMKeeper(t)
	module := evm.NewAppModule(nil, k)
	assert.Equal(t, uint64(21), module.ConsensusVersion())
}

func TestABCI(t *testing.T) {
	k, ctx := testkeeper.MockEVMKeeper(t)
	_, evmAddr1 := testkeeper.MockAddressPair()
	_, evmAddr2 := testkeeper.MockAddressPair()
	amt := sdk.NewCoins(sdk.NewCoin("usei", sdk.NewInt(10)))
	k.BankKeeper().MintCoins(ctx, types.ModuleName, amt)
	k.BankKeeper().SendCoinsFromModuleToAccount(ctx, types.ModuleName, sdk.AccAddress(evmAddr1[:]), amt)
	// first block
	k.BeginBlock(ctx)
	// 1st tx
	s := state.NewDBImpl(ctx.WithTxIndex(1), k, false)
	s.SubBalance(evmAddr1, uint256.NewInt(10000000000000), tracing.BalanceChangeUnspecified)
	s.AddBalance(evmAddr2, uint256.NewInt(8000000000000), tracing.BalanceChangeUnspecified)
	feeCollectorAddr, err := k.GetFeeCollectorAddress(ctx)
	require.Nil(t, err)
	s.AddBalance(feeCollectorAddr, uint256.NewInt(2000000000000), tracing.BalanceChangeUnspecified)
	surplus, err := s.Finalize()
	require.Nil(t, err)
	require.True(t, surplus.Equal(sdk.ZeroInt()))
	k.AppendToEvmTxDeferredInfo(ctx.WithTxIndex(1), ethtypes.Bloom{}, common.Hash{4}, surplus)
	// 3rd tx
	s = state.NewDBImpl(ctx.WithTxIndex(3), k, false)
	s.SubBalance(evmAddr2, uint256.NewInt(5000000000000), tracing.BalanceChangeUnspecified)
	s.AddBalance(evmAddr1, uint256.NewInt(5000000000000), tracing.BalanceChangeUnspecified)
	surplus, err = s.Finalize()
	require.Nil(t, err)
	require.True(t, surplus.Equal(sdk.ZeroInt()))
	k.AppendToEvmTxDeferredInfo(ctx.WithTxIndex(3), ethtypes.Bloom{}, common.Hash{3}, surplus)
	k.SetTxResults([]*abci.ExecTxResult{{Code: 0}, {Code: 0}, {Code: 0}, {Code: 0}})
	k.SetMsgs([]*types.MsgEVMTransaction{nil, {}, nil, {}})
	k.EndBlock(ctx, 0, 0)
	require.Equal(t, uint64(0), k.BankKeeper().GetBalance(ctx, k.AccountKeeper().GetModuleAddress(types.ModuleName), "usei").Amount.Uint64())
	require.Equal(t, uint64(2), k.BankKeeper().GetBalance(ctx, k.AccountKeeper().GetModuleAddress(authtypes.FeeCollectorName), "usei").Amount.Uint64())

	// second block
	k.BeginBlock(ctx)
	// 2nd tx
	s = state.NewDBImpl(ctx.WithTxIndex(2), k, false)
	s.SubBalance(evmAddr2, uint256.NewInt(3000000000000), tracing.BalanceChangeUnspecified)
	s.AddBalance(evmAddr1, uint256.NewInt(2000000000000), tracing.BalanceChangeUnspecified)
	surplus, err = s.Finalize()
	require.Nil(t, err)
	require.Equal(t, sdk.NewInt(1000000000000), surplus)
	k.AppendToEvmTxDeferredInfo(ctx.WithTxIndex(2), ethtypes.Bloom{}, common.Hash{2}, surplus)
	k.SetTxResults([]*abci.ExecTxResult{{Code: 0}, {Code: 0}, {Code: 0}})
	k.SetMsgs([]*types.MsgEVMTransaction{nil, nil, {}})
	k.EndBlock(ctx, 0, 0)
	require.Equal(t, uint64(1), k.BankKeeper().GetBalance(ctx, k.AccountKeeper().GetModuleAddress(types.ModuleName), "usei").Amount.Uint64())
	require.Equal(t, uint64(2), k.BankKeeper().GetBalance(ctx, k.AccountKeeper().GetModuleAddress(authtypes.FeeCollectorName), "usei").Amount.Uint64())

	// third block
	k.BeginBlock(ctx)
	msg := mockEVMTransactionMessage(t)
	k.SetMsgs([]*types.MsgEVMTransaction{msg})
	k.SetTxResults([]*abci.ExecTxResult{{Code: 1, Log: "test error"}})
	k.EndBlock(ctx, 0, 0)
	err = k.FlushTransientReceipts(ctx)
	require.NoError(t, err)
	tx, _ := msg.AsTransaction()
	receipt := testkeeper.WaitForReceipt(t, k, ctx, tx.Hash())
	require.Equal(t, receipt.BlockNumber, uint64(ctx.BlockHeight()))
	require.Equal(t, receipt.VmError, "test error")

	// disallow creating vesting account for coinbase address
	k.BeginBlock(ctx)
	coinbase := state.GetCoinbaseAddress(2)
	vms := vesting.NewMsgServerImpl(*k.AccountKeeper(), k.BankKeeper())
	_, err = vms.CreateVestingAccount(sdk.WrapSDKContext(ctx), &vestingtypes.MsgCreateVestingAccount{
		FromAddress: sdk.AccAddress(evmAddr1[:]).String(),
		ToAddress:   coinbase.String(),
		Amount:      sdk.NewCoins(sdk.NewCoin("usei", sdk.OneInt())),
		EndTime:     math.MaxInt64,
	})
	require.NotNil(t, err)
}

// Ensures legacy receipt migration runs on interval and moves receipts to receipt.db
func TestLegacyReceiptMigrationInterval(t *testing.T) {
	a := app.Setup(t, false, false, false)
	k := a.EvmKeeper
	ctx := a.GetContextForDeliverTx([]byte{})

	// Seed a legacy receipt directly into KV
	txHash := common.BytesToHash([]byte{0x42})
	receipt := &types.Receipt{TxHashHex: txHash.Hex()}
	store := k.PrefixStore(ctx, types.ReceiptKeyPrefix)
	bz, err := receipt.Marshal()
	require.NoError(t, err)
	store.Set(txHash[:], bz)

	// Advance blocks until we hit the migration interval
	// Interval defined in keeper/receipt.go as LegacyReceiptMigrationInterval
	// Ensure we trigger EndBlock on the interval height
	interval := int64(10) // mirror keeper.LegacyReceiptMigrationInterval
	for i := int64(1); i <= interval; i++ {
		ctx = ctx.WithBlockHeight(i)
		k.BeginBlock(ctx)
		k.EndBlock(ctx, 0, 0)
	}
	require.NoError(t, k.FlushTransientReceipts(ctx))

	// After migration interval, legacy KV entry should be gone
	exists := k.PrefixStore(ctx, types.ReceiptKeyPrefix).Get(txHash[:]) != nil
	require.False(t, exists)

	// And receipt should be retrievable through normal path
	r := testkeeper.WaitForReceipt(t, &k, ctx, txHash)
	require.Equal(t, txHash.Hex(), r.TxHashHex)

	// Check that the receipt is retrievable through receipt.db only
	r, err = k.GetReceiptFromReceiptStore(ctx, txHash)
	require.NoError(t, err)
	require.Equal(t, txHash.Hex(), r.TxHashHex)
}

func TestAnteSurplus(t *testing.T) {
	a := app.Setup(t, false, false, false)
	k := a.EvmKeeper
	ctx := a.GetContextForDeliverTx([]byte{})
	// first block
	k.BeginBlock(ctx)
	k.AddAnteSurplus(ctx, common.BytesToHash([]byte("1234")), sdk.NewInt(1_000_000_000_001))
	k.EndBlock(ctx, 0, 0)
	require.Equal(t, uint64(1), k.BankKeeper().GetBalance(ctx, k.AccountKeeper().GetModuleAddress(types.ModuleName), "usei").Amount.Uint64())
	require.Equal(t, uint64(1), k.BankKeeper().GetWeiBalance(ctx, k.AccountKeeper().GetModuleAddress(types.ModuleName)).Uint64())
	// ante surplus should be cleared
	a.SetDeliverStateToCommit()
	a.Commit(context.Background())
	require.Equal(t, uint64(0), k.GetAnteSurplusSum(ctx).Uint64())
}

// This test is just to make sure that the routes can be added without crashing
func TestRoutesAddition(t *testing.T) {
	k, _ := testkeeper.MockEVMKeeper(t)
	appModule := evm.NewAppModule(nil, k)
	mux := runtime.NewServeMux()
	appModule.RegisterGRPCGatewayRoutes(client.Context{}, mux)

	require.NotNil(t, appModule)
}

func mockEVMTransactionMessage(t *testing.T) *types.MsgEVMTransaction {
	k, ctx := testkeeper.MockEVMKeeper(t)
	chainID := k.ChainID(ctx)
	chainCfg := types.DefaultChainConfig()
	ethCfg := chainCfg.EthereumConfig(chainID)
	blockNum := big.NewInt(ctx.BlockHeight())
	privKey := testkeeper.MockPrivateKey()
	testPrivHex := hex.EncodeToString(privKey.Bytes())
	key, _ := crypto.HexToECDSA(testPrivHex)
	to := new(common.Address)
	txData := ethtypes.DynamicFeeTx{
		Nonce:     1,
		GasFeeCap: big.NewInt(10000000000000),
		Gas:       1000,
		To:        to,
		Value:     big.NewInt(1000000000000000),
		Data:      []byte("abc"),
		ChainID:   chainID,
	}

	signer := ethtypes.MakeSigner(ethCfg, blockNum, uint64(ctx.BlockTime().Unix()))
	tx, err := ethtypes.SignTx(ethtypes.NewTx(&txData), signer, key)
	typedTx, err := ethtx.NewDynamicFeeTx(tx)
	msg, err := types.NewMsgEVMTransaction(typedTx)
	require.Nil(t, err)
	return msg
}
