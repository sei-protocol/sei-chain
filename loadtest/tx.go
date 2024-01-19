package main

import (
	"context"
	typestx "github.com/cosmos/cosmos-sdk/types/tx"
)

func SendTx(
	ctx context.Context,
	txBytes []byte,
	mode typestx.BroadcastMode,
	loadtestClient LoadTestClient,
) {

	loadtestClient.GetTxClient().BroadcastTx(
		ctx,
		&typestx.BroadcastTxRequest{
			Mode:    mode,
			TxBytes: txBytes,
		},
	)
}
