package keeper

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/sei-protocol/sei-chain/utils"
	"github.com/sei-protocol/sei-chain/x/evm/types"
)

func (k *Keeper) GetBlockBloom(ctx sdk.Context, height int64) (res ethtypes.Bloom) {
	store := ctx.KVStore(k.storeKey)
	bz := store.Get(types.BlockBloomKey(height))
	if bz != nil {
		res.SetBytes(bz)
	}
	return
}

func (k *Keeper) GetLogsForTx(ctx sdk.Context, txHash common.Hash) []*ethtypes.Log {
	receipt, err := k.GetReceipt(ctx, txHash)
	if err != nil {
		return []*ethtypes.Log{}
	}
	return utils.Map(receipt.Logs, convertLog)
}

func convertLog(l *types.Log) *ethtypes.Log {
	return &ethtypes.Log{
		Address: common.HexToAddress(l.Address),
		Topics:  utils.Map(l.Topics, common.HexToHash),
		Data:    l.Data,
	}
}
