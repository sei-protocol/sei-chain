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
) error {
	grpcRes, err := loadtestClient.GetTxClient().BroadcastTx(
		ctx,
		&typestx.BroadcastTxRequest{
			Mode:    mode,
			TxBytes: txBytes,
		},
	)
	if err != nil {
		return err	
	}
	if grpcRes.TxResponse.Code != 0 {
		return fmt.Errorf("Failed to broadcast tx with response: %v", grpcRes)
	}
	return nil
}
