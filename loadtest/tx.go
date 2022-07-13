package main

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/cosmos/cosmos-sdk/client"
	cryptotypes "github.com/cosmos/cosmos-sdk/crypto/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	typestx "github.com/cosmos/cosmos-sdk/types/tx"
)

func SendTx(
	key cryptotypes.PrivKey,
	txBuilder *client.TxBuilder,
	mode typestx.BroadcastMode,
	seqDelta uint64,
	mu *sync.Mutex,
) func() {
	(*txBuilder).SetGasLimit(2000000)
	(*txBuilder).SetFeeAmount([]sdk.Coin{
		sdk.NewCoin("usei", sdk.NewInt(100000)),
	})
	SignTx(txBuilder, key, seqDelta)
	txBytes, _ := TestConfig.TxConfig.TxEncoder()((*txBuilder).GetTx())
	return func() {
		grpcRes, err := TxClient.BroadcastTx(
			context.Background(),
			&typestx.BroadcastTxRequest{
				Mode:    mode,
				TxBytes: txBytes,
			},
		)
		if err != nil {
			panic(err)
		}
		for grpcRes.TxResponse.Code == sdkerrors.ErrMempoolIsFull.ABCICode() {
			// retry after a second until either succeed or fail for some other reason
			fmt.Printf("Mempool full\n")
			time.Sleep(1 * time.Second)
			grpcRes, err = TxClient.BroadcastTx(
				context.Background(),
				&typestx.BroadcastTxRequest{
					Mode:    mode,
					TxBytes: txBytes,
				},
			)
			if err != nil {
				panic(err)
			}
		}
		if grpcRes.TxResponse.Code != 0 {
			fmt.Printf("Error: %d, %s\n", grpcRes.TxResponse.Code, grpcRes.TxResponse.RawLog)
		} else {
			mu.Lock()
			defer mu.Unlock()
			if _, err := TxHashFile.WriteString(fmt.Sprintf("%s\n", grpcRes.TxResponse.TxHash)); err != nil {
				panic(err)
			}
		}
	}
}
