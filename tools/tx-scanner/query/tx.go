package query

import (
	"context"
	"fmt"

	txtypes "github.com/cosmos/cosmos-sdk/types/tx"
	"github.com/sei-protocol/sei-chain/tools/tx-scanner/client"
)

// GetTxsEvent query the detailed transaction data, same as `seid q txs --events`
func GetTxsEvent(blockHeight int64) (*txtypes.GetTxsEventResponse, error) {
	request := &txtypes.GetTxsEventRequest{
		Events: []string{fmt.Sprintf("tx.height=%d", blockHeight)},
	}

	return client.GetTxClient().GetTxsEvent(context.Background(), request)
}

// GetTxByHash query the transaction by TX hash, same as `seid q tx --hash`
func GetTxByHash(txHash string) (*txtypes.GetTxResponse, error) {
	request := &txtypes.GetTxRequest{
		Hash: txHash,
	}
	return client.GetTxClient().GetTx(context.Background(), request)
}
