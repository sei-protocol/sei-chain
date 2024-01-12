package main

import (
	"context"
	"fmt"
	typestx "github.com/cosmos/cosmos-sdk/types/tx"
	"sync/atomic"
)

func SendTx(
	txBytes []byte,
	mode typestx.BroadcastMode,
	failureExpected bool,
	loadtestClient LoadTestClient,
	sentCount *int64,
) {
	grpcRes, err := loadtestClient.GetTxClient().BroadcastTx(
		context.Background(),
		&typestx.BroadcastTxRequest{
			Mode:    mode,
			TxBytes: txBytes,
		},
	)
	if err != nil {
		if failureExpected {
			fmt.Printf("Error: %s\n", err)
		} else {
			panic(err)
		}

		if grpcRes == nil || grpcRes.TxResponse == nil {
			return
		}
		if grpcRes.TxResponse.Code == 0 {
			atomic.AddInt64(sentCount, 1)
		}
	}

	if grpcRes.TxResponse.Code == 0 {
		atomic.AddInt64(sentCount, 1)
	}
}
