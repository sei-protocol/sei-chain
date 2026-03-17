package blobfee

import (
	"math/big"

	"github.com/ethereum/go-ethereum/params"
	"github.com/sei-protocol/sei-chain/utils"
)

// BlobBaseFeeForNextBlock returns the blob base fee for the next block in wei.
// chainConfig and blockTime define the fork and time context; excessBlobGas may be nil (treated as 0).
// Reusable by RPC (eth_blobBaseFee) or execution; when dynamic blob fee is added, extend here.
func BlobBaseFeeForNextBlock(chainConfig *params.ChainConfig, blockTime uint64, excessBlobGas *uint64) *big.Int {
	_ = chainConfig
	_ = blockTime
	_ = excessBlobGas
	// Cancun not enabled / no dynamic blob fee: fixed 1 wei (matches execution BlockContext).
	return utils.Big1
}
