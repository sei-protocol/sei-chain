// Package types holds the data types the receipt store interface reads and returns.
package types

import (
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	evmtypes "github.com/sei-protocol/sei-chain/x/evm/types"
)

type ReceiptRecord struct {
	TxHash       common.Hash
	Receipt      *evmtypes.Receipt
	ReceiptBytes []byte // Optional pre-marshaled receipt (must match Receipt if set)
	// TxOffset and TxLength locate the transaction within its block's marshalled
	// proto(Block) body, excluding the block store's value framing. TxLength is 0
	// when the location is unknown.
	TxOffset uint32
	TxLength uint32
	// Reward is this tx's priority fee (EffectiveGasPrice - base fee), or nil to exclude it from
	// the block's reward aggregates.
	Reward *big.Int
}

// MarshaledReceipt returns ReceiptBytes when set, and otherwise marshals Receipt.
func (r ReceiptRecord) MarshaledReceipt() ([]byte, error) {
	if len(r.ReceiptBytes) > 0 {
		return r.ReceiptBytes, nil
	}
	return r.Receipt.Marshal()
}
