package keeper

import (
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/sei-protocol/sei-chain/x/evm/types"
)

func (k *Keeper) IsCodeHashWhitelistedForBankSend(ctx sdk.Context, h common.Hash) bool {
	if w := k.GetCodeHashWhitelistedForBankSend(ctx); w != nil {
		return w.IsHashInWhiteList(h)
	}
	return false
}

func (k *Keeper) AddCodeHashWhitelistedForBankSend(ctx sdk.Context, h common.Hash) {
	store := ctx.KVStore(k.storeKey)
	w := k.GetCodeHashWhitelistedForBankSend(ctx)
	if w == nil {
		w = &types.Whitelist{Hashes: []string{h.Hex()}}
	} else if !w.IsHashInWhiteList(h) {
		w.Hashes = append(w.Hashes, h.Hex())
	}
	bz, _ := w.Marshal()
	store.Set(types.WhitelistedCodeHashesForBankSendPrefix, bz)
}

func (k *Keeper) GetCodeHashWhitelistedForBankSend(ctx sdk.Context) *types.Whitelist {
	store := ctx.KVStore(k.storeKey)
	bz := store.Get(types.WhitelistedCodeHashesForBankSendPrefix)
	if bz == nil {
		return nil
	}
	w := &types.Whitelist{}
	if err := w.Unmarshal(bz); err != nil {
		ctx.Logger().Error(fmt.Sprintf("error parsing code hash whitelist for bank send: %s", err))
		return nil
	}
	return w
}
