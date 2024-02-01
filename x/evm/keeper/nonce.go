package keeper

import (
	"encoding/binary"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/sei-protocol/sei-chain/x/evm/types"
)

func (k *Keeper) GetNonce(ctx sdk.Context, addr common.Address) uint64 {
	bz := k.PrefixStore(ctx, types.NonceKeyPrefix).Get(addr[:])
	if bz == nil {
		return 0
	}
	return binary.BigEndian.Uint64(bz)
}

func (k *Keeper) SetNonce(ctx sdk.Context, addr common.Address, nonce uint64) {
	length := make([]byte, 8)
	binary.BigEndian.PutUint64(length, nonce)
	k.PrefixStore(ctx, types.NonceKeyPrefix).Set(addr[:], length)
}
