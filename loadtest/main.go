package main

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"os/exec"
	"strconv"
	"time"

	cryptotypes "github.com/cosmos/cosmos-sdk/crypto/types"
	"github.com/cosmos/cosmos-sdk/std"

	"github.com/cosmos/cosmos-sdk/x/auth/tx"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"

	"github.com/cosmos/cosmos-sdk/codec"
	"github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/app"
	dextypes "github.com/sei-protocol/sei-chain/x/dex/types"
)

var TestConfig EncodingConfig

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

func run() {
	client := NewLoadTestClient()
	config := client.LoadTestConfig

	defer client.Close()

	if config.TxsPerBlock < config.MsgsPerTx {
		panic("Must have more TxsPerBlock than MsgsPerTx")
	}

	configString, _ := json.Marshal(config)
	fmt.Printf("Running with \n %s \n", string(configString))

	fmt.Printf("%s - Starting block prepare\n", time.Now().Format("2006-01-02T15:04:05"))
	workgroups, sendersList := client.BuildTxs()

	client.SendTxs(workgroups, sendersList)

	// Records the resulting TxHash to file
	client.WriteTxHashToFile()
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
	run()
}
