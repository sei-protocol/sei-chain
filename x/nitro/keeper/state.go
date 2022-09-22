package keeper

import (
	"encoding/binary"
	"errors"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

func (k Keeper) SetStateRoot(ctx sdk.Context, slot uint64, stateRoot []byte) {
	store := k.GetStateRootStore(ctx)
	key := make([]byte, 8)
	binary.BigEndian.PutUint64(key, slot)
	store.Set(key, stateRoot)
}

func (k Keeper) GetStateRoot(ctx sdk.Context, slot uint64) ([]byte, error) {
	store := k.GetStateRootStore(ctx)
	key := make([]byte, 8)
	binary.BigEndian.PutUint64(key, slot)
	res := store.Get(key)
	if res == nil {
		return nil, errors.New("not found")
	}
	return res, nil
}
