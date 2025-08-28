package keeper

import (
	"github.com/cosmos/cosmos-sdk/store/prefix"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/sei-protocol/sei-chain/x/evm/types"
)

func (k *Keeper) AddAnteSurplus(ctx sdk.Context, txHash common.Hash, surplus sdk.Int) error {
	store := prefix.NewStore(ctx.TransientStore(k.transientStoreKey), types.AnteSurplusPrefix)
	bz, err := surplus.Marshal()
	if err != nil {
		return err
	}
	store.Set(txHash[:], bz)
	return nil
}

func (k *Keeper) GetAnteSurplusSum(ctx sdk.Context) sdk.Int {
	iter := prefix.NewStore(ctx.TransientStore(k.transientStoreKey), types.AnteSurplusPrefix).Iterator(nil, nil)
	defer iter.Close()
	res := sdk.ZeroInt()
	for ; iter.Valid(); iter.Next() {
		surplus := sdk.Int{}
		_ = surplus.Unmarshal(iter.Value())
		res = res.Add(surplus)
	}
	return res
}
