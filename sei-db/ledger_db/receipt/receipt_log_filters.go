package receipt

import (
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/eth/filters"
	"github.com/sei-protocol/sei-chain/x/evm/types"
)

type receiptGetter func(ctx sdk.Context, txHash common.Hash) (*types.Receipt, error)

func filterLogsFromReceipts(ctx sdk.Context, blockHeight int64, blockHash common.Hash, txHashes []common.Hash, crit filters.FilterCriteria, applyExactMatch bool, getReceipt receiptGetter) ([]*ethtypes.Log, error) {
	hasFilters := len(crit.Addresses) != 0 || len(crit.Topics) != 0
	var filterIndexes [][]bloomIndexes
	if hasFilters {
		filterIndexes = encodeFilters(crit.Addresses, crit.Topics)
	}

	logs := make([]*ethtypes.Log, 0)
	totalLogs := uint(0)
	evmTxIndex := 0

	for _, txHash := range txHashes {
		receipt, err := getReceipt(ctx, txHash)
		if err != nil {
			ctx.Logger().Error(fmt.Sprintf("collectLogs: unable to find receipt for hash %s", txHash.Hex()))
			continue
		}

		txLogs := getLogsForTx(receipt, totalLogs)

		if hasFilters {
			if len(receipt.LogsBloom) == 0 || matchFilters(ethtypes.Bloom(receipt.LogsBloom), filterIndexes) {
				if applyExactMatch {
					for _, log := range txLogs {
						log.TxIndex = uint(evmTxIndex)        //nolint:gosec
						log.BlockNumber = uint64(blockHeight) //nolint:gosec
						log.BlockHash = blockHash
						if isLogExactMatch(log, crit) {
							logs = append(logs, log)
						}
					}
				} else {
					for _, log := range txLogs {
						log.TxIndex = uint(evmTxIndex)        //nolint:gosec
						log.BlockNumber = uint64(blockHeight) //nolint:gosec
						log.BlockHash = blockHash
						logs = append(logs, log)
					}
				}
			}
		} else {
			for _, log := range txLogs {
				log.TxIndex = uint(evmTxIndex)        //nolint:gosec
				log.BlockNumber = uint64(blockHeight) //nolint:gosec
				log.BlockHash = blockHash
				logs = append(logs, log)
			}
		}

		totalLogs += uint(len(txLogs))
		evmTxIndex++
	}

	return logs, nil
}
