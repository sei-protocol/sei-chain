package proxy

import (
	"context"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/params"
	"github.com/holiman/uint256"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	"github.com/stretchr/testify/require"
)

type testApp struct {
	types.BaseApplication
	checkTx func(context.Context, *types.RequestCheckTxV2) *types.ResponseCheckTxV2
}

func (app testApp) CheckTx(ctx context.Context, req *types.RequestCheckTxV2) *types.ResponseCheckTxV2 {
	return app.checkTx(ctx, req)
}

func TestCheckTxSafeReturnsErrorOnPanic(t *testing.T) {
	proxyApp := New(testApp{
		checkTx: func(context.Context, *types.RequestCheckTxV2) *types.ResponseCheckTxV2 {
			panic("boom")
		},
	})
	_, err := proxyApp.CheckTxSafe(t.Context(), &types.RequestCheckTxV2{Tx: []byte("tx")})
	require.Error(t, err)
}

func validEVMResponse() *types.ResponseCheckTxV2 {
	return &types.ResponseCheckTxV2{
		ResponseCheckTx:    &types.ResponseCheckTx{Code: types.CodeTypeOK},
		EVMHash:            common.HexToHash("0x123"),
		IsEVM:              true,
		SeiSenderAddress:   sdk.AccAddress("sender"),
		EVMRequiredBalance: *uint256.NewInt(1),
	}
}

func TestCheckTxSafeReturnsErrorOnMissingEVMHash(t *testing.T) {
	proxyApp := New(testApp{
		checkTx: func(context.Context, *types.RequestCheckTxV2) *types.ResponseCheckTxV2 {
			res := validEVMResponse()
			res.EVMHash = common.Hash{}
			return res
		},
	})
	_, err := proxyApp.CheckTxSafe(t.Context(), &types.RequestCheckTxV2{Tx: []byte("tx")})
	require.Error(t, err)
}

func TestCheckTxSafeReturnsErrorOnMissingSeiSenderAddress(t *testing.T) {
	proxyApp := New(testApp{
		checkTx: func(context.Context, *types.RequestCheckTxV2) *types.ResponseCheckTxV2 {
			res := validEVMResponse()
			res.SeiSenderAddress = nil
			return res
		},
	})
	_, err := proxyApp.CheckTxSafe(t.Context(), &types.RequestCheckTxV2{Tx: []byte("tx")})
	require.Error(t, err)
}

func TestCheckTxSafeAllowsValidEVMResponse(t *testing.T) {
	proxyApp := New(testApp{
		checkTx: func(context.Context, *types.RequestCheckTxV2) *types.ResponseCheckTxV2 {
			return validEVMResponse()
		},
	})
	_, err := proxyApp.CheckTxSafe(t.Context(), &types.RequestCheckTxV2{Tx: []byte("tx")})
	require.NoError(t, err)
}

func TestEvmCodeErrorsWhenApplicationDoesNotSupportIt(t *testing.T) {
	proxyApp := New(testApp{})

	_, err := proxyApp.EvmCode(common.Address{1})

	require.Error(t, err)
}

type testEvmCodeReaderApp struct {
	testApp
	code func(common.Address) []byte
}

func (app testEvmCodeReaderApp) EvmCode(addr common.Address) []byte {
	return app.code(addr)
}

func TestEvmCodeDelegatesToASupportingApplication(t *testing.T) {
	want := []byte{0x60, 0x00, 0xf3}
	var gotAddr common.Address
	proxyApp := New(testEvmCodeReaderApp{
		code: func(addr common.Address) []byte {
			gotAddr = addr
			return want
		},
	})
	addr := common.Address{7}

	got, err := proxyApp.EvmCode(addr)

	require.NoError(t, err)
	require.Equal(t, want, got)
	require.Equal(t, addr, gotAddr)
}

type testEvmStorageReaderApp struct {
	testApp
	storage func(common.Address, common.Hash) common.Hash
}

func (app testEvmStorageReaderApp) EvmStorage(addr common.Address, key common.Hash) common.Hash {
	return app.storage(addr, key)
}

func TestEvmStorage(t *testing.T) {
	addr := common.Address{7}
	key := common.Hash{3}
	want := common.Hash{0x2a}

	t.Run("delegates to a supporting application", func(t *testing.T) {
		var gotAddr common.Address
		var gotKey common.Hash
		proxyApp := New(testEvmStorageReaderApp{
			storage: func(addr common.Address, key common.Hash) common.Hash {
				gotAddr, gotKey = addr, key
				return want
			},
		})

		got, err := proxyApp.EvmStorage(addr, key)

		require.NoError(t, err)
		require.Equal(t, want, got)
		require.Equal(t, addr, gotAddr)
		require.Equal(t, key, gotKey)
	})

	t.Run("errors when the application does not support it", func(t *testing.T) {
		_, err := New(testApp{}).EvmStorage(addr, key)

		require.Error(t, err)
	})
}

// testEvmHistoryApp serves every height's state as values derived from that height.
type testEvmHistoryApp struct {
	testApp
}

func (testEvmHistoryApp) EvmBalanceAt(_ common.Address, height int64) (uint256.Int, error) {
	return *uint256.NewInt(uint64(height)), nil //nolint:gosec // test heights are positive
}

func (testEvmHistoryApp) EvmCodeAt(_ common.Address, height int64) ([]byte, error) {
	return []byte{byte(height)}, nil //nolint:gosec // test heights are small
}

func (testEvmHistoryApp) EvmNonceAt(_ common.Address, height int64) (uint64, error) {
	return uint64(height), nil //nolint:gosec // test heights are positive
}

func (testEvmHistoryApp) EvmStorageAt(_ common.Address, _ common.Hash, height int64) (common.Hash, error) {
	return common.Hash{byte(height)}, nil //nolint:gosec // test heights are small
}

func TestEvmHistoryReads(t *testing.T) {
	addr, key := common.Address{7}, common.Hash{3}
	for _, tc := range []struct {
		name string
		read func(*Proxy) (any, error)
		want any
	}{
		{"balance", func(p *Proxy) (any, error) { return p.EvmBalanceAt(addr, 5) }, *uint256.NewInt(5)},
		{"code", func(p *Proxy) (any, error) { return p.EvmCodeAt(addr, 5) }, []byte{5}},
		{"nonce", func(p *Proxy) (any, error) { return p.EvmNonceAt(addr, 5) }, uint64(5)},
		{"storage", func(p *Proxy) (any, error) { return p.EvmStorageAt(addr, key, 5) }, common.Hash{5}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := tc.read(New(testEvmHistoryApp{}))
			require.NoError(t, err)
			require.Equal(t, tc.want, got)

			_, err = tc.read(New(testApp{}))
			require.ErrorIs(t, err, errNoEVMHistory)
		})
	}
}

// testEVMApp answers every EVMApplication method with a fixed value and
// records the arguments it was called with.
type testEVMApp struct {
	testApp
	gotMsg    *core.Message
	gotCalls  []*core.Message
	gotGasCap uint64
}

var _ EVMApplication = (*testEVMApp)(nil)

var (
	testExecutionResult = &core.ExecutionResult{ReturnData: []byte{0x2a}}
	testAccessList      = ethtypes.AccessList{{Address: common.Address{0x01}, StorageKeys: []common.Hash{{0x02}}}}
	testChainConfig     = &params.ChainConfig{ChainID: big.NewInt(713715)}
	testBaseFee         = big.NewInt(7)
	testMinGasPrice     = big.NewInt(1_000_000_000)
)

func (app *testEVMApp) EvmCall(_ context.Context, msg *core.Message) (*core.ExecutionResult, error) {
	app.gotMsg = msg
	return testExecutionResult, nil
}

func (app *testEVMApp) EvmEstimateGas(_ context.Context, msg *core.Message, gasCap uint64) (uint64, []byte, error) {
	app.gotMsg, app.gotGasCap = msg, gasCap
	return 21_000, []byte{0x01}, nil
}

func (app *testEVMApp) EvmEstimateGasAfterCalls(_ context.Context, calls []*core.Message, msg *core.Message, gasCap uint64) (uint64, []byte, error) {
	app.gotCalls, app.gotMsg, app.gotGasCap = calls, msg, gasCap
	return 42_000, []byte{0x02}, nil
}

func (app *testEVMApp) EvmCreateAccessList(_ context.Context, msg *core.Message) (ethtypes.AccessList, *core.ExecutionResult, error) {
	app.gotMsg = msg
	return testAccessList, testExecutionResult, nil
}

func (app *testEVMApp) EvmChainID() uint64                  { return 713715 }
func (app *testEVMApp) EvmChainConfig() *params.ChainConfig { return testChainConfig }
func (app *testEVMApp) EvmBaseFee() *big.Int                { return testBaseFee }
func (app *testEVMApp) EvmGasLimit() uint64                 { return 35_000_000 }
func (app *testEVMApp) EvmMinGasPrice() *big.Int            { return testMinGasPrice }

func TestEVMIsAbsentForANonEVMApplication(t *testing.T) {
	// Test: wrap an application that is not an EVMApplication.
	proxyApp := New(testApp{})

	// Verify: no EVM proxy is exposed.
	require.False(t, proxyApp.EVM().IsPresent())
}

func TestEVMProxyDelegatesToTheApplication(t *testing.T) {
	// Setup: an EVMApplication wrapped by the proxy.
	app := &testEVMApp{}
	evm, ok := New(app).EVM().Get()
	require.True(t, ok)
	msg := &core.Message{GasLimit: 21_000}

	t.Run("call", func(t *testing.T) {
		// Test: EvmCall.
		got, err := evm.EvmCall(t.Context(), msg)

		// Verify: the app's result is returned for the message passed in.
		require.NoError(t, err)
		require.Same(t, testExecutionResult, got)
		require.Same(t, msg, app.gotMsg)
	})

	t.Run("estimate gas", func(t *testing.T) {
		// Test: EvmEstimateGas with a gas cap.
		got, revert, err := evm.EvmEstimateGas(t.Context(), msg, 10_000_000)

		// Verify: the app's estimate and revert data are returned for the message and cap.
		require.NoError(t, err)
		require.Equal(t, uint64(21_000), got)
		require.Equal(t, []byte{0x01}, revert)
		require.Same(t, msg, app.gotMsg)
		require.Equal(t, uint64(10_000_000), app.gotGasCap)
	})

	t.Run("estimate gas after calls", func(t *testing.T) {
		// Test: EvmEstimateGasAfterCalls with one prior call and a gas cap.
		calls := []*core.Message{{GasLimit: 30_000}}
		got, revert, err := evm.EvmEstimateGasAfterCalls(t.Context(), calls, msg, 10_000_000)

		// Verify: the app's estimate and revert data are returned for the calls, message and cap.
		require.NoError(t, err)
		require.Equal(t, uint64(42_000), got)
		require.Equal(t, []byte{0x02}, revert)
		require.Equal(t, calls, app.gotCalls)
		require.Same(t, msg, app.gotMsg)
		require.Equal(t, uint64(10_000_000), app.gotGasCap)
	})

	t.Run("create access list", func(t *testing.T) {
		// Test: EvmCreateAccessList.
		accessList, result, err := evm.EvmCreateAccessList(t.Context(), msg)

		// Verify: the app's access list and result are returned for the message passed in.
		require.NoError(t, err)
		require.Equal(t, testAccessList, accessList)
		require.Same(t, testExecutionResult, result)
		require.Same(t, msg, app.gotMsg)
	})

	t.Run("execution parameters", func(t *testing.T) {
		// Verify: chain ID, chain config, base fee, gas limit and min gas price come from the app.
		require.Equal(t, uint64(713715), evm.EvmChainID())
		require.Same(t, testChainConfig, evm.EvmChainConfig())
		require.Same(t, testBaseFee, evm.EvmBaseFee())
		require.Equal(t, uint64(35_000_000), evm.EvmGasLimit())
		require.Same(t, testMinGasPrice, evm.EvmMinGasPrice())
	})
}
