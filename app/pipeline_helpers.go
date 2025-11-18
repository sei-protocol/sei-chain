package app

import (
	"math/big"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	abci "github.com/tendermint/tendermint/abci/types"

	evmkeeper "github.com/sei-protocol/sei-chain/x/evm/keeper"
	"github.com/sei-protocol/sei-chain/x/evm/state"
	evmtypes "github.com/sei-protocol/sei-chain/x/evm/types"

	pipelinetypes "github.com/sei-protocol/sei-chain/app/pipeline/types"
)

// appExecutionHelper implements ExecutionHelper for App
type appExecutionHelper struct {
	app *App
}

// Ensure appExecutionHelper implements ExecutionHelper
var _ pipelinetypes.ExecutionHelper = (*appExecutionHelper)(nil)

func newAppExecutionHelper(app *App) *appExecutionHelper {
	return &appExecutionHelper{app: app}
}

func (h *appExecutionHelper) GetBaseFee(ctx sdk.Context) *big.Int {
	return h.app.EvmKeeper.GetBaseFee(ctx)
}

func (h *appExecutionHelper) GetMinimumFeePerGas(ctx sdk.Context) sdk.Dec {
	return h.app.EvmKeeper.GetMinimumFeePerGas(ctx)
}

func (h *appExecutionHelper) GetPriorityNormalizer(ctx sdk.Context) sdk.Dec {
	return h.app.EvmKeeper.GetPriorityNormalizer(ctx)
}

func (h *appExecutionHelper) ChainID(ctx sdk.Context) *big.Int {
	return h.app.EvmKeeper.ChainID(ctx)
}

func (h *appExecutionHelper) EthBlockTestConfigEnabled() bool {
	return h.app.EvmKeeper.EthBlockTestConfig.Enabled
}

func (h *appExecutionHelper) GetGasPool() core.GasPool {
	return h.app.EvmKeeper.GetGasPool()
}

func (h *appExecutionHelper) GetVMBlockContext(ctx sdk.Context, gp core.GasPool) (*vm.BlockContext, error) {
	return h.app.EvmKeeper.GetVMBlockContext(ctx, gp)
}

func (h *appExecutionHelper) GetParams(ctx sdk.Context) evmtypes.Params {
	return h.app.EvmKeeper.GetParams(ctx)
}

func (h *appExecutionHelper) CustomPrecompiles(ctx sdk.Context) map[common.Address]vm.PrecompiledContract {
	return h.app.EvmKeeper.CustomPrecompiles(ctx)
}

func (h *appExecutionHelper) ApplyEVMMessage(ctx sdk.Context, msg *core.Message, stateDB *state.DBImpl, gp core.GasPool, shouldIncrementNonce bool) (*core.ExecutionResult, error) {
	// Duplicate logic from keeper.applyEVMMessage since it's unexported
	blockCtx, err := h.app.EvmKeeper.GetVMBlockContext(ctx, gp)
	if err != nil {
		return nil, err
	}
	params := h.app.EvmKeeper.GetParams(ctx)
	sstore := params.SeiSstoreSetGasEip2200
	cfg := evmtypes.DefaultChainConfig().EthereumConfigWithSstore(h.app.EvmKeeper.ChainID(ctx), &sstore)
	txCtx := core.NewEVMTxContext(msg)
	evmInstance := vm.NewEVM(*blockCtx, stateDB, cfg, vm.Config{}, h.app.EvmKeeper.CustomPrecompiles(ctx))
	evmInstance.SetTxContext(txCtx)
	st := core.NewStateTransition(evmInstance, msg, &gp, true, shouldIncrementNonce)
	return st.Execute()
}

func (h *appExecutionHelper) GetNonce(ctx sdk.Context, addr common.Address) uint64 {
	return h.app.EvmKeeper.GetNonce(ctx, addr)
}

func (h *appExecutionHelper) GetBalance(ctx sdk.Context, addr sdk.AccAddress) sdk.Int {
	// Use BankKeeper to get balance
	denom := h.app.EvmKeeper.GetBaseDenom(ctx)
	balance := h.app.BankKeeper.GetBalance(ctx, addr, denom)
	return balance.Amount
}

func (h *appExecutionHelper) AddAnteSurplus(ctx sdk.Context, hash common.Hash, surplus sdk.Int) error {
	return h.app.EvmKeeper.AddAnteSurplus(ctx, hash, surplus)
}

func (h *appExecutionHelper) DeliverTx(ctx sdk.Context, req abci.RequestDeliverTx, tx sdk.Tx, checksum [32]byte) abci.ResponseDeliverTx {
	return h.app.DeliverTx(ctx, req, tx, checksum)
}

func (h *appExecutionHelper) ProcessTXsWithOCC(ctx sdk.Context, txs [][]byte, typedTxs []sdk.Tx, absoluteTxIndices []int) ([]*abci.ExecTxResult, sdk.Context) {
	return h.app.ProcessTXsWithOCC(ctx, txs, typedTxs, absoluteTxIndices)
}

func (h *appExecutionHelper) DecodeTransactionsConcurrently(ctx sdk.Context, txs [][]byte) []sdk.Tx {
	return h.app.DecodeTransactionsConcurrently(ctx, txs)
}

func (h *appExecutionHelper) GetKeeper() *evmkeeper.Keeper {
	return &h.app.EvmKeeper
}

// appFinalizerHelper implements FinalizerHelper for App
type appFinalizerHelper struct {
	app *App
}

// Ensure appFinalizerHelper implements FinalizerHelper
var _ pipelinetypes.FinalizerHelper = (*appFinalizerHelper)(nil)

func newAppFinalizerHelper(app *App) *appFinalizerHelper {
	return &appFinalizerHelper{app: app}
}

func (h *appFinalizerHelper) GetKeeper() *evmkeeper.Keeper {
	return &h.app.EvmKeeper
}

func (h *appFinalizerHelper) GetPriorityNormalizer(ctx sdk.Context) sdk.Dec {
	return h.app.EvmKeeper.GetPriorityNormalizer(ctx)
}

func (h *appFinalizerHelper) WriteReceipt(ctx sdk.Context, stateDB *state.DBImpl, msg *core.Message, txType uint32, txHash common.Hash, gasUsed uint64, vmError string) (*evmtypes.Receipt, error) {
	return h.app.EvmKeeper.WriteReceipt(ctx, stateDB, msg, txType, txHash, gasUsed, vmError)
}

func (h *appFinalizerHelper) AppendToEvmTxDeferredInfo(ctx sdk.Context, bloom ethtypes.Bloom, txHash common.Hash, surplus sdk.Int) {
	h.app.EvmKeeper.AppendToEvmTxDeferredInfo(ctx, bloom, txHash, surplus)
}

func (h *appFinalizerHelper) FlushTransientReceipts(ctx sdk.Context) error {
	return h.app.EvmKeeper.FlushTransientReceipts(ctx)
}
