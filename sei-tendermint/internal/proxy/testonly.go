package proxy

import (
	"context"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/params"
	"github.com/holiman/uint256"

	"github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	atypes "github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	tmproto "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"
)

// NewTestEVMProxy wraps an ABCI fixture for Autobahn tests. EVM read methods panic.
func NewTestEVMProxy(app types.Application) *EVMProxy {
	return NewEVM(NewTestEVMApplication(app))
}

type testEVMApplication struct {
	app types.Application
}

func (app *testEVMApplication) EvmCall(_ context.Context, msg *core.Message) (*core.ExecutionResult, error) {
	panic("EVM read methods are unavailable in this test fixture")
}

func (app *testEVMApplication) EvmEstimateGas(_ context.Context, msg *core.Message, gasCap uint64) (uint64, []byte, error) {
	panic("EVM read methods are unavailable in this test fixture")
}

func (app *testEVMApplication) EvmCallAt(_ context.Context, msg *core.Message, header *ethtypes.Header) (*core.ExecutionResult, error) {
	panic("EVM read methods are unavailable in this test fixture")
}

func (app *testEVMApplication) EvmEstimateGasAt(_ context.Context, msg *core.Message, gasCap uint64, header *ethtypes.Header) (uint64, []byte, error) {
	panic("EVM read methods are unavailable in this test fixture")
}

func (app *testEVMApplication) EvmEstimateGasAfterCalls(_ context.Context, calls []*core.Message, msg *core.Message, gasCap uint64) (uint64, []byte, error) {
	panic("EVM read methods are unavailable in this test fixture")
}

func (app *testEVMApplication) EvmCreateAccessList(_ context.Context, msg *core.Message) (ethtypes.AccessList, *core.ExecutionResult, error) {
	panic("EVM read methods are unavailable in this test fixture")
}

func (app *testEVMApplication) EvmChainID() uint64 {
	panic("EVM read methods are unavailable in this test fixture")
}

func (app *testEVMApplication) EvmChainConfig() *params.ChainConfig {
	panic("EVM read methods are unavailable in this test fixture")
}

func (app *testEVMApplication) EvmBaseFee() *big.Int {
	panic("EVM read methods are unavailable in this test fixture")
}

func (app *testEVMApplication) EvmGasLimit() uint64 {
	panic("EVM read methods are unavailable in this test fixture")
}

func (app *testEVMApplication) EvmMinGasPrice() *big.Int {
	panic("EVM read methods are unavailable in this test fixture")
}

func (app *testEVMApplication) EvmBlockContext(header *ethtypes.Header) (vm.BlockContext, error) {
	panic("EVM read methods are unavailable in this test fixture")
}

func (app *testEVMApplication) EvmStateAt(_ context.Context, height int64) (vm.StateDB, func(), error) {
	panic("EVM read methods are unavailable in this test fixture")
}

func (app *testEVMApplication) EvmStateBeforeTx(_ context.Context, header *ethtypes.Header, txs [][]byte) (vm.StateDB, func(), error) {
	panic("EVM read methods are unavailable in this test fixture")
}

func (a *testEVMApplication) Info() *ResponseInfo {
	r := a.app.Info()
	return &ResponseInfo{Data: r.Data, Version: r.Version, AppVersion: r.AppVersion, LastBlockHeight: r.LastBlockHeight, LastBlockAppHash: r.LastBlockAppHash, MinimumGasPrices: r.MinimumGasPrices}
}
func (a *testEVMApplication) GetValidators() []ValidatorUpdate {
	return testValidators(a.app.GetValidators())
}
func testValidators(vals []types.ValidatorUpdate) []ValidatorUpdate {
	out := make([]ValidatorUpdate, len(vals))
	for i, v := range vals {
		out[i] = ValidatorUpdate{PubKey: utils.OrPanic1(atypes.PublicKeyFromBytes(v.PubKey.GetEd25519())), Power: v.Power}
	}
	return out
}
func (a *testEVMApplication) LastBlockHeight() int64              { return a.app.LastBlockHeight() }
func (a *testEVMApplication) EvmNonce(addr common.Address) uint64 { return a.app.EvmNonce(addr) }
func (a *testEVMApplication) EvmBalance(addr common.Address, seiAddr []byte) uint256.Int {
	return a.app.EvmBalance(addr, seiAddr)
}
func (a *testEVMApplication) InitChain(req *RequestInitChain) (*ResponseInitChain, error) {
	r, err := a.app.InitChain(&types.RequestInitChain{Time: req.Time, ChainId: req.ChainID, InitialHeight: req.InitialHeight, AppStateBytes: req.AppStateBytes, ConsensusParams: &tmproto.ConsensusParams{Block: &tmproto.BlockParams{MaxGas: req.MaxGas}}})
	if err != nil {
		return nil, err
	}
	return &ResponseInitChain{Validators: testValidators(r.Validators), AppHash: r.AppHash}, nil
}
func testHeader(h *Header) *tmproto.Header {
	if h == nil {
		return nil
	}
	return &tmproto.Header{ChainID: h.ChainID, Height: h.Height, Time: h.Time, ProposerAddress: h.ProposerAddress}
}
func (a *testEVMApplication) InitLastHeader(h *Header) { a.app.InitLastHeader(testHeader(h)) }
func (a *testEVMApplication) CheckTx(ctx context.Context, req *RequestCheckTxV2) *ResponseCheckTxV2 {
	r := a.app.CheckTx(ctx, &types.RequestCheckTxV2{Tx: req.Tx})
	if r == nil {
		return nil
	}
	return &ResponseCheckTxV2{ResponseCheckTx: &ResponseCheckTx{Code: r.Code, Data: r.Data, Log: r.Log, GasWanted: r.GasWanted, Codespace: r.Codespace, Priority: r.Priority, GasEstimated: r.GasEstimated}, IsEVM: r.IsEVM, EVMNonce: r.EVMNonce, EVMHash: r.EVMHash, EVMSenderAddress: r.EVMSenderAddress, SeiSenderAddress: r.SeiSenderAddress, EVMRequiredBalance: r.EVMRequiredBalance}
}
func (a *testEVMApplication) FinalizeBlock(ctx context.Context, req *RequestFinalizeBlock) (*ResponseFinalizeBlock, error) {
	r, err := a.app.FinalizeBlock(ctx, &types.RequestFinalizeBlock{Txs: req.Txs, Hash: req.Hash, Header: testHeader(req.Header)})
	if err != nil {
		return nil, err
	}
	txs := make([]*ExecTxResult, len(r.TxResults))
	for i, v := range r.TxResults {
		txs[i] = &ExecTxResult{Code: v.Code, Data: v.Data, Log: v.Log, Info: v.Info, GasWanted: v.GasWanted, GasUsed: v.GasUsed, Codespace: v.Codespace}
	}
	return &ResponseFinalizeBlock{TxResults: txs, AppHash: r.AppHash}, nil
}
func (a *testEVMApplication) Commit(ctx context.Context) (*ResponseCommit, error) {
	r, err := a.app.Commit(ctx)
	if err != nil {
		return nil, err
	}
	return &ResponseCommit{RetainHeight: r.RetainHeight}, nil
}

// NewTestEVMApplication adapts an ABCI fixture to the plain Autobahn application API.
// EVM read methods panic.
func NewTestEVMApplication(app types.Application) EVMApplication {
	return &testEVMApplication{app: app}
}
