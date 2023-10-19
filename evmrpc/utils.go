package evmrpc

import (
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/sei-protocol/sei-chain/x/evm/types"
)

const LatestCtxHeight int64 = -1

// Since we use a static base fee, we want GasUsedRatio returned in RPC queries
// to reflect the fact that base fee would never change, which is only true if
// the block is exactly half-utilized.
const GasUsedRatio float64 = 0.5

type RPCTransaction struct {
	BlockHash           *common.Hash         `json:"blockHash"`
	BlockNumber         *hexutil.Big         `json:"blockNumber"`
	From                common.Address       `json:"from"`
	Gas                 hexutil.Uint64       `json:"gas"`
	GasPrice            *hexutil.Big         `json:"gasPrice"`
	GasFeeCap           *hexutil.Big         `json:"maxFeePerGas,omitempty"`
	GasTipCap           *hexutil.Big         `json:"maxPriorityFeePerGas,omitempty"`
	MaxFeePerBlobGas    *hexutil.Big         `json:"maxFeePerBlobGas,omitempty"`
	Hash                common.Hash          `json:"hash"`
	Input               hexutil.Bytes        `json:"input"`
	Nonce               hexutil.Uint64       `json:"nonce"`
	To                  *common.Address      `json:"to"`
	TransactionIndex    *hexutil.Uint64      `json:"transactionIndex"`
	Value               *hexutil.Big         `json:"value"`
	Type                hexutil.Uint64       `json:"type"`
	Accesses            *ethtypes.AccessList `json:"accessList,omitempty"`
	ChainID             *hexutil.Big         `json:"chainId,omitempty"`
	BlobVersionedHashes []common.Hash        `json:"blobVersionedHashes,omitempty"`
	V                   *hexutil.Big         `json:"v"`
	R                   *hexutil.Big         `json:"r"`
	S                   *hexutil.Big         `json:"s"`
	YParity             *hexutil.Uint64      `json:"yParity,omitempty"`
}

func hydrateTransaction(
	tx *ethtypes.Transaction,
	blocknumber *big.Int,
	blockhash common.Hash,
	receipt *types.Receipt,
) RPCTransaction {
	idx := hexutil.Uint64(receipt.TransactionIndex)
	al := tx.AccessList()
	v, r, s := tx.RawSignatureValues()
	yparity := hexutil.Uint64(v.Sign())
	return RPCTransaction{
		BlockHash:           &blockhash,
		BlockNumber:         (*hexutil.Big)(blocknumber),
		From:                common.HexToAddress(receipt.From),
		Gas:                 hexutil.Uint64(tx.Gas()),
		GasPrice:            (*hexutil.Big)(tx.GasPrice()),
		GasFeeCap:           (*hexutil.Big)(tx.GasFeeCap()),
		GasTipCap:           (*hexutil.Big)(tx.GasTipCap()),
		MaxFeePerBlobGas:    (*hexutil.Big)(tx.BlobGasFeeCap()),
		Hash:                tx.Hash(),
		Input:               tx.Data(),
		Nonce:               hexutil.Uint64(tx.Nonce()),
		To:                  tx.To(),
		TransactionIndex:    &idx,
		Value:               (*hexutil.Big)(tx.Value()),
		Accesses:            &al,
		ChainID:             (*hexutil.Big)(tx.ChainId()),
		BlobVersionedHashes: tx.BlobHashes(),
		V:                   (*hexutil.Big)(v),
		S:                   (*hexutil.Big)(s),
		R:                   (*hexutil.Big)(r),
		YParity:             &yparity,
	}
}
