package dex

import (
	"encoding/binary"

	"github.com/cosmos/cosmos-sdk/store/prefix"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/x/dex/types"
)

type BlockCancellations struct {
	cancelStore *prefix.Store
}

func NewCancels(cancelStore prefix.Store) *BlockCancellations {
	return &BlockCancellations{cancelStore: &cancelStore}
}

func (o *BlockCancellations) Has(cancel *types.Cancellation) bool {
	keybz := make([]byte, 8)
	binary.BigEndian.PutUint64(keybz, cancel.Id)
	return o.cancelStore.Has(keybz)
}

func (o *BlockCancellations) Get() (list []*types.Cancellation) {
	iterator := sdk.KVStorePrefixIterator(o.cancelStore, []byte{})

	defer iterator.Close()

	for ; iterator.Valid(); iterator.Next() {
		var val types.Cancellation
		if err := val.Unmarshal(iterator.Value()); err != nil {
			panic(err)
		}
		list = append(list, &val)
	}

	return
}

func (o *BlockCancellations) GetIdsToCancel() (list []uint64) {
	iterator := sdk.KVStorePrefixIterator(o.cancelStore, []byte{})

	defer iterator.Close()

	for ; iterator.Valid(); iterator.Next() {
		var val types.Cancellation
		if err := val.Unmarshal(iterator.Value()); err != nil {
			panic(err)
		}
		list = append(list, val.Id)
	}

	return
}

func (o *BlockCancellations) Add(newItem *types.Cancellation) {
	keybz := make([]byte, 8)
	binary.BigEndian.PutUint64(keybz, newItem.Id)
	valbz, err := newItem.Marshal()
	if err != nil {
		panic(err)
	}
	o.cancelStore.Set(keybz, valbz)
}
