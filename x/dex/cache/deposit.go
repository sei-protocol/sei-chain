package dex

import (
	"github.com/cosmos/cosmos-sdk/store/prefix"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/x/dex/types"
)

type DepositInfo struct {
	store *prefix.Store
}

func NewDepositInfo(store prefix.Store) *DepositInfo {
	return &DepositInfo{store: &store}
}

func (d *DepositInfo) Get() (list []*types.DepositInfoEntry) {
	iterator := sdk.KVStorePrefixIterator(d.store, []byte{})

	defer iterator.Close()

	for ; iterator.Valid(); iterator.Next() {
		var val types.DepositInfoEntry
		if err := val.Unmarshal(iterator.Value()); err != nil {
			panic(err)
		}
		list = append(list, &val)
	}

	return
}

func (d *DepositInfo) Add(newItem *types.DepositInfoEntry) {
	key := types.MemDepositSubprefix(newItem.Creator, newItem.Denom)
	if val, err := newItem.Marshal(); err != nil {
		panic(err)
	} else if existing := d.store.Get(key); existing == nil {
		d.store.Set(key, val)
	} else {
		existingItem := types.DepositInfoEntry{}
		if err := existingItem.Unmarshal(existing); err != nil {
			panic(err)
		}
		newItem.Amount = newItem.Amount.Add(existingItem.Amount)
		newVal, err := newItem.Marshal()
		if err != nil {
			panic(err)
		}
		d.store.Set(key, newVal)
	}
}
