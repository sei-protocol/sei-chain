package keeper

import (
	"errors"
	"fmt"
	"time"

	"github.com/ethereum/go-ethereum/common"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"

	"github.com/ethereum/go-ethereum/core"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/sei-protocol/sei-chain/giga/deps/xevm/state"
	"github.com/sei-protocol/sei-chain/giga/deps/xevm/types"
	receipts "github.com/sei-protocol/sei-chain/sei-db/ledger_db/receipt"
	"github.com/sei-protocol/sei-chain/utils"
	evmtypes "github.com/sei-protocol/sei-chain/x/evm/types"
)

// Number of blocks between legacy receipt migration batches
const LegacyReceiptMigrationInterval int64 = 10

// Number of receipts to migrate per batch
const LegacyReceiptMigrationBatchSize int = 100

// SetTransientReceipt sets a data structure that stores EVM specific transaction metadata.
func (k *Keeper) SetTransientReceipt(ctx sdk.Context, txHash common.Hash, receipt *types.Receipt) error {
	store := ctx.TransientStore(k.transientStoreKey)
	bz, err := receipt.Marshal()
	if err != nil {
		return err
	}
	store.Set(types.NewTransientReceiptKey(uint64(receipt.TransactionIndex), txHash), bz)
	return nil
}

func (k *Keeper) GetTransientReceipt(ctx sdk.Context, txHash common.Hash, txIndex uint64) (*types.Receipt, error) {
	store := ctx.TransientStore(k.transientStoreKey)
	bz := store.Get(types.NewTransientReceiptKey(txIndex, txHash))
	if bz == nil {
		return nil, receipts.ErrNotFound
	}
	r := &types.Receipt{}
	if err := r.Unmarshal(bz); err != nil {
		return nil, err
	}
	return r, nil
}

func (k *Keeper) DeleteTransientReceipt(ctx sdk.Context, txHash common.Hash, txIndex uint64) {
	store := ctx.TransientStore(k.transientStoreKey)
	store.Delete(types.NewTransientReceiptKey(txIndex, txHash))
}

// GetReceipt returns a data structure that stores EVM specific transaction metadata.
// Many EVM applications (e.g. MetaMask) relies on being on able to query receipt
// by EVM transaction hash (not Sei transaction hash) to function properly.
func (k *Keeper) GetReceipt(ctx sdk.Context, txHash common.Hash) (*types.Receipt, error) {
	if k.receiptStore == nil {
		return nil, receipts.ErrNotConfigured
	}
	r, err := k.receiptStore.GetReceipt(ctx, txHash)
	if err == nil {
		return convertReceipt(r), nil
	}
	// Only fall back to legacy store for a not found error.
	if !errors.Is(err, receipts.ErrNotFound) {
		return nil, err
	}

	// try legacy store for older receipts
	store := k.GetKVStore(ctx)
	bz := store.Get(types.ReceiptKey(txHash))
	if bz == nil {
		return nil, receipts.ErrNotFound
	}

	var legacy types.Receipt
	if err := legacy.Unmarshal(bz); err != nil {
		return nil, err
	}
	return &legacy, nil
}

// Only used for testing
func (k *Keeper) GetReceiptFromReceiptStore(ctx sdk.Context, txHash common.Hash) (*types.Receipt, error) {
	if k.receiptStore == nil {
		return nil, receipts.ErrNotConfigured
	}
	r, err := k.receiptStore.GetReceiptFromStore(ctx, txHash)
	if err != nil {
		return nil, err
	}
	return convertReceipt(r), nil
}

// GetReceiptWithRetry attempts to get a receipt with retries to handle race conditions
// where the receipt might not be immediately available after the transaction.
func (k *Keeper) GetReceiptWithRetry(ctx sdk.Context, txHash common.Hash, maxRetries int) (*types.Receipt, error) {
	var lastErr error
	for i := 0; i < maxRetries; i++ {
		receipt, err := k.GetReceipt(ctx, txHash)
		if err == nil {
			return receipt, nil
		}

		// If it's not a "not found" error, return immediately
		if !errors.Is(err, receipts.ErrNotFound) {
			return nil, err
		}

		// Wait before retrying, with increasing delay, 200ms, 400ms, 600ms, etc.
		time.Sleep(time.Millisecond * 200 * time.Duration(i+1))
		lastErr = err
	}
	return nil, lastErr
}

//	MockReceipt sets a data structure that stores EVM specific transaction metadata.
//
// this is currently used by a number of tests to set receipts at the moment
func (k *Keeper) MockReceipt(ctx sdk.Context, txHash common.Hash, receipt *types.Receipt) error {
	fmt.Printf("MOCK RECEIPT height=%d, tx=%s\n", ctx.BlockHeight(), txHash.Hex())
	if err := k.SetTransientReceipt(ctx, txHash, receipt); err != nil {
		return err
	}
	return nil
}

func convertReceipt(r *evmtypes.Receipt) *types.Receipt {
	if r == nil {
		return nil
	}
	bz, err := r.Marshal()
	if err != nil {
		return nil
	}
	var out types.Receipt
	if err := out.Unmarshal(bz); err != nil {
		return nil
	}
	return &out
}

func (k *Keeper) WriteReceipt(
	ctx sdk.Context,
	stateDB *state.DBImpl,
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
