package main

import (
	"encoding/json"
	"fmt"
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

func run(
	contractAddress string,
	numberOfAccounts uint64,
	numberOfBlocks uint64,
	longPriceFloor uint64,
	longPriceCeiling uint64,
	shortPriceFloor uint64,
	shortPriceCeiling uint64,
	quantityFloor uint64,
	quantityCeiling uint64,
) {
	grpcConn, _ := grpc.Dial(
		"127.0.0.1:9090",
		grpc.WithInsecure(),
	)
	defer grpcConn.Close()
	TxClient = typestx.NewServiceClient(grpcConn)
	userHomeDir, _ := os.UserHomeDir()
	filename := filepath.Join(userHomeDir, "outputs", "test_tx_hash")
	_ = os.Remove(filename)
	file, err := os.OpenFile(filename, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		fmt.Printf("Error opening file %s", err)
		return
	}
	TxHashFile = file
	var mu sync.Mutex

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
	for i := 0; i < int(numberOfBlocks); i++ {
		fmt.Printf("Preparing %d-th block\n", i)
		wg := &sync.WaitGroup{}
		var senders []func()
		wgs = append(wgs, wg)
		for j, account := range activeAccounts {
			key := GetKey(uint64(account))
			orderPlacements := []*dextypes.Order{}
			longPrice := uint64(j)%(longPriceCeiling-longPriceFloor) + longPriceFloor
			longQuantity := uint64(rand.Intn(int(quantityCeiling)-int(quantityFloor))) + quantityFloor
			shortPrice := uint64(j)%(shortPriceCeiling-shortPriceFloor) + shortPriceFloor
			shortQuantity := uint64(rand.Intn(int(quantityCeiling)-int(quantityFloor))) + quantityFloor
			for j := 0; j < BatchSize; j++ {
				orderPlacements = append(orderPlacements, &dextypes.Order{
					Account:           sdk.AccAddress(key.PubKey().Address()).String(),
					ContractAddr:      contractAddress,
					PositionDirection: dextypes.PositionDirection_LONG,
					Price:             sdk.NewDec(int64(longPrice)).Quo(FromMili),
					Quantity:          sdk.NewDec(int64(longQuantity)).Quo(FromMili),
					PriceDenom:        "SEI",
					AssetDenom:        "ATOM",
					OrderType:         dextypes.OrderType_LIMIT,
					Data:              VortexData,
				}, &dextypes.Order{
					Account:           sdk.AccAddress(key.PubKey().Address()).String(),
					ContractAddr:      contractAddress,
					PositionDirection: dextypes.PositionDirection_SHORT,
					Price:             sdk.NewDec(int64(shortPrice)).Quo(FromMili),
					Quantity:          sdk.NewDec(int64(shortQuantity)).Quo(FromMili),
					PriceDenom:        "SEI",
					AssetDenom:        "ATOM",
					OrderType:         dextypes.OrderType_LIMIT,
					Data:              VortexData,
				})
			}
			amount, err := sdk.ParseCoinsNormalized(fmt.Sprintf("%d%s", longPrice*longQuantity+shortPrice*shortQuantity, "usei"))
			if err != nil {
				panic(err)
			}
			msg := dextypes.MsgPlaceOrders{
				Creator:      sdk.AccAddress(key.PubKey().Address()).String(),
				Orders:       orderPlacements,
				ContractAddr: contractAddress,
				Funds:        amount,
			}
			txBuilder := TestConfig.TxConfig.NewTxBuilder()
			_ = txBuilder.SetMsgs(&msg)
			seqDelta := uint64(i / 2)
			SignTx(&txBuilder, key, seqDelta)
			mode := typestx.BroadcastMode_BROADCAST_MODE_SYNC
			if j == len(activeAccounts)-1 {
				mode = typestx.BroadcastMode_BROADCAST_MODE_BLOCK
			}
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
	for i := 0; i < int(numberOfBlocks); i++ {
		newHeight := getLastHeight()
		for newHeight == lastHeight {
			time.Sleep(50 * time.Millisecond)
			newHeight = getLastHeight()
		}
		fmt.Printf("Sending %d-th block\n", i)

		senders := sendersList[i]
		wg := wgs[i]

		for _, sender := range senders {
			go sender()
		}
		wg.Wait()
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
	result := dat["result"].(map[string]interface{})
	height, err := strconv.Atoi(result["last_height"].(string))
	if err != nil {
		panic(err)
	}
	return height
}

func main() {
	args := os.Args[1:]
	contractAddress := args[0]
	numberOfAccounts, _ := strconv.ParseUint(args[1], 10, 64)
	numberOfBlocks, _ := strconv.ParseUint(args[2], 10, 64)
	longPriceFloor, _ := strconv.ParseUint(args[3], 10, 64)
	longPriceCeiling, _ := strconv.ParseUint(args[4], 10, 64)
	shortPriceFloor, _ := strconv.ParseUint(args[5], 10, 64)
	shortPriceCeiling, _ := strconv.ParseUint(args[6], 10, 64)
	quantityFloor, _ := strconv.ParseUint(args[7], 10, 64)
	quantityCeiling, _ := strconv.ParseUint(args[8], 10, 64)
	chainID := args[9]
	ChainID = chainID
	run(
		contractAddress,
		numberOfAccounts,
		numberOfBlocks,
		longPriceFloor,
		longPriceCeiling,
		shortPriceFloor,
		shortPriceCeiling,
		quantityFloor,
		quantityCeiling,
	)
}
