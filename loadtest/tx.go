package main

import (
	"context"
	"fmt"
	"sync/atomic"
	"time"

	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	typestx "github.com/cosmos/cosmos-sdk/types/tx"
)

func SendTx(
	txBytes []byte,
	mode typestx.BroadcastMode,
	failureExpected bool,
	loadtestClient LoadTestClient,
	constant bool,
	sentCount *int64,
) {
	grpcRes, err := loadtestClient.GetTxClient().BroadcastTx(
		context.Background(),
		&typestx.BroadcastTxRequest{
			Mode:    mode,
			TxBytes: txBytes,
		},
	)
	fmt.Printf("Finished broadcasting tx\n")
	if err != nil {
		if failureExpected {
			fmt.Printf("Error: %s\n", err)
		} else {
			fmt.Printf("Finished broadcasting tx-err\n")
			panic(err)
		}

		if grpcRes == nil || grpcRes.TxResponse == nil {
			fmt.Printf("Finished broadcasting tx - nill response\n")
			return
		}
		if grpcRes.TxResponse.Code == 0 {
			atomic.AddInt64(sentCount, 1)
		} else {
			fmt.Printf("Finished broadcasting tx nonzero resp code: %d\n", grpcRes.TxResponse.Code)

		}
	}

	for grpcRes.TxResponse.Code == sdkerrors.ErrMempoolIsFull.ABCICode() {
		// retry after a second until either succeed or fail for some other reason
		fmt.Printf("Mempool full\n")
		time.Sleep(1 * time.Second)
		grpcRes, err = loadtestClient.GetTxClient().BroadcastTx(
			context.Background(),
			&typestx.BroadcastTxRequest{
				Mode:    mode,
				TxBytes: txBytes,
			},
		)
		if err != nil {
			if failureExpected {
			} else {
				panic(err)
			}
		}
	}
	if grpcRes.TxResponse.Code != 0 {
		fmt.Printf("Error: %d, %s\n", grpcRes.TxResponse.Code, grpcRes.TxResponse.RawLog)
	} else {
		if !constant { // only track txs if we're not running constant load
			loadtestClient.AppendTxHash(grpcRes.TxResponse.TxHash)
		}
		atomic.AddInt64(sentCount, 1)
	}
}
