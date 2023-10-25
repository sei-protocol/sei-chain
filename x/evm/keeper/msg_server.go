package keeper

import (
	"context"
	"math"
	"math/big"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/core"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/sei-protocol/sei-chain/x/evm/state"
	"github.com/sei-protocol/sei-chain/x/evm/types"
)

type msgServer struct {
	Keeper
}

// NewMsgServerImpl returns an implementation of the MsgServer interface
// for the provided Keeper.
func NewMsgServerImpl(keeper Keeper) types.MsgServer {
	return &msgServer{Keeper: keeper}
}

var _ types.MsgServer = msgServer{}

func (server msgServer) EVMTransaction(goCtx context.Context, msg *types.MsgEVMTransaction) (serverRes *types.MsgEVMTransactionResponse, err error) {
	ctx := sdk.UnwrapSDKContext(goCtx)
	stateDB := state.NewDBImpl(ctx, &server)
	tx, _ := msg.AsTransaction()
	ctx, gp := server.getGasPool(ctx)
	emsg, err := server.getEVMMessage(ctx, tx)
	if err != nil {
		return
	}

	success := true
	defer func() {
		err = server.writeReceipt(ctx, tx, emsg, serverRes.GasUsed, success)
		if err != nil {
			return
		}
		err = stateDB.Finalize()
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
	cfg := server.GetChainConfig(ctx).EthereumConfig(server.ChainID())
	signer := ethtypes.MakeSigner(cfg, big.NewInt(ctx.BlockHeight()), uint64(ctx.BlockTime().Unix()))
	return core.TransactionToMessage(tx, signer, nil)
}

func (server msgServer) applyEVMMessage(ctx sdk.Context, msg *core.Message, stateDB vm.StateDB, gp core.GasPool) (*core.ExecutionResult, error) {
	blockCtx, err := server.GetVMBlockContext(ctx, gp)
	if err != nil {
		return nil, err
	}
	cfg := server.GetChainConfig(ctx).EthereumConfig(server.ChainID())
	txCtx := core.NewEVMTxContext(msg)
	evmInstance := vm.NewEVM(*blockCtx, txCtx, stateDB, cfg, vm.Config{})
	st := core.NewStateTransition(evmInstance, msg, &gp)
	return st.TransitionDb()
}

func (server msgServer) writeReceipt(ctx sdk.Context, tx *ethtypes.Transaction, msg *core.Message, usedGas uint64, success bool) error {
	cumulativeGasUsed := usedGas
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
		GasUsed:           usedGas,
		BlockNumber:       uint64(ctx.BlockHeight()),
		TransactionIndex:  uint32(ctx.TxIndex()),
		EffectiveGasPrice: tx.GasPrice().Uint64(),
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

	if sender, found := types.GetContextEVMAddress(ctx); found {
		receipt.From = sender.Hex()
	} else {
		ctx.Logger().Error("sender cannot be found for EVM transaction")
	}

	return server.SetReceipt(ctx, tx.Hash(), receipt)
}
