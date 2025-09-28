package keeper

import (
	"errors"
	"fmt"
	"time"

	"github.com/cosmos/cosmos-sdk/store/prefix"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/iavl"
	"github.com/ethereum/go-ethereum/common"
	"github.com/sei-protocol/sei-db/proto"

	"github.com/ethereum/go-ethereum/core"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/sei-protocol/sei-chain/utils"
	"github.com/sei-protocol/sei-chain/x/evm/state"
	"github.com/sei-protocol/sei-chain/x/evm/types"
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
		return nil, errors.New("not found")
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
	// receipts are immutable, use latest version
	lv, err := k.receiptStore.GetLatestVersion()
	if err != nil {
		return nil, err
	}

	// try persistent store
	bz, err := k.receiptStore.Get(types.ReceiptStoreKey, lv, types.ReceiptKey(txHash))
	if err != nil {
		return nil, err
	}

	if bz == nil {
		// try legacy store for older receipts
		store := ctx.KVStore(k.storeKey)
		bz = store.Get(types.ReceiptKey(txHash))
		if bz == nil {
			return nil, errors.New("not found")
		}
	}

	var r types.Receipt
	if err := r.Unmarshal(bz); err != nil {
		return nil, err
	}
	return &r, nil
}

// Only used for testing
func (k *Keeper) GetReceiptFromReceiptStore(ctx sdk.Context, txHash common.Hash) (*types.Receipt, error) {
	// receipts are immutable, use latest version
	lv, err := k.receiptStore.GetLatestVersion()
	if err != nil {
		return nil, err
	}

	// try persistent store
	bz, err := k.receiptStore.Get(types.ReceiptStoreKey, lv, types.ReceiptKey(txHash))
	if err != nil {
		return nil, err
	}
	if bz == nil {
		return nil, errors.New("not found")
	}

	var r types.Receipt
	if err := r.Unmarshal(bz); err != nil {
		return nil, err
	}
	return &r, nil
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
		if err.Error() != "not found" {
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
	return k.FlushTransientReceiptsSync(ctx)
}

func (k *Keeper) FlushTransientReceiptsSync(ctx sdk.Context) error {
	return k.flushTransientReceipts(ctx, true)
}

func (k *Keeper) FlushTransientReceiptsAsync(ctx sdk.Context) error {
	return k.flushTransientReceipts(ctx, false)
}

func isLegacyReceipt(ctx sdk.Context, receipt *types.Receipt) bool {
	if ctx.ChainID() == "pacific-1" {
		return receipt.BlockNumber < 162745893
	}
	if ctx.ChainID() == "atlantic-2" {
		return receipt.BlockNumber < 191939681
	}
	if ctx.ChainID() == "arctic-1" {
		return receipt.BlockNumber < 109393643
	}
	return false
}

func (k *Keeper) flushTransientReceipts(ctx sdk.Context, sync bool) error {
	transientReceiptStore := prefix.NewStore(ctx.TransientStore(k.transientStoreKey), types.ReceiptKeyPrefix)
	iter := transientReceiptStore.Iterator(nil, nil)
	defer func() { _ = iter.Close() }()
	var pairs []*iavl.KVPair

	// TransientReceiptStore is recreated on commit meaning it will only contain receipts for a single block at a time
	// and will never flush a subset of block's receipts.
	// However in our test suite it can happen that the transient store can contain receipts from different blocks
	// and we need to account for that.
	cumulativeGasUsedPerBlock := make(map[uint64]uint64)
	for ; iter.Valid(); iter.Next() {
		receipt := &types.Receipt{}
		if err := receipt.Unmarshal(iter.Value()); err != nil {
			return err
		}

		if !isLegacyReceipt(ctx, receipt) {
			cumulativeGasUsedPerBlock[receipt.BlockNumber] += receipt.GasUsed
			receipt.CumulativeGasUsed = cumulativeGasUsedPerBlock[receipt.BlockNumber]
		}

		marshalledReceipt, err := receipt.Marshal()
		if err != nil {
			return err
		}

		kvPair := &iavl.KVPair{Key: types.ReceiptKey(types.TransientReceiptKey(iter.Key()).TransactionHash()), Value: marshalledReceipt}
		pairs = append(pairs, kvPair)
	}
	if len(pairs) == 0 {
		return nil
	}
	ncs := &proto.NamedChangeSet{
		Name:      types.ReceiptStoreKey,
		Changeset: iavl.ChangeSet{Pairs: pairs},
	}

	if sync {
		return k.receiptStore.ApplyChangeset(ctx.BlockHeight(), ncs)
	} else {
		var changesets []*proto.NamedChangeSet
		changesets = append(changesets, ncs)
		return k.receiptStore.ApplyChangesetAsync(ctx.BlockHeight(), changesets)
	}
}

// MigrateLegacyReceiptsBatch moves up to batchSize receipts from the legacy KV store
// into the persistent receipt store and deletes them from the legacy store.
// It returns the number of receipts migrated.
func (k *Keeper) MigrateLegacyReceiptsBatch(ctx sdk.Context, batchSize int) (int, error) {
	// Iterate over legacy receipt keys under prefix types.ReceiptKeyPrefix
	legacyStore := prefix.NewStore(ctx.KVStore(k.storeKey), types.ReceiptKeyPrefix)
	iter := legacyStore.Iterator(nil, nil)
	defer func() { _ = iter.Close() }()

	// Early exit if nothing to migrate
	if !iter.Valid() {
		return 0, nil
	}

	if batchSize <= 0 {
		return 0, nil
	}

	var (
		txHashes     []common.Hash
		receipts     []*types.Receipt
		keysToDelete [][]byte
		migrated     int
	)

	txHashes = make([]common.Hash, 0, batchSize)
	receipts = make([]*types.Receipt, 0, batchSize)
	keysToDelete = make([][]byte, 0, batchSize)

	for ; migrated < batchSize && iter.Valid(); iter.Next() {
		keySuffix := iter.Key() // tx hash bytes (without prefix)
		value := iter.Value()   // serialized receipt bytes

		receipt := &types.Receipt{}
		if err := receipt.Unmarshal(value); err != nil {
			return 0, err
		}

		// Derive tx hash directly from key suffix
		txHash := common.BytesToHash(keySuffix)

		receipts = append(receipts, receipt)
		txHashes = append(txHashes, txHash)
		// Save the suffix for deletion from legacy store after successful write
		keysToDelete = append(keysToDelete, append([]byte{}, keySuffix...))
		migrated++
	}

	if migrated == 0 {
		return 0, nil
	}

	// Write to transient receipt store first; they'll be flushed to receipt.db at pre-commit
	for i := range receipts {
		if err := k.SetTransientReceipt(ctx, txHashes[i], receipts[i]); err != nil {
			return 0, err
		}
	}

	// After a successful write, delete from legacy store
	for _, kdel := range keysToDelete {
		legacyStore.Delete(kdel)
	}

	return migrated, nil
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
	bloom := ethtypes.CreateBloom(ethtypes.Receipts{&ethtypes.Receipt{Logs: ethLogs}})
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
