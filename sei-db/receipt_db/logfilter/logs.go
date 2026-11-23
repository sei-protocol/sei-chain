package logfilter

import (
	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/sei-protocol/sei-chain/utils"
	evmtypes "github.com/sei-protocol/sei-chain/x/evm/types"
)

// LogsForTx converts a stored receipt's logs to go-ethereum logs, offsetting
// each Index by logStartIndex. BlockHash is left zero.
func LogsForTx(receipt *evmtypes.Receipt, logStartIndex uint) []*ethtypes.Log {
	return utils.Map(receipt.Logs, func(l *evmtypes.Log) *ethtypes.Log { return ConvertLog(l, receipt, logStartIndex) })
}

// ConvertLog converts one of receipt's logs to a go-ethereum log, offsetting its Index by
// logStartIndex. BlockHash is left zero.
func ConvertLog(l *evmtypes.Log, receipt *evmtypes.Receipt, logStartIndex uint) *ethtypes.Log {
	return &ethtypes.Log{
		Address:     common.HexToAddress(l.Address),
		Topics:      utils.Map(l.Topics, common.HexToHash),
		Data:        l.Data,
		BlockNumber: receipt.BlockNumber,
		TxHash:      common.HexToHash(receipt.TxHashHex),
		TxIndex:     uint(receipt.TransactionIndex),
		Index:       uint(l.Index) + logStartIndex,
	}
}
