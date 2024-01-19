package main

import (
	"context"
	"fmt"
	typestx "github.com/cosmos/cosmos-sdk/types/tx"
)

func SendTx(
	ctx context.Context,
	txBytes []byte,
	mode typestx.BroadcastMode,
	loadtestClient LoadTestClient,
) bool {

	_, err := loadtestClient.GetTxClient().BroadcastTx(
		ctx,
		&typestx.BroadcastTxRequest{
			Mode:    mode,
			TxBytes: txBytes,
		},
	)
	if err != nil && ctx.Err() == nil {
		fmt.Printf("Failed to broadcast tx: %v \n", err)
		return false
	}
	return true
}
