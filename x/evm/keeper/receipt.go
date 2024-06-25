package keeper

import (
	"errors"
	"github.com/cosmos/cosmos-sdk/store/prefix"
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
	bz, err := k.receiptStore.Get(types.ReceiptStoreKey, ctx.BlockHeight(), types.ReceiptKey(txHash))
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
	iter := prefix.NewStore(ctx.TransientStore(k.transientStoreKey), types.ReceiptKeyPrefix).Iterator(nil, nil)
	defer iter.Close()
	var pairs []*iavl.KVPair
	var changesets []*proto.NamedChangeSet
	for ; iter.Valid(); iter.Next() {
		kvPair := &iavl.KVPair{Key: iter.Key(), Value: iter.Value()}
		pairs = append(pairs, kvPair)
	}
	ncs := &proto.NamedChangeSet{
		Name:      types.ReceiptStoreKey,
		Changeset: iavl.ChangeSet{Pairs: pairs},
	}
	changesets = append(changesets, ncs)
	err := k.receiptStore.ApplyChangesetAsync(ctx.BlockHeight(), changesets)
	if err != nil {
		return err
	}

	//TODO: we may not actually need this if transient stores are auto-cleared, we'll need to verify
	return ctx.TransientStore(k.transientStoreKey).DeleteAll(nil, nil)
}
