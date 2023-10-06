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
	tmtypes "github.com/tendermint/tendermint/types"
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
	stateDB := state.NewStateDBImpl(ctx, &server)
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
	} else {
		return ctx, core.GasPool(ctx.BlockGasMeter().Limit() - ctx.BlockGasMeter().GasConsumedToLimit())
	}
}

func (server msgServer) getEVMMessage(ctx sdk.Context, tx *ethtypes.Transaction) (*core.Message, error) {
	cfg := server.GetChainConfig(ctx).EthereumConfig(server.ChainID())
	signer := ethtypes.MakeSigner(cfg, big.NewInt(ctx.BlockHeight()), uint64(ctx.BlockTime().Unix()))
	return core.TransactionToMessage(tx, signer, nil)
}

func (server msgServer) applyEVMMessage(ctx sdk.Context, msg *core.Message, stateDB vm.StateDB, gp core.GasPool) (*core.ExecutionResult, error) {
	coinbase, err := server.GetFeeCollectorAddress(ctx)
	if err != nil {
		return nil, err
	}
	cfg := server.GetChainConfig(ctx).EthereumConfig(server.ChainID())
	blockCtx := vm.BlockContext{
		CanTransfer: core.CanTransfer,
		Transfer:    core.Transfer,
		GetHash:     server.getHashFn(ctx),
		Coinbase:    coinbase,
		GasLimit:    gp.Gas(),
		BlockNumber: big.NewInt(ctx.BlockHeight()),
		Time:        uint64(ctx.BlockHeader().Time.Unix()),
		Difficulty:  big.NewInt(0),       // only needed for PoW
		BaseFee:     server.getBaseFee(), // feemarket not enabled
		Random:      nil,                 // not supported
	}
	txCtx := core.NewEVMTxContext(msg)
	evmInstance := vm.NewEVM(blockCtx, txCtx, stateDB, cfg, vm.Config{})
	st := core.NewStateTransition(evmInstance, msg, &gp)
	return st.TransitionDb()
}

func (server msgServer) writeReceipt(ctx sdk.Context, tx *ethtypes.Transaction, msg *core.Message, usedGas uint64, success bool) error {
	var contractAddr common.Address
	if msg.To == nil {
		contractAddr = crypto.CreateAddress(msg.From, msg.Nonce)
	} else if len(msg.Data) > 0 {
		contractAddr = *msg.To
	}

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
		ContractAddress:   contractAddr.Hex(),
		GasUsed:           usedGas,
		BlockNumber:       uint64(ctx.BlockHeight()),
		TransactionIndex:  uint32(ctx.TxIndex()),
		EffectiveGasPrice: tx.GasPrice().Uint64(),
	}

	if success {
		receipt.Status = uint32(ethtypes.ReceiptStatusSuccessful)
	} else {
		receipt.Status = uint32(ethtypes.ReceiptStatusFailed)
	}

	return server.SetReceipt(ctx, tx.Hash(), receipt)
}

// returns a function that provides block header hash based on block number
func (server msgServer) getHashFn(ctx sdk.Context) vm.GetHashFunc {
	return func(height uint64) common.Hash {
		if height > math.MaxInt64 {
			ctx.Logger().Error("Sei block height is bounded by int64 range")
			return common.Hash{}
		}
		h := int64(height)
		if ctx.BlockHeight() == h {
			// current header hash is in the context already
			return common.BytesToHash(ctx.HeaderHash())
		}
		if ctx.BlockHeight() < h {
			// future block doesn't have a hash yet
			return common.Hash{}
		}
		// fetch historical hash from historical info
		return server.getHistoricalHash(ctx, h)
	}
}

func (server msgServer) getHistoricalHash(ctx sdk.Context, h int64) common.Hash {
	histInfo, found := server.stakingKeeper.GetHistoricalInfo(ctx, h)
	if !found {
		// too old, already pruned
		return common.Hash{}
	}
	header, err := tmtypes.HeaderFromProto(&histInfo.Header)
	if err != nil {
		// parsing issue
		ctx.Logger().Error(fmt.Sprintf("failed to parse historical info header %s due to %s", histInfo.Header.String(), err))
		return common.Hash{}
	}

	return common.BytesToHash(header.Hash())
}

// fee market is not enabled for now, so returning 0
func (server msgServer) getBaseFee() *big.Int {
	return big.NewInt(0)
}
