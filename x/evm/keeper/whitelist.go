package keeper

import (
	"bytes"
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/sei-protocol/sei-chain/x/evm/artifacts/cw20"
	"github.com/sei-protocol/sei-chain/x/evm/artifacts/cw721"
	"github.com/sei-protocol/sei-chain/x/evm/artifacts/native"
	"github.com/sei-protocol/sei-chain/x/evm/types"
)

func (k *Keeper) AddToWhitelistIfApplicable(ctx sdk.Context, data []byte, contractAddress common.Address) {
	if native.IsCodeFromBin(data) {
		codeHash := k.GetCodeHash(ctx, contractAddress)
		if (codeHash != common.Hash{}) {
			k.AddCodeHashWhitelistedForBankSend(ctx, codeHash)
		}
		return
	}
	if cw20.IsCodeFromBin(data) || cw721.IsCodeFromBin(data) {
		codeHash := k.GetCodeHash(ctx, contractAddress)
		if (codeHash != common.Hash{}) {
			k.AddCodeHashWhitelistedForDelegateCall(ctx, codeHash)
		}
		return
	}
}

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
		return &types.Whitelist{Hashes: []string{}}
	}
	w := &types.Whitelist{}
	if err := w.Unmarshal(bz); err != nil {
		ctx.Logger().Error(fmt.Sprintf("error parsing code hash whitelist for bank send: %s", err))
		return nil
	}
	return w
}

func (k *Keeper) IsCWCodeHashWhitelistedForEVMDelegateCall(ctx sdk.Context, h []byte) bool {
	for _, w := range k.WhitelistedCwCodeHashesForDelegateCall(ctx) {
		if bytes.Equal(w, h) {
			return true
		}
	}
	return false
}

func (k *Keeper) IsCodeHashWhitelistedForDelegateCall(ctx sdk.Context, h common.Hash) bool {
	if w := k.GetCodeHashWhitelistedForDelegateCall(ctx); w != nil {
		return w.IsHashInWhiteList(h)
	}
	return false
}

func (k *Keeper) AddCodeHashWhitelistedForDelegateCall(ctx sdk.Context, h common.Hash) {
	store := ctx.KVStore(k.storeKey)
	w := k.GetCodeHashWhitelistedForDelegateCall(ctx)
	if w == nil {
		w = &types.Whitelist{Hashes: []string{h.Hex()}}
	} else if !w.IsHashInWhiteList(h) {
		w.Hashes = append(w.Hashes, h.Hex())
	}
	bz, _ := w.Marshal()
	store.Set(types.WhitelistedCodeHashesForDelegateCallPrefix, bz)
}

func (k *Keeper) GetCodeHashWhitelistedForDelegateCall(ctx sdk.Context) *types.Whitelist {
	store := ctx.KVStore(k.storeKey)
	bz := store.Get(types.WhitelistedCodeHashesForDelegateCallPrefix)
	if bz == nil {
		return &types.Whitelist{Hashes: []string{}}
	}
	w := &types.Whitelist{}
	if err := w.Unmarshal(bz); err != nil {
		ctx.Logger().Error(fmt.Sprintf("error parsing code hash whitelist for delegate call: %s", err))
		return nil
	}
	return w
}
