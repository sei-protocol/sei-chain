package evmonly

import (
	"context"
	"fmt"
	"math"

	"github.com/ethereum/go-ethereum/common"

	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/receipt"
	tmproto "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"
	evmtypes "github.com/sei-protocol/sei-chain/x/evm/types"
)

func receiptRecords(blockNumber uint64, result *BlockResult) ([]receipt.ReceiptRecord, error) {
	if len(result.Receipts) != len(result.Txs) {
		return nil, fmt.Errorf("receipt count %d does not match transaction result count %d", len(result.Receipts), len(result.Txs))
	}
	records := make([]receipt.ReceiptRecord, len(result.Receipts))
	for i, ethReceipt := range result.Receipts {
		if ethReceipt == nil {
			return nil, fmt.Errorf("receipt %d is nil", i)
		}
		if uint64(ethReceipt.TransactionIndex) > math.MaxUint32 {
			return nil, fmt.Errorf("receipt %d transaction index %d exceeds uint32", i, ethReceipt.TransactionIndex)
		}
		txResult := result.Txs[i]
		stored := &evmtypes.Receipt{
			TxType:            uint32(ethReceipt.Type),
			CumulativeGasUsed: ethReceipt.CumulativeGasUsed,
			TxHashHex:         ethReceipt.TxHash.Hex(),
			GasUsed:           ethReceipt.GasUsed,
			BlockNumber:       blockNumber,
			TransactionIndex:  uint32(ethReceipt.TransactionIndex),
			Status:            uint32(ethReceipt.Status),
			From:              txResult.Sender.Hex(),
			Logs:              evmtypes.NewLogsFromEth(ethReceipt.Logs),
			LogsBloom:         append([]byte(nil), ethReceipt.Bloom[:]...),
		}
		if ethReceipt.EffectiveGasPrice != nil {
			stored.EffectiveGasPrice = ethReceipt.EffectiveGasPrice.Uint64()
		}
		if txResult.To != nil {
			stored.To = txResult.To.Hex()
		}
		if txResult.ContractAddress != (common.Address{}) {
			stored.ContractAddress = txResult.ContractAddress.Hex()
		}
		if txResult.Err != nil {
			stored.VmError = txResult.Err.Error()
		}
		records[i] = receipt.ReceiptRecord{TxHash: ethReceipt.TxHash, Receipt: stored}
	}
	return records, nil
}

func newReceiptContext(ctx context.Context, blockHeight int64) sdk.Context {
	return sdk.NewContext(nil, tmproto.Header{Height: blockHeight}, false).WithContext(ctx)
}
