package keeper

import (
	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

type Keeper struct {
	cdc      codec.BinaryCodec
	storeKey sdk.StoreKey
}

func NewKeeper(
	cdc codec.BinaryCodec,
	storeKey sdk.StoreKey,
) Keeper {
	return Keeper{
		cdc:      cdc,
		storeKey: storeKey,
	}
}
