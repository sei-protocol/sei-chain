package main

import (
	"context"
	"fmt"
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
	failureExpected bool,
	loadtestClient LoadTestClient,
) func() string { // TODO: do we need to return string?
	(*txBuilder).SetGasLimit(200000000)
	(*txBuilder).SetFeeAmount([]sdk.Coin{
		sdk.NewCoin("usei", sdk.NewInt(10000000)),
	})
	loadtestClient.SignerClient.SignTx(loadtestClient.ChainID, txBuilder, key, seqDelta)
	txBytes, _ := TestConfig.TxConfig.TxEncoder()((*txBuilder).GetTx())
	return func() string {
		grpcRes, err := loadtestClient.TxClient.BroadcastTx(
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
		}
		for grpcRes.TxResponse.Code == sdkerrors.ErrMempoolIsFull.ABCICode() {
			// retry after a second until either succeed or fail for some other reason
			fmt.Printf("Mempool full\n")
			time.Sleep(1 * time.Second)
			grpcRes, err = loadtestClient.TxClient.BroadcastTx(
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
			}
		}
		if grpcRes.TxResponse.Code != 0 {
			fmt.Printf("Error: %d, %s\n", grpcRes.TxResponse.Code, grpcRes.TxResponse.RawLog)
		} else {
			loadtestClient.AppendTxHash(grpcRes.TxResponse.TxHash)
			return grpcRes.TxResponse.TxHash
		}
		return ""
	}
}
