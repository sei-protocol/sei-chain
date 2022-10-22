package main

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"os/exec"
	"strconv"
	"sync"
	"time"

	cryptotypes "github.com/cosmos/cosmos-sdk/crypto/types"
	"github.com/cosmos/cosmos-sdk/std"

	"github.com/cosmos/cosmos-sdk/x/auth/tx"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"

	"github.com/cosmos/cosmos-sdk/codec"
	"github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	typestx "github.com/cosmos/cosmos-sdk/types/tx"
	"github.com/sei-protocol/sei-chain/app"
	dextypes "github.com/sei-protocol/sei-chain/x/dex/types"
)

var (
	TestConfig EncodingConfig
)

const (
	VortexData = "{\"position_effect\":\"Open\",\"leverage\":\"1\"}"
)

var FromMili = sdk.NewDec(1000000)

func init() {
	cdc := codec.NewLegacyAmino()
	interfaceRegistry := types.NewInterfaceRegistry()
	marshaler := codec.NewProtoCodec(interfaceRegistry)

	TestConfig = EncodingConfig{
		InterfaceRegistry: interfaceRegistry,
		Marshaler:         marshaler,
		TxConfig:          tx.NewTxConfig(marshaler, tx.DefaultSignModes),
		Amino:             cdc,
	}
	std.RegisterLegacyAminoCodec(TestConfig.Amino)
	std.RegisterInterfaces(TestConfig.InterfaceRegistry)
	app.ModuleBasics.RegisterLegacyAminoCodec(TestConfig.Amino)
	app.ModuleBasics.RegisterInterfaces(TestConfig.InterfaceRegistry)
}

func run(config Config) {
	client := NewLoadTestClient(config.ChainID)
	defer client.Close()

	if config.TxsPerBlock < config.MsgsPerTx {
		panic("Must have more orders per block than batch size")
	}

	numberOfAccounts := config.TxsPerBlock / config.MsgsPerTx * 2 // * 2 because we need two sets of accounts
	activeAccounts := []int{}
	inactiveAccounts := []int{}

	for i := 0; i < int(numberOfAccounts); i++ {
		if i%2 == 0 {
			activeAccounts = append(activeAccounts, i)
		} else {
			inactiveAccounts = append(inactiveAccounts, i)
		}
	}

	wgs := []*sync.WaitGroup{}
	sendersList := [][]func(){}

	configString, _ := json.Marshal(config)
	fmt.Printf("Running with \n %s \ns", string(configString))

	fmt.Printf("%s - Starting block prepare\n", time.Now().Format("2006-01-02T15:04:05"))
	for i := 0; i < int(config.Rounds); i++ {
		fmt.Printf("Preparing %d-th round\n", i)
		wg := &sync.WaitGroup{}
		var senders []func()
		wgs = append(wgs, wg)
		for _, account := range activeAccounts {
			key := GetKey(uint64(account))

			msg := generateMessage(config, key, config.MsgsPerTx)
			txBuilder := TestConfig.TxConfig.NewTxBuilder()
			_ = txBuilder.SetMsgs(msg)
			seqDelta := uint64(i / 2)
			mode := typestx.BroadcastMode_BROADCAST_MODE_SYNC

			// Note: There is a potential race condition here with seqnos
			// in which a later seqno is delievered before an earlier seqno
			// In practice, we haven't run into this issue so we'll leave this
			// as is.
			sender := SendTx(key, &txBuilder, mode, seqDelta, *client)
			wg.Add(1)
			senders = append(senders, func() {
				defer wg.Done()
				sender()
			})
		}
		sendersList = append(sendersList, senders)

		inactiveAccounts, activeAccounts = activeAccounts, inactiveAccounts
	}

	lastHeight := getLastHeight()
	for i := 0; i < int(config.Rounds); i++ {
		newHeight := getLastHeight()
		for newHeight == lastHeight {
			time.Sleep(10 * time.Millisecond)
			newHeight = getLastHeight()
		}
		fmt.Printf("Sending %d-th block\n", i)
		senders := sendersList[i]
		wg := wgs[i]
		for _, sender := range senders {
			go sender()
		}
		wg.Wait()
		lastHeight = newHeight
	}
	fmt.Printf("%s - Finished\n", time.Now().Format("2006-01-02T15:04:05"))
}

func generateMessage(config Config, key cryptotypes.PrivKey, msgPerTx uint64) sdk.Msg {
	var msg sdk.Msg
	switch config.MessageType {
	case "basic":
		msg = &banktypes.MsgSend{
			FromAddress: sdk.AccAddress(key.PubKey().Address()).String(),
			ToAddress:   sdk.AccAddress(key.PubKey().Address()).String(),
			Amount: sdk.NewCoins(sdk.Coin{
				Denom:  "usei",
				Amount: sdk.NewInt(1),
			}),
		}
	case "dex":
		msgType := config.MsgTypeDistr.Sample()
		orderPlacements := []*dextypes.Order{}
		var orderType dextypes.OrderType
		if msgType == "limit" {
			orderType = dextypes.OrderType_LIMIT
		} else {
			orderType = dextypes.OrderType_MARKET
		}
		var direction dextypes.PositionDirection
		if rand.Float64() < 0.5 {
			direction = dextypes.PositionDirection_LONG
		} else {
			direction = dextypes.PositionDirection_SHORT
		}
		price := config.PriceDistr.Sample()
		quantity := config.QuantityDistr.Sample()
		contract := config.ContractDistr.Sample()
		for j := 0; j < int(msgPerTx); j++ {
			orderPlacements = append(orderPlacements, &dextypes.Order{
				Account:           sdk.AccAddress(key.PubKey().Address()).String(),
				ContractAddr:      contract,
				PositionDirection: direction,
				Price:             price.Quo(FromMili),
				Quantity:          quantity.Quo(FromMili),
				PriceDenom:        "SEI",
				AssetDenom:        "ATOM",
				OrderType:         orderType,
				Data:              VortexData,
			})
		}
		amount, err := sdk.ParseCoinsNormalized(fmt.Sprintf("%d%s", price.Mul(quantity).Ceil().RoundInt64(), "usei"))
		if err != nil {
			panic(err)
		}
		msg = &dextypes.MsgPlaceOrders{
			Creator:      sdk.AccAddress(key.PubKey().Address()).String(),
			Orders:       orderPlacements,
			ContractAddr: contract,
			Funds:        amount,
		}
	default:
		fmt.Printf("Unrecognized message type %s", config.MessageType)
	}
	return msg
}

func getLastHeight() int {
	out, err := exec.Command("curl", "http://localhost:26657/blockchain").Output()
	if err != nil {
		panic(err)
	}
	var dat map[string]interface{}
	if err := json.Unmarshal(out, &dat); err != nil {
		panic(err)
	}
	height, err := strconv.Atoi(dat["last_height"].(string))
	if err != nil {
		panic(err)
	}
	return height
}

func main() {
	config := Config{}
	pwd, _ := os.Getwd()
	file, _ := os.ReadFile(pwd + "/loadtest/config.json")
	if err := json.Unmarshal(file, &config); err != nil {
		panic(err)
	}
	run(config)
}
