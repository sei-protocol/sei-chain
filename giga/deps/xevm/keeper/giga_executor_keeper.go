package keeper

import (
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/sei-protocol/sei-chain/giga/deps/xevm/state"
	"github.com/sei-protocol/sei-chain/giga/deps/xevm/types"
	"github.com/sei-protocol/sei-chain/utils"
)

// StateDBWithFinalize is the interface that state.DBImpl implements.
// This allows the GigaExecutorKeeper interface to work with both
// giga/deps/xevm/state.DBImpl and x/evm/state.DBImpl.
type StateDBWithFinalize interface {
	vm.StateDB

	// Finalize commits the state changes and returns any surplus.
	Finalize() (sdk.Int, error)

	// Cleanup releases resources held by the state DB.
	Cleanup()

	// GetAllLogs returns all logs emitted during execution.
	GetAllLogs() []*ethtypes.Log

	// GetPrecompileError returns any error from precompile execution.
	GetPrecompileError() error
}

// NewStateDB creates a new state DB for transaction execution.
// This method allows GigaEvmKeeper to satisfy the GigaExecutorKeeper interface.
func (k *Keeper) NewStateDB(ctx sdk.Context, simulation bool) StateDBWithFinalize {
	return state.NewDBImpl(ctx, k, simulation)
}

// WriteReceiptFromInterface writes a transaction receipt using the StateDBWithFinalize interface.
// This method allows GigaEvmKeeper to satisfy the GigaExecutorKeeper interface.
func (k *Keeper) WriteReceiptFromInterface(
	ctx sdk.Context,
	stateDB StateDBWithFinalize,
	msg *core.Message,
	txType uint32,
	txHash common.Hash,
	gasUsed uint64,
	vmError string,
) (*types.Receipt, error) {
	ethLogs := stateDB.GetAllLogs()
	bloom := ethtypes.CreateBloom(&ethtypes.Receipt{Logs: ethLogs})
	receipt := &types.Receipt{
		TxType:            txType,
		CumulativeGasUsed: uint64(0),
		TxHashHex:         txHash.Hex(),
		GasUsed:           gasUsed,
		BlockNumber:       uint64(ctx.BlockHeight()), // nolint:gosec
		TransactionIndex:  uint32(ctx.TxIndex()),     //nolint:gosec
		EffectiveGasPrice: msg.GasPrice.Uint64(),
		VmError:           vmError,
		Logs:              utils.Map(ethLogs, ConvertEthLog),
		LogsBloom:         bloom[:],
	}

	if msg.To == nil {
		receipt.ContractAddress = crypto.CreateAddress(msg.From, msg.Nonce).Hex()
	} else {
		receipt.To = msg.To.Hex()
		if len(msg.Data) > 0 {
			receipt.ContractAddress = msg.To.Hex()
		}
	}

	if vmError == "" {
		receipt.Status = uint32(ethtypes.ReceiptStatusSuccessful)
	} else {
		receipt.Status = uint32(ethtypes.ReceiptStatusFailed)
	}

	if perr := stateDB.GetPrecompileError(); perr != nil {
		if receipt.Status > 0 {
			ctx.Logger().Error(fmt.Sprintf("Transaction %s succeeded in execution but has precompile error %s", receipt.TxHashHex, perr.Error()))
		} else {
			// append precompile error to VM error
			receipt.VmError = fmt.Sprintf("%s|%s", receipt.VmError, perr.Error())
		}
	}

	receipt.From = msg.From.Hex()

	return receipt, k.SetTransientReceipt(ctx, txHash, receipt)
}
