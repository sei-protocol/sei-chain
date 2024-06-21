package keeper

import (
	"errors"
	"github.com/cosmos/cosmos-sdk/store/prefix"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/sei-protocol/sei-chain/x/evm/types"
)

// SetTransientReceipt sets a data structure that stores EVM specific transaction metadata.
func (k *Keeper) SetTransientReceipt(ctx sdk.Context, txHash common.Hash, receipt *types.Receipt) error {
	store := ctx.TransientStore(k.transientStoreKey)
	bz, err := receipt.Marshal()
	if err != nil {
		return err
	}
	store.Set(types.ReceiptKey(txHash), bz)
	return nil
}

// GetReceipt returns a data structure that stores EVM specific transaction metadata.
// Many EVM applications (e.g. MetaMask) relies on being on able to query receipt
// by EVM transaction hash (not Sei transaction hash) to function properly.
func (k *Keeper) GetReceipt(ctx sdk.Context, txHash common.Hash) (*types.Receipt, error) {
	bz, err := ctx.EvmReceiptStateStore().Get(types.ReceiptStoreKey, ctx.BlockHeight(), types.ReceiptKey(txHash))
	if err != nil {
		return nil, err
	}

	if bz == nil {
		// fallback because these used to exist in application state
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

//	SetReceipt sets a data structure that stores EVM specific transaction metadata.
//
// Deprecated: in favor of SetTransientReceipt
// this is currently used by a number of tests to set receipts at the moment
// TODO: remove this once we move off of SetReceipt (tests are using it)
func (k *Keeper) SetReceipt(ctx sdk.Context, txHash common.Hash, receipt *types.Receipt) error {
	store := ctx.KVStore(k.storeKey)
	bz, err := receipt.Marshal()
	if err != nil {
		return err
	}
	store.Set(types.ReceiptKey(txHash), bz)
	return nil
}

func (k *Keeper) FlushTransientReceipts(ctx sdk.Context) error {
	//TODO implement flush receipts
	//receiptStore := ctx.EvmReceiptStateStore()
	iter := prefix.NewStore(ctx.TransientStore(k.transientStoreKey), types.ReceiptKeyPrefix).Iterator(nil, nil)
	defer iter.Close()
	for ; iter.Valid(); iter.Next() {
		//txHash := common.BytesToHash(iter.Key())
		//bz := iter.Value()
		// TODO: write this tx to store to receiptStore
		// Make sure the key aligns with GetReceipt ^
	}
	return ctx.TransientStore(k.transientStoreKey).DeleteAll(nil, nil)
}
