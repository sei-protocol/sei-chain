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
) func() string {
	(*txBuilder).SetGasLimit(200000000)
	(*txBuilder).SetFeeAmount([]sdk.Coin{
		sdk.NewCoin("usei", sdk.NewInt(10000000)),
	})
	SignTx(txBuilder, key, seqDelta)
	txBytes, _ := TestConfig.TxConfig.TxEncoder()((*txBuilder).GetTx())
	return func() string {
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
			return grpcRes.TxResponse.TxHash
		}
		return "";
	}
}


func GetTxResponse(hash string) *sdk.TxResponse {
	grpcRes, err := TxClient.GetTx(
		context.Background(),
		&typestx.GetTxRequest{
			Hash: hash,
		},
	)
	if err != nil {
		fmt.Println(err)
		return &sdk.TxResponse{}
	} else {
		return grpcRes.TxResponse
	}
}
