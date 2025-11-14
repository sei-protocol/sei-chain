package app

import (
	"crypto/sha256"
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	abci "github.com/tendermint/tendermint/abci/types"

	"github.com/sei-protocol/sei-chain/x/evm/state"
	evmtypes "github.com/sei-protocol/sei-chain/x/evm/types"

	pipelinetypes "github.com/sei-protocol/sei-chain/app/pipeline/types"
)

// ExecutePreprocessedEVMTransaction orchestrates EVM transaction execution
func ExecutePreprocessedEVMTransaction(ctx sdk.Context, preprocessed *pipelinetypes.PreprocessedTx, helper pipelinetypes.ExecutionHelper) (*pipelinetypes.TransactionResult, error) {
	// Set EVM context flags
	evmAddr := common.BytesToAddress(preprocessed.SenderEVMAddr)
	etx := ethtypes.NewTx(preprocessed.TxData.AsEthereumData())
	ctx = ctx.WithIsEVM(true)
	ctx = ctx.WithEVMNonce(etx.Nonce())
	ctx = ctx.WithEVMSenderAddress(evmAddr.Hex())
	ctx = ctx.WithEVMTxHash(etx.Hash().Hex())

	// Validate nonce
	nextNonce := helper.GetNonce(ctx, evmAddr)
	if etx.Nonce() != nextNonce {
		return &pipelinetypes.TransactionResult{
			Code:      sdkerrors.ErrWrongSequence.ABCICode(),
			Codespace: sdkerrors.RootCodespace,
			Log:       fmt.Sprintf("nonce mismatch: expected %d, got %d", nextNonce, etx.Nonce()),
			Surplus:   sdk.ZeroInt(),
		}, sdkerrors.Wrapf(sdkerrors.ErrWrongSequence, "nonce mismatch: expected %d, got %d", nextNonce, etx.Nonce())
	}

	// Create stateDB (single stateDB for both fee deduction and execution)
	stateDB := state.NewDBImpl(ctx, helper.GetKeeper(), false)
	defer stateDB.Cleanup()

	// Get VM block context
	gp := helper.GetGasPool()
	blockCtx, err := helper.GetVMBlockContext(ctx, gp)
	if err != nil {
		return &pipelinetypes.TransactionResult{
			Code:      sdkerrors.ErrInvalidRequest.ABCICode(),
			Codespace: sdkerrors.RootCodespace,
			Log:       err.Error(),
			Surplus:   sdk.ZeroInt(),
		}, err
	}

	// Create EVM instance
	params := helper.GetParams(ctx)
	sstore := params.SeiSstoreSetGasEip2200
	cfg := evmtypes.DefaultChainConfig().EthereumConfigWithSstore(helper.ChainID(ctx), &sstore)
	txCtx := core.NewEVMTxContext(preprocessed.EVMMessage)
	evmInstance := vm.NewEVM(*blockCtx, stateDB, cfg, vm.Config{}, helper.CustomPrecompiles(ctx))
	evmInstance.SetTxContext(txCtx)

	// Create StateTransition - this handles BuyGas() and Execute() together
	st := core.NewStateTransition(evmInstance, preprocessed.EVMMessage, &gp, true, true)

	// Run stateless checks
	if err := st.StatelessChecks(); err != nil {
		return &pipelinetypes.TransactionResult{
			Code:      sdkerrors.ErrWrongSequence.ABCICode(),
			Codespace: sdkerrors.RootCodespace,
			Log:       err.Error(),
			Surplus:   sdk.ZeroInt(),
		}, sdkerrors.Wrap(sdkerrors.ErrWrongSequence, err.Error())
	}

	// BuyGas() - deducts fees
	if err := st.BuyGas(); err != nil {
		return &pipelinetypes.TransactionResult{
			Code:      sdkerrors.ErrInsufficientFunds.ABCICode(),
			Codespace: sdkerrors.RootCodespace,
			Log:       err.Error(),
			Surplus:   sdk.ZeroInt(),
		}, sdkerrors.Wrap(sdkerrors.ErrInsufficientFunds, err.Error())
	}

	// Execute() - runs the transaction and handles gas refunds
	result, err := st.Execute()
	if err != nil {
		return &pipelinetypes.TransactionResult{
			Code:      sdkerrors.ErrInvalidRequest.ABCICode(),
			Codespace: sdkerrors.RootCodespace,
			Log:       err.Error(),
			Surplus:   sdk.ZeroInt(),
		}, err
	}

	// Capture logs before finalizing
	logs := stateDB.GetAllLogs()

	// Finalize stateDB (critical state write) - this writes all state changes including fee deductions
	surplus, err := stateDB.Finalize()
	if err != nil {
		return &pipelinetypes.TransactionResult{
			Code:      sdkerrors.ErrInvalidRequest.ABCICode(),
			Codespace: sdkerrors.RootCodespace,
			Log:       err.Error(),
			Surplus:   sdk.ZeroInt(),
		}, err
	}

	// Handle surplus (if needed)
	if surplus.IsPositive() {
		if err := helper.AddAnteSurplus(ctx, etx.Hash(), surplus); err != nil {
			return &pipelinetypes.TransactionResult{
				Code:      sdkerrors.ErrInvalidRequest.ABCICode(),
				Codespace: sdkerrors.RootCodespace,
				Log:       err.Error(),
				Surplus:   sdk.ZeroInt(),
			}, err
		}
	}

	vmError := ""
	if result.Err != nil {
		vmError = result.Err.Error()
	}

	// Write receipt and deferred info during execution (needed for EndBlock)
	// Set TxIndex in context - this is critical for deferred info storage
	execCtx := ctx.WithTxIndex(preprocessed.TxIndex)
	
	// Write receipt (needed for deferred info bloom)
	receipt, err := helper.GetKeeper().WriteReceipt(execCtx, stateDB, preprocessed.EVMMessage, uint32(etx.Type()), etx.Hash(), result.UsedGas, vmError)
	if err != nil {
		return &pipelinetypes.TransactionResult{
			Code:      sdkerrors.ErrInvalidRequest.ABCICode(),
			Codespace: sdkerrors.RootCodespace,
			Log:       fmt.Sprintf("failed to write receipt: %s", err.Error()),
			Surplus:   sdk.ZeroInt(),
		}, fmt.Errorf("failed to write receipt: %w", err)
	}

	// Write deferred info (needed for EndBlock)
	bloom := ethtypes.Bloom{}
	bloom.SetBytes(receipt.LogsBloom)
	helper.GetKeeper().AppendToEvmTxDeferredInfo(execCtx, bloom, etx.Hash(), surplus)

	return &pipelinetypes.TransactionResult{
		GasUsed:    int64(result.UsedGas), //nolint:gosec // GasUsed is bounded by block gas limit
		ReturnData: result.ReturnData,
		Logs:       logs,
		VmError:    vmError,
		Surplus:    surplus, // Store surplus for finalizer (can be zero)
	}, nil
}

// ExecuteCosmosTransaction executes a COSMOS transaction using existing handlers
func ExecuteCosmosTransaction(ctx sdk.Context, tx sdk.Tx, txBytes []byte, helper pipelinetypes.ExecutionHelper) (*pipelinetypes.TransactionResult, error) {
	// Use existing DeliverTx for COSMOS transactions
	checksum := sha256.Sum256(txBytes)
	resp := helper.DeliverTx(ctx, abci.RequestDeliverTx{Tx: txBytes}, tx, checksum)

	evmTxInfo := resp.EvmTxInfo
	var logs []*ethtypes.Log
	vmError := ""
	if evmTxInfo != nil {
		vmError = evmTxInfo.VmError
		// COSMOS transactions don't have EVM logs
		// Logs would come from stateDB if this was an EVM transaction
	}

	return &pipelinetypes.TransactionResult{
		GasUsed:    resp.GasUsed,
		ReturnData: resp.Data,
		Logs:       logs,
		VmError:    vmError,
		Events:     resp.Events,
		Code:       resp.Code,
		Codespace:  resp.Codespace,
		Log:        resp.Log,
		Surplus:    sdk.ZeroInt(), // COSMOS transactions don't have EVM surplus
	}, nil
}
