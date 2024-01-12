package main

import (
	"context"
	typestx "github.com/cosmos/cosmos-sdk/types/tx"
	"sync/atomic"
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

	if grpcRes.TxResponse.Code == 0 {
		atomic.AddInt64(sentCount, 1)
	}
}
