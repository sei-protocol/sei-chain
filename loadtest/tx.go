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
	gas uint64,
	fee int64,
) func() {
	(*txBuilder).SetGasLimit(gas)
	(*txBuilder).SetFeeAmount([]sdk.Coin{
		sdk.NewCoin("usei", sdk.NewInt(fee)),
	})
	loadtestClient.SignerClient.SignTx(loadtestClient.ChainID, txBuilder, key, seqDelta)
	txBytes, _ := TestConfig.TxConfig.TxEncoder()((*txBuilder).GetTx())
	return func() {
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
					fmt.Printf("key=%s error=%s\n", key.PubKey().Address().String(), err)
				} else {
					panic(err)
				}
			}
		}
		if grpcRes.TxResponse.Code != 0 {
			fmt.Printf("Error: %d, %s\n", grpcRes.TxResponse.Code, grpcRes.TxResponse.RawLog)
		} else {
			loadtestClient.AppendTxHash(grpcRes.TxResponse.TxHash)
		}
	}
}
