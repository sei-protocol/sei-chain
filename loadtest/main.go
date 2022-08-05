package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/codec"
	"github.com/cosmos/cosmos-sdk/codec/types"
	"github.com/cosmos/cosmos-sdk/std"
	sdk "github.com/cosmos/cosmos-sdk/types"
	typestx "github.com/cosmos/cosmos-sdk/types/tx"
	"github.com/cosmos/cosmos-sdk/x/auth/tx"
	"github.com/sei-protocol/sei-chain/app"
	dextypes "github.com/sei-protocol/sei-chain/x/dex/types"
	"google.golang.org/grpc"
)

type EncodingConfig struct {
	InterfaceRegistry types.InterfaceRegistry
	// NOTE: this field will be renamed to Codec
	Marshaler codec.Codec
	TxConfig  client.TxConfig
	Amino     *codec.LegacyAmino
}

type Config struct {
	ChainID        string              `json:"chain_id"`
	ContractAddr   string              `json:"contract_address"`
	OrdersPerBlock uint64              `json:"orders_per_block"`
	Rounds         uint64              `json:"rounds"`
	PriceDistr     NumericDistribution `json:"price_distribution"`
	QuantityDistr  NumericDistribution `json:"quantity_distribution"`
	MsgTypeDistr   MsgTypeDistribution `json:"message_type_distribution"`
}

type NumericDistribution struct {
	Min         sdk.Dec `json:"min"`
	Max         sdk.Dec `json:"max"`
	NumDistinct int64   `json:"number_of_distinct_values"`
}

func (d *NumericDistribution) Sample() sdk.Dec {
	steps := sdk.NewDec(rand.Int63n(d.NumDistinct))
	return d.Min.Add(d.Max.Sub(d.Min).QuoInt64(d.NumDistinct).Mul(steps))
}

type MsgTypeDistribution struct {
	LimitOrderPct  sdk.Dec `json:"limit_order_percentage"`
	MarketOrderPct sdk.Dec `json:"market_order_percentage"`
}

func (d *MsgTypeDistribution) Sample() string {
	if !d.LimitOrderPct.Add(d.MarketOrderPct).Equal(sdk.OneDec()) {
		panic("Distribution percentages must add up to 1")
	}
	randNum := sdk.MustNewDecFromStr(fmt.Sprintf("%f", rand.Float64()))
	if randNum.LT(d.LimitOrderPct) {
		return "limit"
	}
	return "market"
}

var (
	TestConfig EncodingConfig
	TxClient   typestx.ServiceClient
	TxHashFile *os.File
	ChainID    string
)

const (
	BatchSize  = 100
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
	ChainID = config.ChainID
	grpcConn, _ := grpc.Dial(
		"127.0.0.1:9090",
		grpc.WithInsecure(),
	)
	defer grpcConn.Close()
	TxClient = typestx.NewServiceClient(grpcConn)
	userHomeDir, _ := os.UserHomeDir()
	_ = os.Mkdir(filepath.Join(userHomeDir, "outputs"), os.ModePerm)
	filename := filepath.Join(userHomeDir, "outputs", "test_tx_hash")
	_ = os.Remove(filename)
	file, _ := os.OpenFile(filename, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	TxHashFile = file
	var mu sync.Mutex

	if config.OrdersPerBlock < BatchSize {
		panic("Must have more orders per block than batch size")
	}
	numberOfAccounts := config.OrdersPerBlock / BatchSize * 2 // * 2 because we need two sets of accounts
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
	fmt.Printf("%s - Starting block prepare\n", time.Now().Format("2006-01-02T15:04:05"))
	for i := 0; i < int(config.Rounds); i++ {
		fmt.Printf("Preparing %d-th round\n", i)
		wg := &sync.WaitGroup{}
		var senders []func()
		wgs = append(wgs, wg)
		for _, account := range activeAccounts {
			key := GetKey(uint64(account))
			var msg sdk.Msg
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
			for j := 0; j < BatchSize; j++ {
				orderPlacements = append(orderPlacements, &dextypes.Order{
					Account:           sdk.AccAddress(key.PubKey().Address()).String(),
					ContractAddr:      config.ContractAddr,
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
				ContractAddr: config.ContractAddr,
				Funds:        amount,
			}
			txBuilder := TestConfig.TxConfig.NewTxBuilder()

			_ = txBuilder.SetMsgs(msg)
			seqDelta := uint64(i / 2)
			mode := typestx.BroadcastMode_BROADCAST_MODE_SYNC
			// Note: There is a potential race condition here with seqnos
			// in which a later seqno is delievered before an earlier seqno
			// In practice, we haven't run into this issue so we'll leave this
			// as is.
			sender := SendTx(key, &txBuilder, mode, seqDelta, &mu)
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

func getLastHeight() int {
	out, err := exec.Command("curl", "http://localhost:26657/blockchain").Output()
	if err != nil {
		panic(err)
	}
	var dat map[string]interface{}
	if err := json.Unmarshal(out, &dat); err != nil {
		panic(err)
	}
	result := dat["result"].(map[string]interface{})
	height, err := strconv.Atoi(result["last_height"].(string))
	if err != nil {
		panic(err)
	}
	return height
}

func main() {
	config := Config{}
	pwd, _ := os.Getwd()
	file, _ := ioutil.ReadFile(pwd + "/loadtest/config.json")
	if err := json.Unmarshal(file, &config); err != nil {
		panic(err)
	}
	run(config)
}
