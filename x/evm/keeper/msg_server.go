package keeper

import (
	"context"
	"fmt"
	"math"
	"math/big"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/sei-protocol/sei-chain/x/evm/state"
	"github.com/sei-protocol/sei-chain/x/evm/types"
)

type msgServer struct {
	*Keeper
}

// NewMsgServerImpl returns an implementation of the MsgServer interface
// for the provided Keeper.
func NewMsgServerImpl(keeper *Keeper) types.MsgServer {
	return &msgServer{Keeper: keeper}
}

var _ types.MsgServer = msgServer{}

func (server msgServer) EVMTransaction(goCtx context.Context, msg *types.MsgEVMTransaction) (serverRes *types.MsgEVMTransactionResponse, err error) {
	if msg.IsAssociateTx() {
		// no-op in msg server for associate tx; all the work have been done in ante handler
		return &types.MsgEVMTransactionResponse{}, nil
	}
	ctx := sdk.UnwrapSDKContext(goCtx)
	// EVM has a special case here, mainly because for an EVM transaction the gas limit is set on EVM payload level, not on top-level GasWanted field
	// as normal transactions (because existing eth client can't). As a result EVM has its own dedicated ante handler chain. The full sequence is:

	// 	1. At the beginning of the ante handler chain, gas meter is set to infinite so that the ante processing itself won't run out of gas (EVM ante is pretty light but it does read a parameter or two)
	// 	2. At the end of the ante handler chain, gas meter is set based on the gas limit specified in the EVM payload; this is only to provide a GasWanted return value to tendermint mempool when CheckTx returns, and not used for anything else.
	// 	3. At the beginning of message server (here), gas meter is set to infinite again, because EVM internal logic will then take over and manage out-of-gas scenarios.
	// 	4. At the end of message server, gas consumed by EVM is adjusted to Sei's unit and counted in the original gas meter, because that original gas meter will be used to count towards block gas after message server returns
	originalGasMeter := ctx.GasMeter()
	ctx = ctx.WithGasMeter(sdk.NewInfiniteGasMeter())

	stateDB := state.NewDBImpl(ctx, &server, false)
	tx, _ := msg.AsTransaction()
	ctx, gp := server.getGasPool(ctx)
	emsg, err := server.getEVMMessage(ctx, tx)
	if err != nil {
		ctx.Logger().Error(fmt.Sprintf("EVM message server error: getting EVM message failed due to %s", err))
		return
	}

	success := true
	defer func() {
		if pe := recover(); pe != nil {
			ctx.Logger().Error(fmt.Sprintf("EVM PANIC: %s", pe))
			panic(pe)
		}
		err = server.writeReceipt(ctx, msg, tx, emsg, serverRes, success)
		if err != nil {
			ctx.Logger().Error(fmt.Sprintf("failed to write EVM receipt: %s", err))
			return
		}
		err = stateDB.Finalize()
		if err != nil {
			ctx.Logger().Error(fmt.Sprintf("failed to finalize EVM stateDB: %s", err))
			return
		}

		// GasUsed in serverRes is in EVM's gas unit, not Sei's gas unit.
		// PriorityNormalizer is the coefficient that's used to adjust EVM
		// transactions' priority, which is based on gas limit in EVM unit,
		// to Sei transactions' priority, which is based on gas limit in
		// Sei unit, so we use the same coefficient to convert gas unit here.
		adjustedGasUsed := server.GetPriorityNormalizer(ctx).MulInt64(int64(serverRes.GasUsed))
		originalGasMeter.ConsumeGas(adjustedGasUsed.RoundInt().Uint64(), "evm transaction")
	}()

	res, applyErr := server.applyEVMMessage(ctx, emsg, stateDB, gp)
	serverRes = &types.MsgEVMTransactionResponse{
		Hash: tx.Hash().Hex(),
	}
	if applyErr != nil {
		success = false
		serverRes.VmError = applyErr.Error()
		serverRes.GasUsed = tx.Gas() // all gas will be considered as used
	} else {
		// if applyErr is nil then res must be non-nil
		if res.Err != nil {
			serverRes.VmError = res.Err.Error()
			success = false
		}
		serverRes.GasUsed = res.UsedGas
		serverRes.ReturnData = res.ReturnData
	}

	return
}

func (server msgServer) getGasPool(ctx sdk.Context) (sdk.Context, core.GasPool) {
	if ctx.BlockGasMeter() == nil {
		ctx = ctx.WithBlockGasMeter(sdk.NewInfiniteGasMeter())
	}
	if ctx.BlockGasMeter().Limit() == 0 {
		// infinite gas meter
		return ctx, math.MaxUint64
	}
	return ctx, core.GasPool(ctx.BlockGasMeter().Limit() - ctx.BlockGasMeter().GasConsumedToLimit())
}

func (server msgServer) getEVMMessage(ctx sdk.Context, tx *ethtypes.Transaction) (*core.Message, error) {
	cfg := server.GetChainConfig(ctx).EthereumConfig(server.ChainID(ctx))
	signer := ethtypes.MakeSigner(cfg, big.NewInt(ctx.BlockHeight()), uint64(ctx.BlockTime().Unix()))
	return core.TransactionToMessage(tx, signer, nil)
}

func (server msgServer) applyEVMMessage(ctx sdk.Context, msg *core.Message, stateDB vm.StateDB, gp core.GasPool) (*core.ExecutionResult, error) {
	blockCtx, err := server.GetVMBlockContext(ctx, gp)
	if err != nil {
		return nil, err
	}
	cfg := server.GetChainConfig(ctx).EthereumConfig(server.ChainID(ctx))
	txCtx := core.NewEVMTxContext(msg)
	evmInstance := vm.NewEVM(*blockCtx, txCtx, stateDB, cfg, vm.Config{})
	st := core.NewStateTransition(evmInstance, msg, &gp)
	return st.TransitionDb()
}

func (server msgServer) writeReceipt(ctx sdk.Context, origMsg *types.MsgEVMTransaction, tx *ethtypes.Transaction, msg *core.Message, response *types.MsgEVMTransactionResponse, success bool) error {
	cumulativeGasUsed := response.GasUsed
	if ctx.BlockGasMeter() != nil {
		limit := ctx.BlockGasMeter().Limit()
		cumulativeGasUsed += ctx.BlockGasMeter().GasConsumed()
		if cumulativeGasUsed > limit {
			cumulativeGasUsed = limit
		}
	}

	receipt := &types.Receipt{
		TxType:            uint32(tx.Type()),
		CumulativeGasUsed: cumulativeGasUsed,
		TxHashHex:         tx.Hash().Hex(),
		GasUsed:           response.GasUsed,
		BlockNumber:       uint64(ctx.BlockHeight()),
		TransactionIndex:  uint32(ctx.TxIndex()),
		EffectiveGasPrice: tx.GasPrice().Uint64(),
		VmError:           response.VmError,
	}

	if msg.To == nil {
		receipt.ContractAddress = crypto.CreateAddress(msg.From, msg.Nonce).Hex()
	} else {
		receipt.To = msg.To.Hex()
		if len(msg.Data) > 0 {
			receipt.ContractAddress = msg.To.Hex()
		}
	}

	if success {
		receipt.Status = uint32(ethtypes.ReceiptStatusSuccessful)
	} else {
		receipt.Status = uint32(ethtypes.ReceiptStatusFailed)
	}

	receipt.From = common.BytesToAddress(origMsg.Derived.SenderEVMAddr).Hex()

	return server.SetReceipt(ctx, tx.Hash(), receipt)
}
