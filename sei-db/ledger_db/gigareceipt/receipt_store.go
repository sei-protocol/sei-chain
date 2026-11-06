package gigareceipt

import (
	"errors"

	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	dbconfig "github.com/sei-protocol/sei-chain/sei-db/config"
	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/receipt"
	"github.com/sei-protocol/sei-chain/utils"
	"github.com/sei-protocol/sei-chain/x/evm/types"
	"github.com/sei-protocol/seilog"
)

var logger = seilog.NewLogger("db", "ledger-db", "gigareceipt")

// NewReceiptStore opens the receipt store in config.DBDirectory. storeKey names the legacy KV store
// a receipt lookup falls back to, or is nil when there is none.
func NewReceiptStore(config dbconfig.ReceiptStoreConfig, storeKey sdk.StoreKey) (receipt.ReceiptStore, error) {
	if config.DBDirectory == "" {
		return nil, errors.New("receipt store db directory not configured")
	}
	return newLittReceiptStore(config, storeKey)
}

func convertLog(l *types.Log, stored *types.Receipt, logStartIndex uint) *ethtypes.Log {
	return &ethtypes.Log{
		Address:     common.HexToAddress(l.Address),
		Topics:      utils.Map(l.Topics, common.HexToHash),
		Data:        l.Data,
		BlockNumber: stored.BlockNumber,
		TxHash:      common.HexToHash(stored.TxHashHex),
		TxIndex:     uint(stored.TransactionIndex),
		Index:       uint(l.Index) + logStartIndex,
	}
}
