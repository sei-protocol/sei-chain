package keeper

import (
	"bytes"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/bitutil"
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

func (k *Keeper) SetBlockBloom(ctx sdk.Context, height int64, blooms []ethtypes.Bloom) {
	blockBloom := make([]byte, ethtypes.BloomByteLength)
	for _, bloom := range blooms {
		or := make([]byte, ethtypes.BloomByteLength)
		bitutil.ORBytes(or, blockBloom, bloom[:])
		blockBloom = or
	}
	if bytes.Equal(blockBloom, make([]byte, ethtypes.BloomByteLength)) {
		// early return if bloom is empty
		return
	}
	store := ctx.KVStore(k.storeKey)
	store.Set(types.BlockBloomKey(height), blockBloom)
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
	}
}

func ConvertEthLog(l *ethtypes.Log) *types.Log {
	return &types.Log{
		Address: l.Address.Hex(),
		Topics:  utils.Map(l.Topics, func(h common.Hash) string { return h.Hex() }),
		Data:    l.Data,
	}
}
