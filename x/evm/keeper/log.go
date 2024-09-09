package keeper

import (
	"encoding/binary"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/bitutil"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/sei-protocol/sei-chain/utils"
	"github.com/sei-protocol/sei-chain/x/evm/types"
)

func (k *Keeper) GetBlockBloom(ctx sdk.Context) (res ethtypes.Bloom) {
	store := ctx.KVStore(k.storeKey)
	bz := store.Get(types.BlockBloomPrefix)
	if bz != nil {
		res.SetBytes(bz)
		return
	}
	cutoff := k.GetLegacyBlockBloomCutoffHeight(ctx)
	if cutoff == 0 || ctx.BlockHeight() < cutoff {
		res = k.GetLegacyBlockBloom(ctx, ctx.BlockHeight())
	}
	return
}

func (k *Keeper) GetLegacyBlockBloom(ctx sdk.Context, height int64) (res ethtypes.Bloom) {
	store := ctx.KVStore(k.storeKey)
	bz := store.Get(types.BlockBloomKey(height))
	if bz != nil {
		res.SetBytes(bz)
	}
	return
}

func (k *Keeper) SetBlockBloom(ctx sdk.Context, blooms []ethtypes.Bloom) {
	blockBloom := make([]byte, ethtypes.BloomByteLength)
	for _, bloom := range blooms {
		or := make([]byte, ethtypes.BloomByteLength)
		bitutil.ORBytes(or, blockBloom, bloom[:])
		blockBloom = or
	}
	store := ctx.KVStore(k.storeKey)
	store.Set(types.BlockBloomPrefix, blockBloom)
}

func (k *Keeper) SetLegacyBlockBloomCutoffHeight(ctx sdk.Context) {
	store := ctx.KVStore(k.storeKey)
	bz := make([]byte, 8)
	binary.BigEndian.PutUint64(bz, uint64(ctx.BlockHeight()))
	store.Set(types.LegacyBlockBloomCutoffHeightKey, bz)
}

func (k *Keeper) GetLegacyBlockBloomCutoffHeight(ctx sdk.Context) int64 {
	store := ctx.KVStore(k.storeKey)
	bz := store.Get(types.LegacyBlockBloomCutoffHeightKey)
	if len(bz) == 0 {
		return 0
	}
	return int64(binary.BigEndian.Uint64(bz))
}

func GetLogsForTx(receipt *types.Receipt) []*ethtypes.Log {
	return utils.Map(receipt.Logs, func(l *types.Log) *ethtypes.Log { return convertLog(l, receipt) })
}

func convertLog(l *types.Log, receipt *types.Receipt) *ethtypes.Log {
	return &ethtypes.Log{
		Address:     common.HexToAddress(l.Address),
		Topics:      utils.Map(l.Topics, common.HexToHash),
		Data:        l.Data,
		BlockNumber: receipt.BlockNumber,
		TxHash:      common.HexToHash(receipt.TxHashHex),
		TxIndex:     uint(receipt.TransactionIndex),
		Index:       uint(l.Index)}
}

func ConvertEthLog(l *ethtypes.Log) *types.Log {
	return &types.Log{
		Address: l.Address.Hex(),
		Topics:  utils.Map(l.Topics, func(h common.Hash) string { return h.Hex() }),
		Data:    l.Data,
		Index:   uint32(l.Index),
	}
}

func ConvertSyntheticEthLog(l *ethtypes.Log) *types.Log {
	log := ConvertEthLog(l)
	log.Synthetic = true
	return log
}
