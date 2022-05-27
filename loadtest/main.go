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
	TEST_CONFIG  EncodingConfig
	TX_CLIENT    typestx.ServiceClient
	TX_HASH_FILE *os.File
	CHAIN_ID     string
)

const BATCH_SIZE = 100

var FROM_MILI = sdk.NewDec(1000000)

func init() {
	cdc := codec.NewLegacyAmino()
	interfaceRegistry := types.NewInterfaceRegistry()
	marshaler := codec.NewProtoCodec(interfaceRegistry)

	TEST_CONFIG = EncodingConfig{
		InterfaceRegistry: interfaceRegistry,
		Marshaler:         marshaler,
		TxConfig:          tx.NewTxConfig(marshaler, tx.DefaultSignModes),
		Amino:             cdc,
	}
	std.RegisterLegacyAminoCodec(TEST_CONFIG.Amino)
	std.RegisterInterfaces(TEST_CONFIG.InterfaceRegistry)
	app.ModuleBasics.RegisterLegacyAminoCodec(TEST_CONFIG.Amino)
	app.ModuleBasics.RegisterInterfaces(TEST_CONFIG.InterfaceRegistry)
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
	TX_CLIENT = typestx.NewServiceClient(grpcConn)
	userHomeDir, _ := os.UserHomeDir()
	filename := filepath.Join(userHomeDir, "outputs", "test_tx_hash")
	_ = os.Remove(filename)
	file, err := os.OpenFile(filename, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		fmt.Printf("Error opening file %s", err)
		return
	}
	TX_HASH_FILE = file
	var mu sync.Mutex
	var senders []func()

	for i := uint64(0); i < numberOfOrders/BATCH_SIZE; i++ {
		fmt.Println(fmt.Sprintf("Preparing %d-th order", i))
		accountIdx := i % numberOfAccounts
		key := GetKey(accountIdx)
		orderPlacements := []*dextypes.OrderPlacement{}
		longPrice := i%(longPriceCeiling-longPriceFloor) + longPriceFloor
		longQuantity := uint64(rand.Intn(int(quantityCeiling)-int(quantityFloor))) + quantityFloor
		shortPrice := i%(shortPriceCeiling-shortPriceFloor) + shortPriceFloor
		shortQuantity := uint64(rand.Intn(int(quantityCeiling)-int(quantityFloor))) + quantityFloor
		for j := 0; j < BATCH_SIZE; j++ {
			orderPlacements = append(orderPlacements, &dextypes.OrderPlacement{
				Long:       true,
				Price:      longPrice,
				Quantity:   longQuantity,
				PriceDenom: "ust",
				AssetDenom: "luna",
				Open:       true,
				Limit:      true,
				Leverage:   "1",
			}, &dextypes.OrderPlacement{
				Long:       false,
				Price:      shortPrice,
				Quantity:   shortQuantity,
				PriceDenom: "ust",
				AssetDenom: "luna",
				Open:       true,
				Limit:      true,
				Leverage:   "1",
			})
		}
		sendersList = append(sendersList, senders)

		tmp := inactiveAccounts
		inactiveAccounts = activeAccounts
		activeAccounts = tmp
	}

	lastHeight := getLastHeight()
	for i := 0; i < int(numberOfBlocks); i++ {
		newHeight := getLastHeight()
		for newHeight == lastHeight {
			time.Sleep(50 * time.Millisecond)
			newHeight = getLastHeight()
		}
		fmt.Println(fmt.Sprintf("Sending %d-th block", i))

		senders := sendersList[i]
		wg := wgs[i]

		for _, sender := range senders {
			go sender()
		}
		txBuilder := TEST_CONFIG.TxConfig.NewTxBuilder()
		_ = txBuilder.SetMsgs(&msg)
		SignTx(&txBuilder, key)
		sender := SendTx(key, &txBuilder, &mu)
		wg.Add(1)
		senders = append(senders, func() {
			defer wg.Done()
			sender()
		})
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
	chainId := args[9]
	CHAIN_ID = chainId
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
