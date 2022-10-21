package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	cryptotypes "github.com/cosmos/cosmos-sdk/crypto/types"

	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/codec"
	"github.com/cosmos/cosmos-sdk/codec/types"
	"github.com/cosmos/cosmos-sdk/std"
	sdk "github.com/cosmos/cosmos-sdk/types"
	typestx "github.com/cosmos/cosmos-sdk/types/tx"
	"github.com/cosmos/cosmos-sdk/x/auth/tx"
	"github.com/sei-protocol/sei-chain/app"
	"github.com/sei-protocol/sei-chain/utils"
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
	BatchSize      uint64                `json:"batch_size"`
	ChainID        string                `json:"chain_id"`
	OrdersPerBlock uint64                `json:"orders_per_block"`
	Rounds         uint64                `json:"rounds"`
	MessageType    string                `json:"message_type"`
	PriceDistr     NumericDistribution   `json:"price_distribution"`
	QuantityDistr  NumericDistribution   `json:"quantity_distribution"`
	MsgTypeDistr   MsgTypeDistribution   `json:"message_type_distribution"`
	ContractDistr  ContractDistributions `json:"contract_distribution"`
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

// Invalid numeric distribution sample
func (d *NumericDistribution) InvalidSample() sdk.Dec {
	steps := sdk.NewDec(rand.Int63n(d.NumDistinct))
	if rand.Float64() < 0.5 {
		return d.Min.Sub(d.Max.Sub(d.Min).QuoInt64(d.NumDistinct).Mul(steps))
	} else {
		return d.Max.Add(d.Max.Sub(d.Min).QuoInt64(d.NumDistinct).Mul(steps))
	}
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

type InvalidMsgTypeDistribution struct {
	FakeLimitOrderPct  sdk.Dec `json:"fake_limit_order_percentage"`
	FakeMarketOrderPct sdk.Dec `json:"fake_market_order_percentage"`
}

// Invalid msg type distribution sample
func (d *InvalidMsgTypeDistribution) InvalidSample() string {
	if !d.FakeLimitOrderPct.Add(d.FakeMarketOrderPct).Equal(sdk.OneDec()) {
		panic("Distribution percentages for failure case must add up to 1")
	}
	randNum := sdk.MustNewDecFromStr(fmt.Sprintf("%f", rand.Float64()))
	if randNum.LT(d.FakeLimitOrderPct) {
		return "fake_limit"
	}
	return "fake_market"
}

type ContractDistributions []ContractDistribution

func (d *ContractDistributions) Sample() string {
	if !utils.Reduce(*d, func(i ContractDistribution, o sdk.Dec) sdk.Dec { return o.Add(i.Percentage) }, sdk.ZeroDec()).Equal(sdk.OneDec()) {
		panic("Distribution percentages must add up to 1")
	}
	randNum := sdk.MustNewDecFromStr(fmt.Sprintf("%f", rand.Float64()))
	cumPct := sdk.ZeroDec()
	for _, dist := range *d {
		cumPct = cumPct.Add(dist.Percentage)
		if randNum.LTE(cumPct) {
			return dist.ContractAddr
		}
	}
	panic("this should never be triggered")
}

type ContractDistribution struct {
	ContractAddr string  `json:"contract_address"`
	Percentage   sdk.Dec `json:"percentage"`
}

var (
	TestConfig EncodingConfig
	TxClient   typestx.ServiceClient
	TxHashFile *os.File
	ChainID    string
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
	batchSize := config.BatchSize
	if config.OrdersPerBlock < batchSize {
		panic("Must have more orders per block than batch size")
	}

	numberOfAccounts := config.OrdersPerBlock / batchSize * 2 // * 2 because we need two sets of accounts
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

			msg, failureExpected := generateMessage(config, key, batchSize)
			txBuilder := TestConfig.TxConfig.NewTxBuilder()
			_ = txBuilder.SetMsgs(msg)
			seqDelta := uint64(i / 2)
			mode := typestx.BroadcastMode_BROADCAST_MODE_SYNC

			// Note: There is a potential race condition here with seqnos
			// in which a later seqno is delievered before an earlier seqno
			// In practice, we haven't run into this issue so we'll leave this
			// as is.
			sender := SendTx(key, &txBuilder, mode, seqDelta, &mu, failureExpected)
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

func generateMessage(config Config, key cryptotypes.PrivKey, batchSize uint64) (sdk.Msg, bool) {
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
	case "failure_basic_malformed":
		var denom string
		if rand.Float64() < 0.5 {
			denom = "unknown"
		} else {
			denom = "other"
		}
		var fromAddr string
		if rand.Float64() < 0.5 {
			fromAddr = "fromAddressRandom"
		} else {
			fromAddr = "otherFromAddressRandom"
		}
		var toAddr string
		if rand.Float64() < 0.5 {
			fromAddr = "toAddressRandom"
		} else {
			fromAddr = "toFromAddressRandom"
		}
		msg = &banktypes.MsgSend{
			FromAddress: fromAddr,
			ToAddress:   toAddr,
			Amount: sdk.NewCoins(sdk.Coin{
				Denom:  denom,
				Amount: sdk.NewInt(1),
			}),
		}
	case "failure_basic_invalid":
		var amount_usei int64
		if rand.Float64() < 0.5 {
			amount_usei = 1000000000000000000
		} else {
			amount_usei = 0
		}
		msg = &banktypes.MsgSend{
			FromAddress: sdk.AccAddress(key.PubKey().Address()).String(),
			ToAddress:   sdk.AccAddress(key.PubKey().Address()).String(),
			Amount: sdk.NewCoins(sdk.Coin{
				Denom:  "usei",
				Amount: sdk.NewInt(amount_usei),
			}),
		}
	case "failure_dex_malformed":
		msgType := config.MsgTypeDistr.Sample()
		orderPlacements := []*dextypes.Order{}
		var orderType dextypes.OrderType
		if msgType == "fake_limit" {
			orderType = 8
		} else {
			orderType = 9
		}
		var direction dextypes.PositionDirection
		if rand.Float64() < 0.5 {
			direction = dextypes.PositionDirection_LONG
		} else {
			direction = dextypes.PositionDirection_SHORT
		}
		price := config.PriceDistr.InvalidSample()
		quantity := config.QuantityDistr.InvalidSample()
		contract := config.ContractDistr.Sample()
		for j := 0; j < int(batchSize); j++ {
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
	case "failure_dex_invalid":
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
		for j := 0; j < int(batchSize); j++ {
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
		var amount_usei int64
		if rand.Float64() < 0.5 {
			amount_usei = 1000000000000000000
		} else {
			amount_usei = 0
		}
		amount, err := sdk.ParseCoinsNormalized(fmt.Sprintf("%d%s", amount_usei, "usei"))
		if err != nil {
			panic(err)
		}
		msg = &dextypes.MsgPlaceOrders{
			Creator:      sdk.AccAddress(key.PubKey().Address()).String(),
			Orders:       orderPlacements,
			ContractAddr: contract,
			Funds:        amount,
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
		for j := 0; j < int(batchSize); j++ {
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

	if strings.Contains(config.MessageType, "failure") {
		return msg, true
	} else {
		return msg, false
	}
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
	clientType := flag.String("clientType", "", "a string")
	flag.Parse()
	fmt.Printf("in main -> clientType: %s \n", *clientType)
	config := Config{}
	pwd, _ := os.Getwd()
	file, _ := os.ReadFile(pwd + "/loadtest/config.json")
	if *clientType == "failure_basic_malformed" {
		file, _ = os.ReadFile(pwd + "/loadtest/failure_basic_malformed.json")
	}
	if *clientType == "failure_basic_invalid" {
		file, _ = os.ReadFile(pwd + "/loadtest/failure_basic_invalid.json")
	}
	if *clientType == "failure_dex_malformed" {
		file, _ = os.ReadFile(pwd + "/loadtest/failure_dex_malformed.json")
	}
	if *clientType == "failure_dex_invalid" {
		file, _ = os.ReadFile(pwd + "/loadtest/failure_dex_invalid.json")
	}
	if err := json.Unmarshal(file, &config); err != nil {
		panic(err)
	}
	run(config)
}
