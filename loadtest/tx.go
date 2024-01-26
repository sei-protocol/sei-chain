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
	grpcRes, err := loadtestClient.GetTxClient().BroadcastTx(
		ctx,
		&typestx.BroadcastTxRequest{
			Mode:    mode,
			TxBytes: txBytes,
		},
	)
	if grpcRes != nil {
		if grpcRes.TxResponse.Code == 0 {
			return true
		} else {
			fmt.Printf("Failed to broadcast tx with response: %v \n", grpcRes)
		}
	} else if err != nil && ctx.Err() == nil {
		fmt.Printf("Failed to broadcast tx: %v \n", err)
	}
	return false
}
