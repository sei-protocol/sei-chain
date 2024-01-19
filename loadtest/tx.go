package main

import (
	"context"
	"fmt"
	"sync/atomic"

	typestx "github.com/cosmos/cosmos-sdk/types/tx"
)

func SendTx(
	ctx context.Context,
	txBytes []byte,
	mode typestx.BroadcastMode,
	failureExpected bool,
	loadtestClient LoadTestClient,
	sentCount *int64,
) {
	grpcRes, _ := loadtestClient.GetTxClient().BroadcastTx(
		ctx,
		&typestx.BroadcastTxRequest{
			Mode:    mode,
			TxBytes: txBytes,
		},
	)

	if failureExpected {
		atomic.AddInt64(sentCount, 1)
		return
	} else if grpcRes != nil && grpcRes.TxResponse.Code == 0 {
		atomic.AddInt64(sentCount, 1)
		return
	}
}
