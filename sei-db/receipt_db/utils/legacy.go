package utils

import (
	"github.com/ethereum/go-ethereum/common"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	receiptdb "github.com/sei-protocol/sei-chain/sei-db/receipt_db"
	evmtypes "github.com/sei-protocol/sei-chain/x/evm/types"
)

// LegacyReceiptFromKVStore looks up a receipt in the legacy KV store for
// receipts that predate the receipt store. Returns receiptdb.ErrNotFound when unavailable.
func LegacyReceiptFromKVStore(ctx sdk.Context, storeKey sdk.StoreKey, txHash common.Hash) (*evmtypes.Receipt, error) {
	if storeKey == nil {
		return nil, receiptdb.ErrNotFound
	}
	bz := ctx.KVStore(storeKey).Get(evmtypes.ReceiptKey(txHash))
	if bz == nil {
		return nil, receiptdb.ErrNotFound
	}
	var r evmtypes.Receipt
	if err := r.Unmarshal(bz); err != nil {
		return nil, err
	}
	return &r, nil
}
