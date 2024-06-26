package keeper

import (
	"errors"
	"fmt"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/iavl"
	"github.com/ethereum/go-ethereum/common"
	"github.com/sei-protocol/sei-chain/x/evm/types"
	"github.com/sei-protocol/sei-db/proto"
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

//	MockReceipt sets a data structure that stores EVM specific transaction metadata.
//
// this is currently used by a number of tests to set receipts at the moment
func (k *Keeper) MockReceipt(ctx sdk.Context, txHash common.Hash, receipt *types.Receipt) error {
	fmt.Printf("MOCK RECEIPT height=%d, tx=%s\n", ctx.BlockHeight(), txHash.Hex())
	if err := k.SetTransientReceipt(ctx, txHash, receipt); err != nil {
		return err
	}
	return k.FlushTransientReceipts(ctx)
}

func (k *Keeper) FlushTransientReceipts(ctx sdk.Context) error {
	iter := ctx.TransientStore(k.transientStoreKey).Iterator(nil, nil)
	defer iter.Close()
	var pairs []*iavl.KVPair
	var changesets []*proto.NamedChangeSet
	for ; iter.Valid(); iter.Next() {
		kvPair := &iavl.KVPair{Key: iter.Key(), Value: iter.Value()}
		pairs = append(pairs, kvPair)
	}
	if len(pairs) == 0 {
		return nil
	}
	ncs := &proto.NamedChangeSet{
		Name:      types.ReceiptStoreKey,
		Changeset: iavl.ChangeSet{Pairs: pairs},
	}
	changesets = append(changesets, ncs)

	return k.receiptStore.ApplyChangesetAsync(ctx.BlockHeight(), changesets)
}
