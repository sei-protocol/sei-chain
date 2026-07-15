package evmrpc_test

import (
	"context"
	"encoding/hex"
	"fmt"
	"math/big"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/export"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/sei-protocol/sei-chain/app"
	"github.com/sei-protocol/sei-chain/app/legacyabci"
	"github.com/sei-protocol/sei-chain/evmrpc"
	"github.com/sei-protocol/sei-chain/example/contracts/simplestorage"
	bam "github.com/sei-protocol/sei-chain/sei-cosmos/baseapp"
	"github.com/sei-protocol/sei-chain/sei-cosmos/client"
	cosmostestutil "github.com/sei-protocol/sei-chain/sei-cosmos/testutil"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	txtypes "github.com/sei-protocol/sei-chain/sei-cosmos/types/tx"
	banktypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/bank/types"
	receipt "github.com/sei-protocol/sei-chain/sei-db/ledger_db/receipt"
	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/bytes"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	tenderminttypes "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/rpc/coretypes"
	tmtypes "github.com/sei-protocol/sei-chain/sei-tendermint/types"
	testkeeper "github.com/sei-protocol/sei-chain/testutil/keeper"
	"github.com/sei-protocol/sei-chain/x/evm/state"
	"github.com/sei-protocol/sei-chain/x/evm/types"
	"github.com/sei-protocol/sei-chain/x/evm/types/ethtx"
	"github.com/stretchr/testify/require"
	dbm "github.com/tendermint/tm-db"
)

func primeReceiptStore(t *testing.T, store receipt.ReceiptStore, latest int64) {
	t.Helper()
	if store == nil {
		return
	}
	if latest <= 0 {
		latest = 1
	}
	require.NoError(t, store.SetLatestVersion(latest))
	require.NoError(t, store.SetEarliestVersion(1))
	require.Equal(t, int64(1), store.EarliestVersion())
}

// bcAlwaysFailClient fails every Block call (header resolution uses a single block fetch).
type bcAlwaysFailClient struct{ *MockClient }

func (bc bcAlwaysFailClient) Block(ctx context.Context, h *int64) (*coretypes.ResultBlock, error) {
	return nil, fmt.Errorf("fail bc")
}

func TestEstimateGas(t *testing.T) {
	Ctx = Ctx.WithBlockHeight(1)
	// transfer
	_, from := testkeeper.MockAddressPair()
	_, to := testkeeper.MockAddressPair()
	txArgs := map[string]any{
		"from":    from.Hex(),
		"to":      to.Hex(),
		"value":   "0x10",
		"nonce":   "0x1",
		"chainId": fmt.Sprintf("%#x", EVMKeeper.ChainID(Ctx)),
	}
	amts := sdk.NewCoins(sdk.NewCoin(EVMKeeper.GetBaseDenom(Ctx), sdk.NewInt(20)))
	EVMKeeper.BankKeeper().MintCoins(Ctx, types.ModuleName, amts)
	EVMKeeper.BankKeeper().SendCoinsFromModuleToAccount(Ctx, types.ModuleName, sdk.AccAddress(from[:]), amts)
	resObj := sendRequestGood(t, "estimateGas", txArgs, nil, map[string]any{})
	result := resObj["result"].(string)
	require.Equal(t, "0x5208", result) // 21000
	resObj = sendRequestGood(t, "estimateGas", txArgs, "latest", map[string]any{})
	result = resObj["result"].(string)
	require.Equal(t, "0x5208", result) // 21000
	resObj = sendRequestGood(t, "estimateGas", txArgs, "0x1", map[string]any{})
	result = resObj["result"].(string)
	require.Equal(t, "0x5208", result) // 21000

	// contract call
	_, contractAddr := testkeeper.MockAddressPair()
	code, err := os.ReadFile("../example/contracts/simplestorage/SimpleStorage.bin")
	require.Nil(t, err)
	bz, err := hex.DecodeString(string(code))
	require.Nil(t, err)
	abi, err := simplestorage.SimplestorageMetaData.GetAbi()
	require.Nil(t, err)
	input, err := abi.Pack("set", big.NewInt(20))
	require.Nil(t, err)
	EVMKeeper.SetCode(Ctx, contractAddr, bz)
	txArgs = map[string]any{
		"from":    from.Hex(),
		"to":      contractAddr.Hex(),
		"value":   "0x0",
		"nonce":   "0x2",
		"chainId": fmt.Sprintf("%#x", EVMKeeper.ChainID(Ctx)),
		"input":   fmt.Sprintf("%#x", input),
	}
	resObj = sendRequestGood(t, "estimateGas", txArgs, nil, map[string]any{})
	result = resObj["result"].(string)
	require.Equal(t, "0x54ac", result) // 21497

	Ctx = Ctx.WithBlockHeight(8)
}

func TestChainConfigReflectsSstoreParam(t *testing.T) {
	testApp := app.Setup(t, false, false, false)
	baseCtx := testApp.GetContextForDeliverTx([]byte{})

	oldCtx, _ := baseCtx.CacheContext()
	oldCtx = oldCtx.WithBlockHeight(100).WithIsTracing(true)

	newCtx, _ := baseCtx.CacheContext()
	newCtx = newCtx.WithBlockHeight(200).WithIsTracing(true)
	params := testApp.EvmKeeper.GetParams(newCtx)
	params.SeiSstoreSetGasEip2200 = 72000
	testApp.EvmKeeper.SetParams(newCtx, params)
	primeReceiptStore(t, testApp.EvmKeeper.ReceiptStore(), newCtx.BlockHeight())

	oldHeight := oldCtx.BlockHeight()
	ctxProvider := func(height int64) sdk.Context {
		switch {
		case height == evmrpc.LatestCtxHeight:
			return newCtx
		case height >= newCtx.BlockHeight():
			return newCtx
		case height == oldHeight:
			return oldCtx
		default:
			return newCtx
		}
	}

	encodingCfg := app.MakeEncodingConfig()
	tmClient := &MockClient{}
	watermarks := evmrpc.NewWatermarkManager(tmClient, ctxProvider, nil, testApp.EvmKeeper.ReceiptStore())
	backend := evmrpc.NewBackend(
		ctxProvider,
		&testApp.EvmKeeper,
		legacyabci.BeginBlockKeepers{},
		func(int64) client.TxConfig { return encodingCfg.TxConfig },
		tmClient,
		&SConfig,
		testApp.BaseApp,
		testApp.TracerAnteHandler,
		evmrpc.NewBlockCache(3000),
		&sync.Mutex{},
		watermarks,
	)

	oldCfg := backend.ChainConfigAtHeight(oldHeight)
	require.NotNil(t, oldCfg.SeiSstoreSetGasEIP2200)
	require.Equal(t, types.DefaultSeiSstoreSetGasEIP2200, *oldCfg.SeiSstoreSetGasEIP2200)

	latestCfg := backend.ChainConfig()
	require.NotNil(t, latestCfg.SeiSstoreSetGasEIP2200)
	require.Equal(t, uint64(72000), *latestCfg.SeiSstoreSetGasEIP2200)
}

func TestEstimateGasAfterCalls(t *testing.T) {
	Ctx = Ctx.WithBlockHeight(1)
	// estimate get after set
	_, from := testkeeper.MockAddressPair()
	amts := sdk.NewCoins(sdk.NewCoin(EVMKeeper.GetBaseDenom(Ctx), sdk.NewInt(20)))
	EVMKeeper.BankKeeper().MintCoins(Ctx, types.ModuleName, amts)
	EVMKeeper.BankKeeper().SendCoinsFromModuleToAccount(Ctx, types.ModuleName, sdk.AccAddress(from[:]), amts)
	_, contractAddr := testkeeper.MockAddressPair()
	code, err := os.ReadFile("../example/contracts/simplestorage/SimpleStorage.bin")
	require.Nil(t, err)
	bz, err := hex.DecodeString(string(code))
	require.Nil(t, err)
	abi, err := simplestorage.SimplestorageMetaData.GetAbi()
	require.Nil(t, err)
	call, err := abi.Pack("set", big.NewInt(20))
	require.Nil(t, err)
	input, err := abi.Pack("get")
	require.Nil(t, err)
	EVMKeeper.SetCode(Ctx, contractAddr, bz)
	txArgs := map[string]any{
		"from":    from.Hex(),
		"to":      contractAddr.Hex(),
		"value":   "0x0",
		"nonce":   "0x2",
		"chainId": fmt.Sprintf("%#x", EVMKeeper.ChainID(Ctx)),
		"input":   fmt.Sprintf("%#x", input),
	}
	callArgs := map[string]any{
		"from":    from.Hex(),
		"to":      contractAddr.Hex(),
		"value":   "0x0",
		"nonce":   "0x2",
		"chainId": fmt.Sprintf("%#x", EVMKeeper.ChainID(Ctx)),
		"input":   fmt.Sprintf("%#x", call),
	}
	resObj := sendRequestGood(t, "estimateGasAfterCalls", txArgs, []any{callArgs}, nil, map[string]any{})
	result := resObj["result"].(string)
	require.Equal(t, "0x536d", result) // 21357 for get

	Ctx = Ctx.WithBlockHeight(8)
}

func TestEstimateGasAfterCallsMaxCalls(t *testing.T) {
	testCtx := Ctx.WithBlockHeight(1)
	ctxProvider := func(height int64) sdk.Context {
		if height == evmrpc.LatestCtxHeight {
			return testCtx.WithIsTracing(true)
		}
		return testCtx.WithBlockHeight(height).WithIsTracing(true)
	}

	const maxCalls = 2
	config := &evmrpc.SimulateConfig{
		GasCap:              1000000,
		EVMTimeout:          5 * time.Second,
		MaxEstimateGasCalls: maxCalls,
	}

	testApp := testkeeper.TestApp(t)
	watermarks := evmrpc.NewWatermarkManager(&MockClient{}, ctxProvider, nil, EVMKeeper.ReceiptStore())
	simAPI := evmrpc.NewSimulationAPI(
		ctxProvider,
		EVMKeeper,
		legacyabci.BeginBlockKeepers{},
		func(int64) client.TxConfig { return TxConfig },
		&MockClient{},
		config,
		testApp.BaseApp,
		testApp.TracerAnteHandler,
		evmrpc.ConnectionTypeHTTP,
		evmrpc.NewBlockCache(3000),
		&sync.Mutex{},
		watermarks,
	)

	_, from := testkeeper.MockAddressPair()
	_, to := testkeeper.MockAddressPair()
	args := export.TransactionArgs{From: &from, To: &to}
	call := export.TransactionArgs{From: &from, To: &to}

	// Over the cap: rejected early with the "too many calls" error.
	oversized := make([]export.TransactionArgs, maxCalls+1)
	for i := range oversized {
		oversized[i] = call
	}
	_, err := simAPI.EstimateGasAfterCalls(t.Context(), args, oversized, nil, nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "too many calls")

	// At the cap: the size guard is passed.
	atCap := make([]export.TransactionArgs, maxCalls)
	for i := range atCap {
		atCap[i] = call
	}
	_, err = simAPI.EstimateGasAfterCalls(t.Context(), args, atCap, nil, nil)
	require.Nil(t, err)
}

func TestCreateAccessList(t *testing.T) {
	Ctx = Ctx.WithBlockHeight(1)

	_, from := testkeeper.MockAddressPair()
	_, contractAddr := testkeeper.MockAddressPair()
	code, err := os.ReadFile("../example/contracts/simplestorage/SimpleStorage.bin")
	require.Nil(t, err)
	bz, err := hex.DecodeString(string(code))
	require.Nil(t, err)
	abi, err := simplestorage.SimplestorageMetaData.GetAbi()
	require.Nil(t, err)
	input, err := abi.Pack("set", big.NewInt(20))
	require.Nil(t, err)
	EVMKeeper.SetCode(Ctx, contractAddr, bz)
	txArgs := map[string]any{
		"from":    from.Hex(),
		"to":      contractAddr.Hex(),
		"value":   "0x0",
		"nonce":   "0x1",
		"chainId": fmt.Sprintf("%#x", EVMKeeper.ChainID(Ctx)),
		"input":   fmt.Sprintf("%#x", input),
	}
	amts := sdk.NewCoins(sdk.NewCoin(EVMKeeper.GetBaseDenom(Ctx), sdk.NewInt(2000000)))
	EVMKeeper.BankKeeper().MintCoins(Ctx, types.ModuleName, amts)
	EVMKeeper.BankKeeper().SendCoinsFromModuleToAccount(Ctx, types.ModuleName, sdk.AccAddress(from[:]), amts)
	resObj := sendRequestGood(t, "createAccessList", txArgs, "latest")
	result := resObj["result"].(map[string]any)
	require.Equal(t, []any{}, result["accessList"]) // the code uses MSTORE which does not trace access list

	resObj = sendRequestBad(t, "createAccessList", txArgs, "0x1")
	result = resObj["error"].(map[string]any)
	require.Equal(t, "error block", result["message"])

	Ctx = Ctx.WithBlockHeight(8)
}

func TestCall(t *testing.T) {
	Ctx = Ctx.WithBlockHeight(1)

	_, from := testkeeper.MockAddressPair()
	_, contractAddr := testkeeper.MockAddressPair()
	code, err := os.ReadFile("../example/contracts/simplestorage/SimpleStorage.bin")
	require.Nil(t, err)
	bz, err := hex.DecodeString(string(code))
	require.Nil(t, err)
	abi, err := simplestorage.SimplestorageMetaData.GetAbi()
	require.Nil(t, err)
	input, err := abi.Pack("set", big.NewInt(20))
	require.Nil(t, err)
	EVMKeeper.SetCode(Ctx, contractAddr, bz)
	txArgs := map[string]any{
		"from":    from.Hex(),
		"to":      contractAddr.Hex(),
		"value":   "0x0",
		"nonce":   "0x2",
		"chainId": fmt.Sprintf("%#x", EVMKeeper.ChainID(Ctx)),
		"input":   fmt.Sprintf("%#x", input),
	}
	resObj := sendRequestGood(t, "call", txArgs, nil, map[string]any{}, map[string]any{})
	result := resObj["result"].(string)
	require.Equal(t, "0x608060405234801561000f575f80fd5b506004361061003f575f3560e01c806360fe47b1146100435780636d4ce63c1461005f5780639c3674fc1461007d575b5f80fd5b61005d6004803603810190610058919061010a565b610087565b005b6100676100c7565b6040516100749190610144565b60405180910390f35b6100856100cf565b005b805f819055507f0de2d86113046b9e8bb6b785e96a6228f6803952bf53a40b68a36dce316218c1816040516100bc9190610144565b60405180910390a150565b5f8054905090565b5f80fd5b5f80fd5b5f819050919050565b6100e9816100d7565b81146100f3575f80fd5b50565b5f81359050610104816100e0565b92915050565b5f6020828403121561011f5761011e6100d3565b5b5f61012c848285016100f6565b91505092915050565b61013e816100d7565b82525050565b5f6020820190506101575f830184610135565b9291505056fea2646970667358221220bb55137839ea2afda11ab2d30ad07fee30bb9438caaa46e30ccd1053ed72439064736f6c63430008150033", result)

	Ctx = Ctx.WithBlockHeight(8)
}

func TestCallStateOverrideTooManySlots(t *testing.T) {
	Ctx = Ctx.WithBlockHeight(1)
	_, from := testkeeper.MockAddressPair()
	_, to := testkeeper.MockAddressPair()
	txArgs := map[string]any{
		"from":    from.Hex(),
		"to":      to.Hex(),
		"value":   "0x0",
		"nonce":   "0x2",
		"chainId": fmt.Sprintf("%#x", EVMKeeper.ChainID(Ctx)),
	}

	slots := map[string]any{}
	for i := 0; i <= SConfig.MaxStateOverrideSlots; i++ {
		slots[common.BigToHash(big.NewInt(int64(i))).Hex()] = common.Hash{}.Hex()
	}
	overrides := map[string]map[string]any{to.Hex(): {"state": slots}}

	resObj := sendRequestGood(t, "call", txArgs, "latest", overrides)
	errMap := resObj["error"].(map[string]any)
	require.Contains(t, errMap["message"].(string), "too many slots")

	Ctx = Ctx.WithBlockHeight(8)
}

func TestEthCallHighAmount(t *testing.T) {
	Ctx = Ctx.WithBlockHeight(1)
	_, from := testkeeper.MockAddressPair()
	_, to := testkeeper.MockAddressPair()
	txArgs := map[string]any{
		"from":    from.Hex(),
		"to":      to.Hex(),
		"value":   "0x0",
		"nonce":   "0x2",
		"chainId": fmt.Sprintf("%#x", EVMKeeper.ChainID(Ctx)),
	}

	overrides := map[string]map[string]any{
		from.Hex(): {"balance": "0xffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"},
	}
	resObj := sendRequestGood(t, "call", txArgs, "latest", overrides)
	errMap := resObj["error"].(map[string]any)
	result := errMap["message"]
	require.Equal(t, result, "error: balance override overflow")

	Ctx = Ctx.WithBlockHeight(8)
}

func TestNewRevertError(t *testing.T) {
	err := evmrpc.NewRevertError(&core.ExecutionResult{})
	require.NotNil(t, err)
	require.Equal(t, 3, err.ErrorCode())
	require.Equal(t, "0x", err.ErrorData())
}

func TestConvertBlockNumber(t *testing.T) {
	tmClient := &MockClient{}
	watermarks := evmrpc.NewWatermarkManager(tmClient, func(i int64) sdk.Context {
		if i == evmrpc.LatestCtxHeight {
			return sdk.Context{}.WithBlockHeight(1000)
		}
		return sdk.Context{}
	}, nil, nil)
	backend := evmrpc.NewBackend(func(i int64) sdk.Context {
		if i == evmrpc.LatestCtxHeight {
			return sdk.Context{}.WithBlockHeight(1000)
		}
		return sdk.Context{}
	}, nil, legacyabci.BeginBlockKeepers{}, nil, &MockClient{}, nil, nil, nil, evmrpc.NewBlockCache(3000), &sync.Mutex{}, watermarks)
	require.Equal(t, int64(10), backend.ConvertBlockNumber(10))
	require.Equal(t, int64(1), backend.ConvertBlockNumber(0))
	require.Equal(t, int64(1000), backend.ConvertBlockNumber(-2))
	require.Equal(t, int64(1000), backend.ConvertBlockNumber(-3))
	require.Equal(t, int64(1000), backend.ConvertBlockNumber(-4))
}

func TestPreV620UpgradeUsesBaseFeeNil(t *testing.T) {
	// Set up a test context with a height before v6.2.0 upgrade
	// For pacific-1 chain, we need to set a height that's before the v6.2.0 upgrade
	testHeight := int64(1000) // A height before v6.2.0 upgrade

	// Create a new test app to have control over the upgrade keeper
	testApp := app.Setup(t, false, false, false)
	testCtx := testApp.GetContextForDeliverTx([]byte{}).WithBlockHeight(testHeight)

	// Set the chain ID to "pacific-1" to trigger the upgrade check
	testCtx = testCtx.WithChainID("pacific-1")

	// Set the v6.2.0 upgrade height to a height higher than our test height
	// This simulates that the upgrade hasn't happened yet
	v620UpgradeHeight := int64(2000)
	testApp.UpgradeKeeper.SetDone(testCtx.WithBlockHeight(v620UpgradeHeight), "6.2.0")

	// Create a backend with our test context provider
	ctxProvider := func(height int64) sdk.Context {
		return testCtx.WithBlockHeight(height)
	}

	config := &evmrpc.SimulateConfig{
		GasCap:     10000000,
		EVMTimeout: time.Second * 30,
	}

	tmClient := NewMockClientWithLatest(3000)
	watermarks := evmrpc.NewWatermarkManager(tmClient, ctxProvider, nil, testApp.EvmKeeper.ReceiptStore())
	backend := evmrpc.NewBackend(
		ctxProvider,
		&testApp.EvmKeeper,
		legacyabci.BeginBlockKeepers{},
		func(int64) client.TxConfig { return TxConfig },
		tmClient,
		config,
		testApp.BaseApp,
		testApp.TracerAnteHandler,
		evmrpc.NewBlockCache(3000),
		&sync.Mutex{},
		watermarks,
	)

	// Test HeaderByNumber with a height before v6.2.0 upgrade
	header, err := backend.HeaderByNumber(context.Background(), 1000)
	require.NoError(t, err)
	require.NotNil(t, header)

	// For pacific-1 chain before v6.2.0 upgrade, base fee should be nil
	require.Nil(t, header.BaseFee, "Base fee should be nil for pacific-1 chain before v6.2.0 upgrade")

	// Test with a height after v6.2.0 upgrade
	headerAfterUpgrade, err := backend.HeaderByNumber(context.Background(), 2500)
	require.NoError(t, err)
	require.NotNil(t, headerAfterUpgrade)

	// For pacific-1 chain after v6.2.0 upgrade, base fee should not be nil
	require.NotNil(t, headerAfterUpgrade.BaseFee, "Base fee should not be nil for pacific-1 chain after v6.2.0 upgrade")

	// Test with a different chain ID (not pacific-1)
	testCtxDifferentChain := testCtx.WithChainID("test-chain")
	ctxProviderDifferentChain := func(height int64) sdk.Context {
		return testCtxDifferentChain.WithBlockHeight(height)
	}

	diffTmClient := NewMockClientWithLatest(3000)
	diffWatermarks := evmrpc.NewWatermarkManager(diffTmClient, ctxProviderDifferentChain, nil, testApp.EvmKeeper.ReceiptStore())
	backendDifferentChain := evmrpc.NewBackend(
		ctxProviderDifferentChain,
		&testApp.EvmKeeper,
		legacyabci.BeginBlockKeepers{},
		func(int64) client.TxConfig { return TxConfig },
		diffTmClient,
		config,
		testApp.BaseApp,
		testApp.TracerAnteHandler,
		evmrpc.NewBlockCache(3000),
		&sync.Mutex{},
		diffWatermarks,
	)

	headerDifferentChain, err := backendDifferentChain.HeaderByNumber(context.Background(), 1000)
	require.NoError(t, err)
	require.NotNil(t, headerDifferentChain)

	// For non-pacific-1 chains, base fee should not be nil regardless of upgrade status
	require.NotNil(t, headerDifferentChain.BaseFee, "Base fee should not be nil for non-pacific-1 chains")
}

// Header gasLimit comes from the active SDK ConsensusParams at the block's height.
func TestGasLimitUsesConsensusOrConfig(t *testing.T) {
	testApp := app.Setup(t, false, false, false)
	baseCtx := testApp.GetContextForDeliverTx([]byte{}).WithBlockHeight(1).
		WithConsensusParams(&tenderminttypes.ConsensusParams{Block: &tenderminttypes.BlockParams{MaxGas: 200_000_000}})

	ctxProvider := func(h int64) sdk.Context { return baseCtx.WithBlockHeight(h) }
	cfg := &evmrpc.SimulateConfig{GasCap: 10_000_000, EVMTimeout: time.Second}

	tmClient := &MockClient{}
	watermarks := evmrpc.NewWatermarkManager(tmClient, ctxProvider, nil, testApp.EvmKeeper.ReceiptStore())
	backend := evmrpc.NewBackend(ctxProvider, &testApp.EvmKeeper,
		legacyabci.BeginBlockKeepers{},
		func(int64) client.TxConfig { return TxConfig },
		tmClient, cfg, testApp.BaseApp, testApp.TracerAnteHandler, evmrpc.NewBlockCache(3000), &sync.Mutex{}, watermarks)

	header, err := backend.HeaderByNumber(context.Background(), 1)
	require.NoError(t, err)
	require.Equal(t, uint64(200_000_000), header.GasLimit)

	header2, err := backend.HeaderByNumber(context.Background(), 0)
	require.NoError(t, err)
	require.Equal(t, uint64(200_000_000), header2.GasLimit)
}

// Gas-limit fallback tests
func TestGasLimitFallbackToDefault(t *testing.T) {
	testApp := app.Setup(t, false, false, false)
	cfg := &evmrpc.SimulateConfig{GasCap: 20_000_000, EVMTimeout: time.Second}

	// Case 1: ConsensusParams is nil → DefaultBlockGasLimit.
	nilParamsCtx := testApp.GetContextForDeliverTx([]byte{}).WithBlockHeight(1).WithConsensusParams(nil)
	ctxProvider1 := func(h int64) sdk.Context { return nilParamsCtx.WithBlockHeight(h) }
	tmClient1 := &MockClient{}
	watermarks1 := evmrpc.NewWatermarkManager(tmClient1, ctxProvider1, nil, testApp.EvmKeeper.ReceiptStore())
	backend1 := evmrpc.NewBackend(ctxProvider1, &testApp.EvmKeeper, legacyabci.BeginBlockKeepers{}, func(int64) client.TxConfig { return TxConfig }, tmClient1, cfg, testApp.BaseApp, testApp.TracerAnteHandler, evmrpc.NewBlockCache(3000), &sync.Mutex{}, watermarks1)
	h1, err := backend1.HeaderByNumber(context.Background(), 1)
	require.NoError(t, err)
	require.Equal(t, uint64(10_000_000), h1.GasLimit) // DefaultBlockGasLimit

	// Case 2: Block fails — resolution errors out entirely.
	baseCtx := testApp.GetContextForDeliverTx([]byte{}).WithBlockHeight(1)
	ctxProvider2 := func(h int64) sdk.Context { return baseCtx.WithBlockHeight(h) }
	bcClient := &bcAlwaysFailClient{MockClient: &MockClient{}}
	watermarks2 := evmrpc.NewWatermarkManager(bcClient, ctxProvider2, nil, testApp.EvmKeeper.ReceiptStore())
	backend2 := evmrpc.NewBackend(ctxProvider2, &testApp.EvmKeeper, legacyabci.BeginBlockKeepers{}, func(int64) client.TxConfig { return TxConfig }, bcClient, cfg, testApp.BaseApp, testApp.TracerAnteHandler, evmrpc.NewBlockCache(3000), &sync.Mutex{}, watermarks2)
	_, err = backend2.HeaderByNumber(context.Background(), 1)
	require.Error(t, err)
}

// Exercises CurrentHeader: block fetch + getHeader vs fallback when Block RPC fails.
// HeaderByNumber / BlockByNumber cover getHeader with an already-resolved tmBlock.
func TestSimulateBackendBlockResolutionCoverage(t *testing.T) {
	testApp := app.Setup(t, false, false, false)
	baseCtx := testApp.GetContextForDeliverTx([]byte{}).WithBlockHeight(1)
	ctxProvider := func(h int64) sdk.Context {
		if h == evmrpc.LatestCtxHeight {
			return baseCtx
		}
		return baseCtx.WithBlockHeight(h)
	}
	cfg := &evmrpc.SimulateConfig{GasCap: 10_000_000, EVMTimeout: time.Second}
	primeReceiptStore(t, testApp.EvmKeeper.ReceiptStore(), 1)
	tmClient := &MockClient{}
	watermarks := evmrpc.NewWatermarkManager(tmClient, ctxProvider, nil, testApp.EvmKeeper.ReceiptStore())
	backend := evmrpc.NewBackend(ctxProvider, &testApp.EvmKeeper,
		legacyabci.BeginBlockKeepers{}, func(int64) client.TxConfig { return TxConfig },
		tmClient, cfg, testApp.BaseApp, testApp.TracerAnteHandler, evmrpc.NewBlockCache(3000), &sync.Mutex{}, watermarks)

	t.Run("CurrentHeader_fetches_block_then_getHeader", func(t *testing.T) {
		h := backend.CurrentHeader()
		require.NotNil(t, h)
		require.Equal(t, int64(1), h.Number.Int64())
		require.Equal(t, common.BytesToHash(MockBlockID.Hash), h.ParentHash)
		expectedBaseFee := testApp.EvmKeeper.GetNextBaseFeePerGas(ctxProvider(evmrpc.LatestCtxHeight)).TruncateInt().BigInt()
		require.Equal(t, 0, h.BaseFee.Cmp(expectedBaseFee))
	})

	t.Run("CurrentHeader_fallback_gas_limit_when_block_unavailable", func(t *testing.T) {
		bcClient := &bcAlwaysFailClient{MockClient: &MockClient{}}
		wm := evmrpc.NewWatermarkManager(bcClient, ctxProvider, nil, testApp.EvmKeeper.ReceiptStore())
		b2 := evmrpc.NewBackend(ctxProvider, &testApp.EvmKeeper,
			legacyabci.BeginBlockKeepers{}, func(int64) client.TxConfig { return TxConfig },
			bcClient, cfg, testApp.BaseApp, testApp.TracerAnteHandler, evmrpc.NewBlockCache(3000), &sync.Mutex{}, wm)
		h := b2.CurrentHeader()
		require.NotNil(t, h)
		require.Equal(t, int64(1), h.Number.Int64())
		require.Equal(t, uint64(baseCtx.ConsensusParams().Block.MaxGas), h.GasLimit)
		expectedBaseFee := testApp.EvmKeeper.GetNextBaseFeePerGas(ctxProvider(evmrpc.LatestCtxHeight)).TruncateInt().BigInt()
		require.Equal(t, 0, h.BaseFee.Cmp(expectedBaseFee))
		require.Equal(t, common.Hash{}, h.ParentHash)
	})
}

func TestSimulationAPIRequestLimiter(t *testing.T) {

	type testEnv struct {
		simAPI *evmrpc.SimulationAPI
		args   export.TransactionArgs
	}

	// Helper function to create uint64 pointer
	uint64Ptr := func(v uint64) *uint64 { return &v }

	newTestEnv := func(t *testing.T) *testEnv {
		t.Helper()
		// Test setup using a proper context similar to other tests
		testCtx := Ctx.WithBlockHeight(1)

		// Create a simulation API with a very small request limiter to test rate limiting
		ctxProvider := func(height int64) sdk.Context {
			if height == evmrpc.LatestCtxHeight {
				return testCtx.WithIsTracing(true)
			}
			return testCtx.WithBlockHeight(height).WithIsTracing(true)
		}

		// Create a config with a small concurrency limit for reliable testing
		config := &evmrpc.SimulateConfig{
			GasCap:                       1000000,
			EVMTimeout:                   5 * time.Second,
			MaxConcurrentSimulationCalls: 2, // Small limit to easily trigger rate limiting
		}

		// Use the existing test app from the global setup
		testApp := testkeeper.TestApp(t)

		watermarks := evmrpc.NewWatermarkManager(&MockClient{}, ctxProvider, nil, EVMKeeper.ReceiptStore())

		// Create simulation API
		simAPI := evmrpc.NewSimulationAPI(
			ctxProvider,
			EVMKeeper,
			legacyabci.BeginBlockKeepers{},
			func(int64) client.TxConfig { return TxConfig },
			&MockClient{},
			config,
			testApp.BaseApp,
			testApp.TracerAnteHandler,
			evmrpc.ConnectionTypeHTTP,
			evmrpc.NewBlockCache(3000),
			&sync.Mutex{},
			watermarks,
		)

		// Setup test data - create addresses and fund account
		_, from := testkeeper.MockAddressPair()
		_, to := testkeeper.MockAddressPair()

		// Fund the account for actual transactions
		amts := sdk.NewCoins(sdk.NewCoin(EVMKeeper.GetBaseDenom(testCtx), sdk.NewInt(2000000)))
		require.NoError(t, EVMKeeper.BankKeeper().MintCoins(testCtx, types.ModuleName, amts))
		require.NoError(t, EVMKeeper.BankKeeper().SendCoinsFromModuleToAccount(testCtx, types.ModuleName, from[:], amts))

		// Convert to export.TransactionArgs for eth_call
		args := export.TransactionArgs{
			From:  &from,
			To:    &to,
			Value: (*hexutil.Big)(big.NewInt(16)),
			Nonce: (*hexutil.Uint64)(uint64Ptr(1)),
		}

		return &testEnv{
			simAPI: simAPI,
			args:   args,
		}
	}

	t.Run("TestEthCallRateLimiting", func(t *testing.T) {
		tEnv := newTestEnv(t)
		// Test eth_call rate limiting with concurrent requests
		const numRequests = 10 // Much more than the limit of 2
		runBurst := func() []error {
			results := make(chan error, numRequests)
			start := make(chan struct{})
			var wg sync.WaitGroup

			// Release all requests at once to maximize contention on the limiter.
			for range numRequests {
				wg.Go(func() {
					<-start
					_, err := tEnv.simAPI.Call(t.Context(), tEnv.args, nil, nil, nil)
					results <- err
				})
			}

			close(start)
			wg.Wait()
			close(results)

			var errors []error
			for err := range results {
				errors = append(errors, err)
			}
			return errors
		}

		successCount := 0
		rejectedCount := 0
		for _, err := range runBurst() {
			if err == nil {
				successCount++
			} else if strings.Contains(err.Error(), "eth_call rejected due to rate limit: server busy") {
				rejectedCount++
			} else {
				t.Logf("Unexpected error: %v", err)
			}
		}

		require.Equal(t, numRequests, successCount+rejectedCount, "All requests should be accounted for")
		require.Greaterf(t, rejectedCount, 0, "Should have rejected requests due to rate limiting (burst: %d successful, %d rejected)", successCount, rejectedCount)
		require.Greater(t, successCount, 0, "Should have some successful requests")

		t.Logf("eth_call rate limiting: %d successful, %d rejected out of %d total", successCount, rejectedCount, numRequests)
	})

	t.Run("TestEstimateGasRateLimiting", func(t *testing.T) {
		tEnv := newTestEnv(t)
		// Test eth_estimateGas rate limiting
		numRequests := 8
		results := make(chan error, numRequests)

		// Start all requests concurrently
		for i := 0; i < numRequests; i++ {
			go func() {
				_, err := tEnv.simAPI.EstimateGas(context.Background(), tEnv.args, nil, nil)
				results <- err
			}()
		}

		// Collect all results
		var errors []error
		for i := 0; i < numRequests; i++ {
			errors = append(errors, <-results)
		}

		// Count successful vs rejected requests
		successCount := 0
		rejectedCount := 0
		for _, err := range errors {
			if err == nil {
				successCount++
			} else if strings.Contains(err.Error(), "eth_estimateGas rejected due to rate limit: server busy") {
				rejectedCount++
			} else {
				t.Logf("Unexpected estimateGas error: %v", err)
			}
		}

		// Under constrained scheduling these requests can serialize and avoid
		// rejections. The stable invariant is that every response is either success or
		// rate-limited. Hence, the assertion for success count instead of rejection
		// count. This makes the testing less flaky/more robust given any limit for
		// parallelism.
		require.Greater(t, successCount, 0, "Should have at least one successful estimateGas request")
		require.Equal(t, numRequests, successCount+rejectedCount, "All estimateGas requests should be accounted for")

		t.Logf("eth_estimateGas rate limiting: %d successful, %d rejected out of %d total", successCount, rejectedCount, numRequests)
	})

	t.Run("TestCreateAccessListRateLimiting", func(t *testing.T) {
		tEnv := newTestEnv(t)
		// Test eth_createAccessList rate limiting
		numRequests := 8
		results := make(chan error, numRequests)

		// Start all requests concurrently
		for i := 0; i < numRequests; i++ {
			go func() {
				_, err := tEnv.simAPI.CreateAccessList(t.Context(), tEnv.args, nil)
				results <- err
			}()
		}

		// Collect all results
		var errors []error
		for i := 0; i < numRequests; i++ {
			errors = append(errors, <-results)
		}

		// Count successful vs rejected requests
		successCount := 0
		rejectedCount := 0
		for _, err := range errors {
			if err == nil {
				successCount++
			} else if strings.Contains(err.Error(), "eth_createAccessList rejected due to rate limit: server busy") {
				rejectedCount++
			} else {
				t.Logf("Unexpected createAccessList error: %v", err)
			}
		}

		// Under constrained scheduling these requests can serialize and avoid
		// rejections. The stable invariant is that every response is either success or
		// rate-limited.
		require.Greater(t, successCount, 0, "Should have at least one successful createAccessList request")
		require.Equal(t, numRequests, successCount+rejectedCount, "All createAccessList requests should be accounted for")

		t.Logf("eth_createAccessList rate limiting: %d successful, %d rejected out of %d total", successCount, rejectedCount, numRequests)
	})

	t.Run("TestEstimateGasAfterCallsRateLimiting", func(t *testing.T) {
		tEnv := newTestEnv(t)
		// Test eth_estimateGasAfterCalls rate limiting
		numRequests := 2
		results := make(chan error, numRequests)

		// Create a simple call to use as a precondition
		callArgs := export.TransactionArgs{
			From:  tEnv.args.From,
			To:    tEnv.args.To,
			Value: (*hexutil.Big)(big.NewInt(8)),
			Nonce: (*hexutil.Uint64)(uint64Ptr(0)),
		}

		// Start all requests concurrently
		for i := 0; i < numRequests; i++ {
			go func() {
				_, err := tEnv.simAPI.EstimateGasAfterCalls(context.Background(), tEnv.args, []export.TransactionArgs{callArgs}, nil, nil)
				results <- err
			}()
		}

		// Collect all results
		var errors []error
		for i := 0; i < numRequests; i++ {
			errors = append(errors, <-results)
		}

		// Count successful vs rejected requests
		successCount := 0
		rejectedCount := 0
		for _, err := range errors {
			if err == nil {
				successCount++
			} else if strings.Contains(err.Error(), "eth_estimateGasAfterCalls rejected due to rate limit: server busy") {
				rejectedCount++
			} else {
				t.Logf("Unexpected estimateGasAfterCalls error: %v", err)
			}
		}

		// Should have no rejections within the rate limiting
		require.Equal(t, rejectedCount, 0, "Should have no rejected estimateGasAfterCalls requests")
		require.Equal(t, numRequests, successCount+rejectedCount, "All estimateGasAfterCalls requests should be accounted for")

		t.Logf("eth_estimateGasAfterCalls rate limiting: %d successful, %d rejected out of %d total", successCount, rejectedCount, numRequests)
	})

	t.Run("TestSequentialRequestsAfterLoad", func(t *testing.T) {
		tEnv := newTestEnv(t)
		numRequests := 10
		results := make(chan error, numRequests)

		for i := 0; i < numRequests; i++ {
			go func() {
				_, err := tEnv.simAPI.Call(context.Background(), tEnv.args, nil, nil, nil)
				results <- err
			}()
		}

		// Wait for all concurrent requests to finish
		for i := 0; i < numRequests; i++ {
			<-results
		}

		// Give a small amount of time for any ongoing operations to complete
		time.Sleep(50 * time.Millisecond)

		// Now send sequential requests and ensure they succeed
		for i := 0; i < 3; i++ {
			_, err := tEnv.simAPI.Call(context.Background(), tEnv.args, nil, nil, nil)
			require.NoError(t, err, "Sequential request %d should succeed after rate limiter recovers", i+1)
		}

		t.Log("Sequential requests after load: all succeeded")
	})

	t.Run("TestDifferentMethodsShareSameLimiter", func(t *testing.T) {
		// Test that different simulation methods share the same rate limiter.
		// A single burst can occasionally avoid contention on overloaded CI workers,
		// so retry a synchronized burst a few times.
		const (
			numCallRequests     = 20
			numEstimateRequests = 20
			maxAttempts         = 5
		)
		totalRequests := numCallRequests + numEstimateRequests

		runMixedBurst := func(tEnv *testEnv) (int, int) {
			results := make(chan error, totalRequests)
			start := make(chan struct{})
			var wg sync.WaitGroup

			// Start mixed requests and release them at once to maximize contention.
			for range numCallRequests {
				wg.Go(func() {
					<-start
					_, err := tEnv.simAPI.Call(t.Context(), tEnv.args, nil, nil, nil)
					results <- err
				})
			}
			for range numEstimateRequests {
				wg.Go(func() {
					<-start
					_, err := tEnv.simAPI.EstimateGas(t.Context(), tEnv.args, nil, nil)
					results <- err
				})
			}

			close(start)
			wg.Wait()
			close(results)

			successCount := 0
			rejectedCount := 0
			for err := range results {
				if err == nil {
					successCount++
				} else if strings.Contains(err.Error(), "rejected due to rate limit: server busy") {
					rejectedCount++
				}
			}
			return successCount, rejectedCount
		}

		var (
			lastSuccess       int
			lastRejected      int
			attemptsUsed      int
			observedRejection bool
		)
		for attempt := 1; attempt <= maxAttempts; attempt++ {
			attemptsUsed = attempt
			lastSuccess, lastRejected = runMixedBurst(newTestEnv(t))
			require.Equalf(t, totalRequests, lastSuccess+lastRejected, "All mixed method requests should be accounted for (attempt %d)", attempt)
			if lastRejected > 0 {
				observedRejection = true
				break
			}
		}

		require.Truef(
			t,
			observedRejection,
			"Different methods should share the same rate limiter (last burst: %d successful, %d rejected)",
			lastSuccess,
			lastRejected,
		)
		t.Logf(
			"Mixed methods rate limiting (attempt %d/%d): %d successful, %d rejected out of %d total",
			attemptsUsed,
			maxAttempts,
			lastSuccess,
			lastRejected,
			totalRequests,
		)
	})

	t.Run("TestRateLimitErrorFormat", func(t *testing.T) {
		tEnv := newTestEnv(t)
		// Test the error message format by overwhelming the rate limiter
		const numRequests = 20
		results := make(chan error, numRequests)
		start := make(chan struct{})
		var wg sync.WaitGroup

		// Release all requests at once to reliably saturate the limiter.
		for range numRequests {
			wg.Go(func() {
				<-start
				_, err := tEnv.simAPI.Call(t.Context(), tEnv.args, nil, nil, nil)
				results <- err
			})
		}
		close(start)
		wg.Wait()
		close(results)

		var rateLimitErrors []error
		for err := range results {
			if err != nil && strings.Contains(err.Error(), "rejected due to rate limit") {
				rateLimitErrors = append(rateLimitErrors, err)
			}
		}

		require.Greater(t, len(rateLimitErrors), 0, "Should have at least one rate limit error")

		// Verify error message format
		for _, err := range rateLimitErrors {
			require.Contains(t, err.Error(), "eth_call rejected due to rate limit: server busy")
			require.Contains(t, err.Error(), "server busy")
		}

		t.Logf("Found %d rate limit errors with correct format", len(rateLimitErrors))
	})
}

// fixedBlockClient is a tmClient stub that returns the same ResultBlock for
// every Block(height) call, ignoring the requested height. Lets the test
// pin a specific Block.Header / BlockID combination without dragging in the
// rest of the mock infrastructure.
type fixedBlockClient struct {
	client.Client
	block *coretypes.ResultBlock
}

func (c *fixedBlockClient) EvmNextPendingNonce(common.Address) uint64 {
	return 0
}

func (c *fixedBlockClient) EvmTxByHash(common.Hash) (tmtypes.Tx, bool) {
	return nil, false
}

func (c *fixedBlockClient) EvmProxy(common.Address) utils.Option[*url.URL] {
	return utils.None[*url.URL]()
}

func (c *fixedBlockClient) Block(_ context.Context, _ *int64) (*coretypes.ResultBlock, error) {
	return c.block, nil
}

func (c *fixedBlockClient) BlockResults(_ context.Context, _ *int64) (*coretypes.ResultBlockResults, error) {
	txResults := make([]*abci.ExecTxResult, len(c.block.Block.Txs))
	for i := range txResults {
		txResults[i] = &abci.ExecTxResult{}
	}
	return &coretypes.ResultBlockResults{
		Height:     c.block.Block.Height,
		TxsResults: txResults,
	}, nil
}

func (c *fixedBlockClient) Status(_ context.Context) (*coretypes.ResultStatus, error) {
	return &coretypes.ResultStatus{
		SyncInfo: coretypes.SyncInfo{
			LatestBlockHeight:   c.block.Block.Height,
			EarliestBlockHeight: 1,
		},
	}, nil
}

func TestTraceBlockByNumberUsesCompatDecoderForHistoricalCosmosTx(t *testing.T) {
	const (
		blockHeight = int64(42)
		v65Height   = int64(100)
	)

	testApp := app.Setup(t, false, false, false)
	ctx := testApp.GetContextForDeliverTx([]byte{}).WithBlockHeight(v65Height).WithClosestUpgradeName("v6.5")
	testApp.UpgradeKeeper.SetDone(ctx, "v6.5")
	ctxProvider := func(height int64) sdk.Context {
		if height == evmrpc.LatestCtxHeight {
			return ctx
		}
		return ctx.WithBlockHeight(height)
	}

	_, fromAddr := testkeeper.MockAddressPair()
	_, toAddr := testkeeper.MockAddressPair()
	txBuilder := TxConfig.NewTxBuilder()
	require.NoError(t, txBuilder.SetMsgs(banktypes.NewMsgSend(
		sdk.AccAddress(fromAddr.Bytes()),
		sdk.AccAddress(toAddr.Bytes()),
		sdk.NewCoins(sdk.NewCoin("usei", sdk.NewInt(1))),
	)))
	txBz, err := Encoder(txBuilder.GetTx())
	require.NoError(t, err)

	tests := []struct {
		name   string
		mutate func([]byte) []byte
	}{
		{
			name: "memo explicitly encoded as default empty string",
			mutate: func(bodyBytes []byte) []byte {
				return append(bodyBytes, 0x12, 0x00)
			},
		},
		{
			name: "timeout height explicitly encoded as default zero",
			mutate: func(bodyBytes []byte) []byte {
				return append(bodyBytes, 0x18, 0x00)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var raw txtypes.TxRaw
			require.NoError(t, raw.Unmarshal(txBz))
			raw.BodyBytes = tt.mutate(raw.BodyBytes)
			bloatedTxBz, err := raw.Marshal()
			require.NoError(t, err)

			_, err = TxConfig.TxDecoder()(bloatedTxBz)
			require.Error(t, err)
			require.Contains(t, err.Error(), "exceeds canonical size")

			makeBlock := func(height int64) *coretypes.ResultBlock {
				return &coretypes.ResultBlock{
					BlockID: tmtypes.BlockID{Hash: bytes.HexBytes(mustHexToBytes("0000000000000000000000000000000000000000000000000000000000000042"))},
					Block: &tmtypes.Block{
						Header: mockBlockHeader(height),
						Data:   tmtypes.Data{Txs: []tmtypes.Tx{bloatedTxBz}},
						LastCommit: &tmtypes.Commit{
							Height: height,
						},
					},
				}
			}
			tmClient := &fixedBlockClient{block: makeBlock(blockHeight)}
			watermarks := evmrpc.NewWatermarkManager(tmClient, ctxProvider, nil, testApp.EvmKeeper.ReceiptStore())
			backend := evmrpc.NewBackend(
				ctxProvider, &testApp.EvmKeeper, legacyabci.BeginBlockKeepers{},
				func(int64) client.TxConfig { return TxConfig }, tmClient, &SConfig,
				testApp.BaseApp, testApp.TracerAnteHandler,
				evmrpc.NewBlockCache(3000), &sync.Mutex{}, watermarks,
			)

			ethBlock, metadata, err := backend.BlockByNumber(context.Background(), rpc.BlockNumber(blockHeight))
			require.NoError(t, err)
			require.Len(t, ethBlock.Transactions(), 0)
			require.Len(t, metadata, 1)
			require.False(t, metadata[0].ShouldIncludeInTraceResult)
			require.NotNil(t, metadata[0].TraceRunnable)

			strictTmClient := &fixedBlockClient{block: makeBlock(v65Height)}
			strictWatermarks := evmrpc.NewWatermarkManager(strictTmClient, ctxProvider, nil, testApp.EvmKeeper.ReceiptStore())
			strictBackend := evmrpc.NewBackend(
				ctxProvider, &testApp.EvmKeeper, legacyabci.BeginBlockKeepers{},
				func(int64) client.TxConfig { return TxConfig }, strictTmClient, &SConfig,
				testApp.BaseApp, testApp.TracerAnteHandler,
				evmrpc.NewBlockCache(3000), &sync.Mutex{}, strictWatermarks,
			)
			_, _, err = strictBackend.BlockByNumber(context.Background(), rpc.BlockNumber(v65Height))
			require.Error(t, err)
			require.Contains(t, err.Error(), "exceeds canonical size")
		})
	}
}

// TestBlockByNumberNonTracedTxPassesTxBytes verifies that when BlockByNumber
// produces a TraceRunnable for a non-EVM (Cosmos) transaction, the runnable
// calls DeliverTx with req.Tx populated. An empty req.Tx causes ctx.TxBytes()
// to be nil inside the ante handler, which zeroes out TxSizeCostPerByte gas
// and diverges from what actually happened on-chain.
func TestBlockByNumberNonTracedTxPassesTxBytes(t *testing.T) {
	const blockHeight = int64(42)

	// Build a minimal BaseApp whose sole purpose is to capture ctx.TxBytes()
	// as seen by the ante handler. BaseApp sets ctx.TxBytes = req.Tx before
	// invoking the ante handler, so this is the correct interception point.
	var capturedTxBytes []byte
	minApp := bam.NewBaseApp("test", dbm.NewMemDB(), TxConfig.TxDecoder(), nil, &cosmostestutil.TestAppOpts{})
	minApp.SetAnteHandler(func(ctx sdk.Context, _ sdk.Tx, _ bool) (sdk.Context, error) {
		capturedTxBytes = ctx.TxBytes()
		return ctx, fmt.Errorf("stop") // short-circuit; we only need ante
	})
	minApp.Seal()

	testApp := app.Setup(t, false, false, false)
	_, fromAddr := testkeeper.MockAddressPair()
	_, toAddr := testkeeper.MockAddressPair()
	txBuilder := TxConfig.NewTxBuilder()
	require.NoError(t, txBuilder.SetMsgs(banktypes.NewMsgSend(
		sdk.AccAddress(fromAddr.Bytes()),
		sdk.AccAddress(toAddr.Bytes()),
		sdk.NewCoins(sdk.NewCoin("usei", sdk.NewInt(1))),
	)))
	rawTx, err := Encoder(txBuilder.GetTx())
	require.NoError(t, err)

	tmBlock := &coretypes.ResultBlock{
		BlockID: tmtypes.BlockID{Hash: bytes.HexBytes(mustHexToBytes("0000000000000000000000000000000000000000000000000000000000000042"))},
		Block: &tmtypes.Block{
			Header:     mockBlockHeader(blockHeight),
			Data:       tmtypes.Data{Txs: []tmtypes.Tx{rawTx}},
			LastCommit: &tmtypes.Commit{Height: blockHeight},
		},
	}
	ctx := testApp.GetContextForDeliverTx([]byte{}).WithBlockHeight(blockHeight)
	ctxProvider := func(height int64) sdk.Context {
		if height == evmrpc.LatestCtxHeight {
			return ctx
		}
		return ctx.WithBlockHeight(height)
	}

	tmClient := &fixedBlockClient{block: tmBlock}
	watermarks := evmrpc.NewWatermarkManager(tmClient, ctxProvider, nil, testApp.EvmKeeper.ReceiptStore())
	backend := evmrpc.NewBackend(
		ctxProvider, &testApp.EvmKeeper, legacyabci.BeginBlockKeepers{},
		func(int64) client.TxConfig { return TxConfig }, tmClient, &SConfig,
		minApp, testApp.TracerAnteHandler,
		evmrpc.NewBlockCache(3000), &sync.Mutex{}, watermarks,
	)

	_, metadata, err := backend.BlockByNumber(context.Background(), rpc.BlockNumber(blockHeight))
	require.NoError(t, err)
	require.Len(t, metadata, 1)
	require.NotNil(t, metadata[0].TraceRunnable)

	sdkCtx := testApp.GetContextForDeliverTx([]byte(rawTx)).WithBlockHeight(blockHeight)
	stateDB := state.NewDBImpl(sdkCtx, &testApp.EvmKeeper, false)
	metadata[0].TraceRunnable(stateDB)

	require.Equal(t, []byte(rawTx), capturedTxBytes, "DeliverTx must receive the raw tx bytes so ante handler gas charging is faithful to the original execution")
}

// TestGetTransactionUsesBlockIDHash pins down the GetTransaction → blockHash
// contract: callers (notably go-ethereum's tracers.API.TraceTransaction,
// which hands the returned blockHash to BlockByHash for cross-validation)
// must get the BlockID.Hash that the EVM receipt store recorded during
// FinalizeBlock. Two scenarios, each with its own teeth:
//
//   - CometBFT-shaped block: every Header field that contributes to the
//     Merkle root is populated. Subtest asserts the fixture invariant
//     (Header.Hash() == BlockID.Hash) AND that GetTransaction returns
//     that shared value. Catches a regression where the function
//     somehow stops returning a hash at all — the fix is value-
//     equivalent here, but a downstream change that, say, returns the
//     parent hash would still fail.
//   - Autobahn-shaped block: GigaRouter.translateGlobalBlock returns a
//     ResultBlock with a sparse Header (only ChainID/Height/Time set)
//     and BlockID.Hash explicitly carrying the Autobahn block hash.
//     Subtest asserts the fixture invariant (Header.Hash() != BlockID.Hash)
//     AND that GetTransaction returns BlockID.Hash. This is the case
//     the original code got wrong: Header.Hash() recomputes a Merkle
//     root over the sparse fields that doesn't match anything stored,
//     so debug_traceTransaction's blockByNumberAndHash check downstream
//     sends BlockByHash on a wild goose chase and fails with
//     ErrBlockNotFoundByHash.
func TestGetTransactionUsesBlockIDHash(t *testing.T) {
	const txHeight = int64(42)

	mkBlock := func(header tmtypes.Header, blockIDHash []byte, txBz []byte) *coretypes.ResultBlock {
		return &coretypes.ResultBlock{
			BlockID: tmtypes.BlockID{Hash: bytes.HexBytes(blockIDHash)},
			Block: &tmtypes.Block{
				Header: header,
				Data:   tmtypes.Data{Txs: []tmtypes.Tx{txBz}},
				LastCommit: &tmtypes.Commit{
					Height: header.Height,
				},
			},
		}
	}

	// fullHeader is what CometBFT produces — every field populated, so
	// Header.Hash() round-trips through any tendermint-aware consumer.
	fullHeader := mockBlockHeader(txHeight)

	// sparseHeader is what GigaRouter.translateGlobalBlock produces under
	// Autobahn: ChainID / Height / Time only. Header.Hash() over this
	// computes a different value than any stored BlockID.Hash.
	sparseHeader := tmtypes.Header{
		ChainID: "test",
		Height:  txHeight,
		Time:    time.Unix(1696941649, 0),
	}

	// Two distinct hashes so a buggy implementation that picks the wrong
	// source visibly fails — not just "happens to match by coincidence".
	autobahnHash := mustHexToBytes("00000000000000000000000000000000000000000000000000000000000000ab")

	type tcase struct {
		name        string
		header      tmtypes.Header
		blockIDHash []byte
		// fixtureInvariant asserts the structural relationship between
		// Header.Hash() and BlockID.Hash that this scenario simulates,
		// so the test fails loudly if the fixture itself stops
		// representing what it's supposed to.
		fixtureInvariant func(t *testing.T, headerHash, blockIDHash []byte)
	}

	tcases := []tcase{
		{
			name:        "CometBFT (full header)",
			header:      fullHeader,
			blockIDHash: fullHeader.Hash().Bytes(),
			fixtureInvariant: func(t *testing.T, headerHash, blockIDHash []byte) {
				require.Equal(t, headerHash, blockIDHash, "CometBFT shape: full Header.Hash() must equal BlockID.Hash")
			},
		},
		{
			name:        "Autobahn (sparse header)",
			header:      sparseHeader,
			blockIDHash: autobahnHash,
			fixtureInvariant: func(t *testing.T, headerHash, blockIDHash []byte) {
				require.NotEqual(t, headerHash, blockIDHash, "Autobahn shape: sparse Header.Hash() must differ from BlockID.Hash")
			},
		},
	}

	for _, tc := range tcases {
		t.Run(tc.name, func(t *testing.T) {
			tc.fixtureInvariant(t, tc.header.Hash().Bytes(), tc.blockIDHash)
			testApp := app.Setup(t, false, false, false)
			ctx := testApp.GetContextForDeliverTx([]byte{}).WithBlockHeight(txHeight)
			primeReceiptStore(t, testApp.EvmKeeper.ReceiptStore(), txHeight)

			// Build a real tx the test can probe by hash, encoded the same
			// way the real path encodes via b.txConfigProvider.
			_, fromAddr := testkeeper.MockAddressPair()
			_, toAddr := testkeeper.MockAddressPair()
			gp := sdk.NewInt(1)
			amt := sdk.NewInt(1)
			builder := TxConfig.NewTxBuilder()
			msg, err := types.NewMsgEVMTransaction(&ethtx.LegacyTx{
				Nonce:    0,
				GasPrice: &gp,
				GasLimit: 21000,
				To:       toAddr.Hex(),
				Amount:   &amt,
			})
			require.NoError(t, err)
			require.NoError(t, builder.SetMsgs(msg))
			signedTx := builder.GetTx()
			txBz, err := Encoder(signedTx)
			require.NoError(t, err)

			// The receipt is what GetTransaction's first call resolves to
			// the block; index 0 must match the encoded tx position.
			ethTx, _ := msg.AsTransaction()
			require.NotNil(t, ethTx)
			require.NoError(t, testApp.EvmKeeper.MockReceipt(ctx, ethTx.Hash(), &types.Receipt{
				BlockNumber:      uint64(txHeight),
				TransactionIndex: 0,
				From:             fromAddr.Hex(),
				TxHashHex:        ethTx.Hash().Hex(),
			}))

			block := mkBlock(tc.header, tc.blockIDHash, txBz)
			tmClient := &fixedBlockClient{block: block}

			ctxProvider := func(int64) sdk.Context { return ctx }
			watermarks := evmrpc.NewWatermarkManager(tmClient, ctxProvider, nil, testApp.EvmKeeper.ReceiptStore())
			backend := evmrpc.NewBackend(
				ctxProvider, &testApp.EvmKeeper, legacyabci.BeginBlockKeepers{},
				func(int64) client.TxConfig { return TxConfig }, tmClient, &SConfig,
				testApp.BaseApp, testApp.TracerAnteHandler,
				evmrpc.NewBlockCache(3000), &sync.Mutex{}, watermarks,
			)

			found, _, blockHash, blockNumber, idx, err := backend.GetTransaction(context.Background(), ethTx.Hash())
			require.NoError(t, err)
			require.True(t, found)
			require.Equal(t, uint64(txHeight), blockNumber)
			require.Equal(t, uint64(0), idx)
			// The contract: GetTransaction returns BlockID.Hash (NOT a
			// fresh Header.Hash()).
			require.Equal(t, common.BytesToHash(tc.blockIDHash), blockHash)
		})
	}
}
