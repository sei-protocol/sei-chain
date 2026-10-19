package proxy

import (
	"context"
	"fmt"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
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

func TestEvmCallErrorsWhenApplicationDoesNotSupportIt(t *testing.T) {
	proxyApp := New(testApp{})

	_, err := proxyApp.EvmCall(t.Context(), &core.Message{})

	require.Error(t, err)
}

type testEvmCallerApp struct {
	testApp
	call func(context.Context, *core.Message) (*core.ExecutionResult, error)
}

func (app testEvmCallerApp) EvmCall(ctx context.Context, msg *core.Message) (*core.ExecutionResult, error) {
	return app.call(ctx, msg)
}

func TestEvmCallDelegatesToASupportingApplication(t *testing.T) {
	want := &core.ExecutionResult{ReturnData: []byte{0x2a}}
	var gotMsg *core.Message
	proxyApp := New(testEvmCallerApp{
		call: func(_ context.Context, msg *core.Message) (*core.ExecutionResult, error) {
			gotMsg = msg
			return want, nil
		},
	})
	msg := &core.Message{GasLimit: 21_000}

	got, err := proxyApp.EvmCall(t.Context(), msg)

	require.NoError(t, err)
	require.Same(t, want, got)
	require.Same(t, msg, gotMsg)
}

func TestEvmEstimateGasErrorsWhenApplicationDoesNotSupportIt(t *testing.T) {
	proxyApp := New(testApp{})

	_, _, err := proxyApp.EvmEstimateGas(t.Context(), &core.Message{}, 0)

	require.Error(t, err)
}

type testEvmGasEstimatorApp struct {
	testApp
	estimateGas func(context.Context, *core.Message, uint64) (uint64, []byte, error)
}

func (app testEvmGasEstimatorApp) EvmEstimateGas(ctx context.Context, msg *core.Message, gasCap uint64) (uint64, []byte, error) {
	return app.estimateGas(ctx, msg, gasCap)
}

func TestEvmEstimateGasDelegatesToASupportingApplication(t *testing.T) {
	var gotMsg *core.Message
	var gotGasCap uint64
	proxyApp := New(testEvmGasEstimatorApp{
		estimateGas: func(_ context.Context, msg *core.Message, gasCap uint64) (uint64, []byte, error) {
			gotMsg = msg
			gotGasCap = gasCap
			return 21_000, nil, nil
		},
	})
	msg := &core.Message{GasLimit: 21_000}

	got, revert, err := proxyApp.EvmEstimateGas(t.Context(), msg, 10_000_000)

	require.NoError(t, err)
	require.Equal(t, uint64(21_000), got)
	require.Empty(t, revert)
	require.Same(t, msg, gotMsg)
	require.Equal(t, uint64(10_000_000), gotGasCap)
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

func TestEvmChainConfigErrorsWhenApplicationDoesNotSupportIt(t *testing.T) {
	proxyApp := New(testApp{})

	_, err := proxyApp.EvmChainConfig()

	require.Error(t, err)
}

type testEvmChainConfigApp struct {
	testApp
	chainConfig *params.ChainConfig
}

func (app testEvmChainConfigApp) EvmChainConfig() *params.ChainConfig {
	return app.chainConfig
}

func TestEvmChainConfigDelegatesToASupportingApplication(t *testing.T) {
	want := &params.ChainConfig{ChainID: big.NewInt(713715)}
	proxyApp := New(testEvmChainConfigApp{chainConfig: want})

	got, err := proxyApp.EvmChainConfig()

	require.NoError(t, err)
	require.Same(t, want, got)
}

func TestEvmBaseFeeErrorsWhenApplicationDoesNotSupportIt(t *testing.T) {
	proxyApp := New(testApp{})

	_, err := proxyApp.EvmBaseFee()

	require.Error(t, err)
}

func TestEvmGasLimitErrorsWhenApplicationDoesNotSupportIt(t *testing.T) {
	proxyApp := New(testApp{})

	_, err := proxyApp.EvmGasLimit()

	require.Error(t, err)
}

type testEvmBaseFeeApp struct {
	testApp
	baseFee *big.Int
}

func (app testEvmBaseFeeApp) EvmBaseFee() *big.Int {
	return app.baseFee
}

func TestEvmBaseFeeDelegatesToASupportingApplication(t *testing.T) {
	want := big.NewInt(7)
	proxyApp := New(testEvmBaseFeeApp{baseFee: want})

	got, err := proxyApp.EvmBaseFee()

	require.NoError(t, err)
	require.Same(t, want, got)
}

type testEvmGasLimitApp struct {
	testApp
	gasLimit uint64
}

func (app testEvmGasLimitApp) EvmGasLimit() uint64 {
	return app.gasLimit
}

func TestEvmGasLimitDelegatesToASupportingApplication(t *testing.T) {
	proxyApp := New(testEvmGasLimitApp{gasLimit: 35_000_000})

	got, err := proxyApp.EvmGasLimit()

	require.NoError(t, err)
	require.Equal(t, uint64(35_000_000), got)
}

func TestEvmMinGasPriceErrorsWhenApplicationDoesNotSupportIt(t *testing.T) {
	proxyApp := New(testApp{})
	_, err := proxyApp.EvmMinGasPrice()
	require.Error(t, err)
}

type testEvmMinGasPriceApp struct {
	testApp
	minGasPrice *big.Int
}

func (app testEvmMinGasPriceApp) EvmMinGasPrice() *big.Int {
	return app.minGasPrice
}

func TestEvmMinGasPriceDelegatesToASupportingApplication(t *testing.T) {
	want := big.NewInt(1_000_000_000)
	proxyApp := New(testEvmMinGasPriceApp{minGasPrice: want})
	got, err := proxyApp.EvmMinGasPrice()
	require.NoError(t, err)
	require.Equal(t, want, got)
}

func TestEvmChainIDDelegatesToApplication(t *testing.T) {
	proxyApp := New(testApp{})

	// Test: EvmChainID is on Application, so every wrapped app exposes it.
	got := proxyApp.EvmChainID()

	// Verify: BaseApplication's zero chain ID is returned.
	require.Equal(t, uint64(0), got)
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

// tracedAt is the error testEvmTracerApp answers a state read with, naming what it was asked for.
type tracedAt int64

func (h tracedAt) Error() string { return fmt.Sprintf("traced at %d", int64(h)) }

// testEvmTracerApp answers each tracing read with a value derived from its input.
type testEvmTracerApp struct {
	testApp
}

func (testEvmTracerApp) EvmBlockContext(header *ethtypes.Header) (vm.BlockContext, error) {
	return vm.BlockContext{BlockNumber: header.Number}, nil
}

func (testEvmTracerApp) EvmStateAt(_ context.Context, height int64) (vm.StateDB, func(), error) {
	return nil, nil, tracedAt(height)
}

func (testEvmTracerApp) EvmStateBeforeTx(_ context.Context, _ *ethtypes.Header, txs [][]byte) (vm.StateDB, func(), error) {
	return nil, nil, tracedAt(len(txs))
}

func TestEvmTracerReads(t *testing.T) {
	header := &ethtypes.Header{Number: big.NewInt(5)}
	for _, tc := range []struct {
		name    string
		read    func(*Proxy) error
		wantErr error
	}{
		{"block context", func(p *Proxy) error {
			blockCtx, err := p.EvmBlockContext(header)
			if err == nil && blockCtx.BlockNumber.Cmp(header.Number) != 0 {
				return fmt.Errorf("block context for block %s", blockCtx.BlockNumber)
			}
			return err
		}, nil},
		{"state after a block", func(p *Proxy) error {
			_, _, err := p.EvmStateAt(t.Context(), 5)
			return err
		}, tracedAt(5)},
		{"state before a transaction", func(p *Proxy) error {
			_, _, err := p.EvmStateBeforeTx(t.Context(), header, [][]byte{{1}, {2}})
			return err
		}, tracedAt(2)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.read(New(testEvmTracerApp{}))
			if tc.wantErr == nil {
				require.NoError(t, err)
			} else {
				require.ErrorIs(t, err, tc.wantErr)
			}

			require.ErrorIs(t, tc.read(New(testApp{})), errNoEVMTracing)
		})
	}
}
