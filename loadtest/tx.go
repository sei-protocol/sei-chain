package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"sync"
	"time"

	wasmtypes "github.com/CosmWasm/wasmd/x/wasm/types"
	"github.com/cosmos/cosmos-sdk/client"
	cryptotypes "github.com/cosmos/cosmos-sdk/crypto/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	typestx "github.com/cosmos/cosmos-sdk/types/tx"
)

const (
	LONG  = "Long"
	SHORT = "Short"
	OPEN  = "Open"
	CLOSE = "Close"
)

func SendTx(
	key cryptotypes.PrivKey,
	txBuilder *client.TxBuilder,
	mu *sync.Mutex,
) func() {
	(*txBuilder).SetGasLimit(2000000)
	(*txBuilder).SetFeeAmount([]sdk.Coin{
		sdk.NewCoin("ust", sdk.NewInt(1000)),
	})
	SignTx(txBuilder, key)
	txBytes, _ := TEST_CONFIG.TxConfig.TxEncoder()((*txBuilder).GetTx())
	return func() {
		grpcRes, err := TX_CLIENT.BroadcastTx(
			context.Background(),
			&typestx.BroadcastTxRequest{
				Mode:    typestx.BroadcastMode_BROADCAST_MODE_SYNC,
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
			grpcRes, err = TX_CLIENT.BroadcastTx(
				context.Background(),
				&typestx.BroadcastTxRequest{
					Mode:    typestx.BroadcastMode_BROADCAST_MODE_SYNC,
					TxBytes: txBytes,
				},
			)
			if err != nil {
				panic(err)
			}
		}
		if grpcRes.TxResponse.Code != 0 {
			fmt.Printf("Error: %d\n", grpcRes.TxResponse.Code)
		} else {
			mu.Lock()
			defer mu.Unlock()
			TX_HASH_FILE.WriteString(fmt.Sprintf("%s\n", grpcRes.TxResponse.TxHash))
		}
	}
}

func GetLimitOrderTxBuilder(
	contractAddress string,
	key cryptotypes.PrivKey,
	price uint64,
	quantity uint64,
	long bool,
	open bool,
	nonce uint64,
) client.TxBuilder {
	txBuilder := TEST_CONFIG.TxConfig.NewTxBuilder()
	var direction string
	if long {
		direction = LONG
	} else {
		direction = SHORT
	}
	var effect string
	if open {
		effect = OPEN
	} else {
		effect = CLOSE
	}
	body := map[string]interface{}{
		"limit_order": map[string]interface{}{
			"price":              strconv.FormatUint(price, 10),
			"quantity":           strconv.FormatUint(quantity, 10),
			"position_direction": direction,
			"position_effect":    effect,
			"price_denom":        "ust",
			"asset_denom":        "luna",
			"nonce":              nonce,
		},
	}
	amount, err := sdk.ParseCoinsNormalized(fmt.Sprintf("%d%s", price*quantity, "ust"))
	if err != nil {
		panic(err)
	}
	serialized_body, _ := json.Marshal(body)
	msg := wasmtypes.MsgExecuteContract{
		Sender:   sdk.AccAddress(key.PubKey().Address()).String(),
		Contract: contractAddress,
		Msg:      serialized_body,
		Funds:    amount,
	}
	_ = txBuilder.SetMsgs(&msg)
	return txBuilder
}

func GetMarketOrderTxBuilder(
	contractAddress string,
	key cryptotypes.PrivKey,
	price uint64,
	quantity uint64,
	long bool,
	open bool,
	nonce uint64,
) client.TxBuilder {
	txBuilder := TEST_CONFIG.TxConfig.NewTxBuilder()
	var direction string
	if long {
		direction = LONG
	} else {
		direction = SHORT
	}
	var effect string
	if open {
		effect = OPEN
	} else {
		effect = CLOSE
	}
	body := map[string]interface{}{
		"market_order": map[string]interface{}{
			"worst_price":        strconv.FormatUint(price, 10),
			"quantity":           strconv.FormatUint(quantity, 10),
			"position_direction": direction,
			"position_effect":    effect,
			"price_denom":        "ust",
			"asset_denom":        "luna",
			"nonce":              nonce,
		},
	}
	amount, err := sdk.ParseCoinsNormalized(fmt.Sprintf("%d%s", price*quantity, "ust"))
	if err != nil {
		panic(err)
	}
	serialized_body, _ := json.Marshal(body)
	msg := wasmtypes.MsgExecuteContract{
		Sender:   sdk.AccAddress(key.PubKey().Address()).String(),
		Contract: contractAddress,
		Msg:      serialized_body,
		Funds:    amount,
	}
	_ = txBuilder.SetMsgs(&msg)
	return txBuilder
}
