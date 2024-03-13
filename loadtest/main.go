package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	wasmtypes "github.com/CosmWasm/wasmd/x/wasm/types"
	"github.com/cosmos/cosmos-sdk/codec"
	"github.com/cosmos/cosmos-sdk/codec/types"
	cryptotypes "github.com/cosmos/cosmos-sdk/crypto/types"
	"github.com/cosmos/cosmos-sdk/std"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/x/auth/tx"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	distributiontypes "github.com/cosmos/cosmos-sdk/x/distribution/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	"github.com/ethereum/go-ethereum/common"
	"golang.org/x/sync/semaphore"
	"golang.org/x/time/rate"

	"github.com/sei-protocol/sei-chain/app"
	dextypes "github.com/sei-protocol/sei-chain/x/dex/types"
	tokenfactorytypes "github.com/sei-protocol/sei-chain/x/tokenfactory/types"
)

var TestConfig EncodingConfig

const (
	VortexData = "{\"position_effect\":\"Open\",\"leverage\":\"1\"}"
)

var (
	FromMili      = sdk.NewDec(1000000)
	producedCount = atomic.Int64{}
	sentCount     = atomic.Int64{}
	prevSentCount = atomic.Int64{}
)

type BlockData struct {
	Txs []string `json:"txs"`
}

type BlockHeader struct {
	Time string `json:"time"`
}

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
	// Add this so that we don't end up getting disconnected for EVM client
	http.DefaultTransport.(*http.Transport).MaxIdleConnsPerHost = 100
}

// deployEvmContract executes a bash script and returns its output as a string.
//
//nolint:gosec
func deployEvmContract(scriptPath string, config *Config) (common.Address, error) {
	cmd := exec.Command(scriptPath, config.EVMRpcEndpoint())
	var out bytes.Buffer
	cmd.Stdout = &out
	err := cmd.Run()
	if err != nil {
		return common.Address{}, err
	}
	return common.HexToAddress(out.String()), nil
}

func deployEvmContracts(config *Config) {
	config.EVMAddresses = &EVMAddresses{}
	if config.ContainsAnyMessageTypes(ERC20) {
		fmt.Println("Deploying ERC20 contract")
		erc20, err := deployEvmContract("loadtest/contracts/deploy_erc20.sh", config)
		if err != nil {
			fmt.Println("error deploying, make sure 0xF87A299e6bC7bEba58dbBe5a5Aa21d49bCD16D52 is funded")
			panic(err)
		}
		config.EVMAddresses.ERC20 = erc20
	}
	if config.ContainsAnyMessageTypes(ERC721) {
		fmt.Println("Deploying ERC721 contract")
		erc721, err := deployEvmContract("loadtest/contracts/deploy_erc721.sh", config)
		if err != nil {
			fmt.Println("error deploying, make sure 0xF87A299e6bC7bEba58dbBe5a5Aa21d49bCD16D52 is funded")
			panic(err)
		}
		config.EVMAddresses.ERC721 = erc721
	}
}

//nolint:gosec
func deployUniswapContracts(client *LoadTestClient, config *Config) {
	config.EVMAddresses = &EVMAddresses{}
	if config.ContainsAnyMessageTypes(UNIV2) {
		fmt.Println("Deploying Uniswap contracts")
		cmd := exec.Command("loadtest/contracts/deploy_univ2.sh", config.EVMRpcEndpoint())
		var out bytes.Buffer
		cmd.Stdout = &out
		err := cmd.Run()
		fmt.Println("script output: ", out.String())
		if err != nil {
			panic("deploy_univ2.sh failed with error: " + err.Error())
		}
		UniV2SwapperRe := regexp.MustCompile(`Swapper Address: "(\w+)"`)
		match := UniV2SwapperRe.FindStringSubmatch(out.String())
		uniV2SwapperAddress := common.HexToAddress(match[1])
		fmt.Println("Found UniV2Swapper Address: ", uniV2SwapperAddress.String())
		for _, txClient := range client.EvmTxClients {
			txClient.evmAddresses.UniV2Swapper = uniV2SwapperAddress
		}
	}
}

func run(config *Config) {
	// Start metrics collector in another thread
	metricsServer := MetricsServer{}
	go metricsServer.StartMetricsClient(*config)

	client := NewLoadTestClient(*config)
	client.SetValidators()
	deployEvmContracts(config)
	deployUniswapContracts(client, config)
	startLoadtestWorkers(client, *config)
}

// starts loadtest workers. If config.Constant is true, then we don't gather loadtest results and let producer/consumer
// workers continue running. If config.Constant is false, then we will gather load test results in a file
func startLoadtestWorkers(client *LoadTestClient, config Config) {
	fmt.Printf("Starting loadtest workers\n")
	configString, _ := json.Marshal(config)
	fmt.Printf("Running with \n %s \n", string(configString))

	// Catch OS signals for graceful shutdown
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM)

	var blockHeights []int
	var blockTimes []string
	var startHeight = getLastHeight(config.BlockchainEndpoint)
	keys := client.AccountKeys

	// Create producers and consumers
	fmt.Printf("Starting loadtest producers and consumers\n")
	txQueues := make([]chan SignedTx, len(keys))
	for i := range txQueues {
		txQueues[i] = make(chan SignedTx, 10)
	}
	done := make(chan struct{})
	producerRateLimiter := rate.NewLimiter(rate.Limit(config.TargetTps), int(config.TargetTps))
	consumerSemaphore := semaphore.NewWeighted(int64(config.TargetTps))
	var wg sync.WaitGroup
	for i := 0; i < len(keys); i++ {
		go client.BuildTxs(txQueues[i], i, &wg, done, producerRateLimiter, &producedCount)
		go client.SendTxs(txQueues[i], i, done, &sentCount, consumerSemaphore, &wg)
	}

	// Statistics reporting goroutine
	ticker := time.NewTicker(10 * time.Second)
	go func() {
		start := time.Now()
		for {
			select {
			case <-ticker.C:
				currHeight := getLastHeight(config.BlockchainEndpoint)
				for i := startHeight; i <= currHeight; i++ {
					_, blockTime, err := getTxBlockInfo(config.BlockchainEndpoint, strconv.Itoa(i))
					if err != nil {
						fmt.Printf("Encountered error scraping data: %s\n", err)
						return
					}
					blockHeights = append(blockHeights, i)
					blockTimes = append(blockTimes, blockTime)
				}

				totalProduced := producedCount.Load()
				totalSent := sentCount.Load()
				prevTotalSent := prevSentCount.Load()
				printStats(start, totalProduced, totalSent, prevTotalSent, blockHeights, blockTimes)
				startHeight = currHeight
				blockHeights, blockTimes = nil, nil
				start = time.Now()
				prevSentCount.Store(totalSent)
			case <-done:
				ticker.Stop()
				return
			}
		}
	}()

	// Wait for a termination signal
	<-signals
	fmt.Println("SIGINT received, shutting down producers and consumers...")
	close(done)

	fmt.Println("Waiting for wait groups...")

	wg.Wait()
	fmt.Println("Closing channels...")
	for i := range txQueues {
		close(txQueues[i])
	}
}

func printStats(startTime time.Time, totalProduced int64, totalSent int64, prevTotalSent int64, blockHeights []int, blockTimes []string) {
	elapsed := time.Since(startTime)
	tps := float64(totalSent-prevTotalSent) / elapsed.Seconds()

	var totalDuration time.Duration
	var prevTime time.Time
	for i, blockTimeStr := range blockTimes {
		blockTime, _ := time.Parse(time.RFC3339Nano, blockTimeStr)
		if i > 0 {
			duration := blockTime.Sub(prevTime)
			totalDuration += duration
		}
		prevTime = blockTime
	}
	if len(blockTimes)-1 < 1 {

		fmt.Printf("Unable to calculate stats, not enough data. Skipping...\n")
	} else {
		avgDuration := totalDuration.Milliseconds() / int64(len(blockTimes)-1)
		fmt.Printf("High Level - Time Elapsed: %v, Produced: %d, Sent: %d, TPS: %f, Avg Block Time: %d ms\nBlock Heights %v\n\n", elapsed, totalProduced, totalSent, tps, avgDuration, blockHeights)
	}
}

// Generate a random message, only generate one admin message per block to prevent acc seq errors
func (c *LoadTestClient) getRandomMessageType(messageTypes []string) string {
	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	messageType := messageTypes[r.Intn(len(messageTypes))]
	for c.generatedAdminMessageForBlock && c.isAdminMessageMapping[messageType] {
		messageType = messageTypes[r.Intn(len(messageTypes))]
	}

	if c.isAdminMessageMapping[messageType] {
		c.generatedAdminMessageForBlock = true
	}
	return messageType
}

func (c *LoadTestClient) generateMessage(key cryptotypes.PrivKey, msgType string) ([]sdk.Msg, bool, cryptotypes.PrivKey, uint64, int64) {
	var msgs []sdk.Msg
	config := c.LoadTestConfig
	msgPerTx := config.MsgsPerTx
	signer := key

	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	defer IncrTxMessageType(msgType)

	defaultMessageTypeConfig := config.PerMessageConfigs["default"]
	gas := defaultMessageTypeConfig.Gas
	fee := defaultMessageTypeConfig.Fee

	messageTypeConfig, ok := config.PerMessageConfigs[msgType]
	if ok {
		gas = messageTypeConfig.Gas
		fee = messageTypeConfig.Fee
	}

	switch msgType {
	case Vortex:
		price := config.PriceDistr.Sample()
		quantity := config.QuantityDistr.Sample()
		msgs = c.generateVortexOrder(config, key, config.WasmMsgTypes.Vortex.NumOrdersPerTx, price, quantity)
	case WasmMintNft:
		contract := config.WasmMsgTypes.MintNftType.ContractAddr
		// TODO: Potentially just hard code the Funds amount here
		price := config.PriceDistr.Sample()
		quantity := config.QuantityDistr.Sample()
		amount, err := sdk.ParseCoinsNormalized(fmt.Sprintf("%d%s", price.Mul(quantity).Ceil().RoundInt64(), "usei"))
		if err != nil {
			panic(err)
		}
		msgs = []sdk.Msg{&wasmtypes.MsgExecuteContract{
			Sender:   sdk.AccAddress(key.PubKey().Address()).String(),
			Contract: contract,
			Msg:      wasmtypes.RawContractMessage([]byte("{\"mint\":{\"owner\": \"sei1a27kj2j27c6uz58rn9zmhcjee9s3h3nhyhtvjj\"}}")),
			Funds:    amount,
		}}
	case WasmInstantiate:
		msgs = []sdk.Msg{&wasmtypes.MsgInstantiateContract{
			Sender: sdk.AccAddress(key.PubKey().Address()).String(),
			CodeID: config.WasmMsgTypes.Instantiate.CodeID,
			Label:  "test",
			Msg:    wasmtypes.RawContractMessage([]byte(config.WasmMsgTypes.Instantiate.Payload)),
			Funds: sdk.NewCoins(sdk.Coin{
				Denom:  "usei",
				Amount: sdk.NewInt(1),
			}), // maybe make this configurable as well in the future
		}}
	case Bank:
		msgs = []sdk.Msg{}
		for i := 0; i < int(msgPerTx); i++ {

			msgs = append(msgs, &banktypes.MsgSend{
				FromAddress: sdk.AccAddress(key.PubKey().Address()).String(),
				ToAddress:   sdk.AccAddress(key.PubKey().Address()).String(),
				Amount: sdk.NewCoins(sdk.Coin{
					Denom:  "usei",
					Amount: sdk.NewInt(1),
				}),
			})
		}
	case DistributeRewards:
		adminKey := c.SignerClient.GetAdminKey()
		msgs = []sdk.Msg{&banktypes.MsgSend{
			FromAddress: sdk.AccAddress(adminKey.PubKey().Address()).String(),
			ToAddress:   sdk.AccAddress(key.PubKey().Address()).String(),
			Amount: sdk.NewCoins(sdk.Coin{
				Denom:  "usei",
				Amount: sdk.NewInt(10000000),
			}),
		}}
		signer = adminKey
		gas = 10000000
		fee = 1000000
		fmt.Printf("Distribute rewards to %s \n", sdk.AccAddress(key.PubKey().Address()).String())
	case CollectRewards:
		adminKey := c.SignerClient.GetAdminKey()
		delegatorAddr := sdk.AccAddress(adminKey.PubKey().Address())
		operatorAddress := c.Validators[r.Intn(len(c.Validators))].OperatorAddress
		randomValidatorAddr, err := sdk.ValAddressFromBech32(operatorAddress)
		if err != nil {
			panic(err.Error())
		}
		msgs = []sdk.Msg{distributiontypes.NewMsgWithdrawDelegatorReward(
			delegatorAddr,
			randomValidatorAddr,
		)}
		fmt.Printf("Collecting rewards from %s \n", operatorAddress)
		signer = adminKey
		gas = 10000000
		fee = 1000000
	case Dex:
		price := config.PriceDistr.Sample()
		quantity := config.QuantityDistr.Sample()
		contract := config.ContractDistr.Sample()
		orderPlacements := generateDexOrderPlacements(config, key, msgPerTx, price, quantity)
		amount, err := sdk.ParseCoinsNormalized(fmt.Sprintf("%d%s", price.Mul(quantity).Ceil().RoundInt64(), "usei"))
		if err != nil {
			panic(err)
		}
		msgs = []sdk.Msg{&dextypes.MsgPlaceOrders{
			Creator:      sdk.AccAddress(key.PubKey().Address()).String(),
			Orders:       orderPlacements,
			ContractAddr: contract,
			Funds:        amount,
		}}
	case Staking:
		delegatorAddr := sdk.AccAddress(key.PubKey().Address()).String()
		chosenValidator := c.Validators[r.Intn(len(c.Validators))].OperatorAddress
		// Randomly pick someone to redelegate / unbond from
		srcAddr := ""
		c.mtx.RLock()
		for k := range c.DelegationMap[delegatorAddr] {
			if k == chosenValidator {
				continue
			}
			srcAddr = k
			break
		}
		c.mtx.RUnlock()
		msgs = []sdk.Msg{c.generateStakingMsg(delegatorAddr, chosenValidator, srcAddr)}
	case Tokenfactory:
		denomCreatorAddr := sdk.AccAddress(key.PubKey().Address()).String()
		// No denoms, let's mint
		randNum := r.Float64()
		denom, ok := c.TokenFactoryDenomOwner[denomCreatorAddr]
		switch {
		case !ok || randNum <= 0.33:
			subDenom := fmt.Sprintf("tokenfactory-created-denom-%d", time.Now().UnixMilli())
			denom = fmt.Sprintf("factory/%s/%s", denomCreatorAddr, subDenom)
			msgs = []sdk.Msg{&tokenfactorytypes.MsgCreateDenom{
				Sender:   denomCreatorAddr,
				Subdenom: subDenom,
			}}
			c.TokenFactoryDenomOwner[denomCreatorAddr] = denom
		case randNum <= 0.66:
			msgs = []sdk.Msg{&tokenfactorytypes.MsgMint{
				Sender: denomCreatorAddr,
				Amount: sdk.Coin{Denom: denom, Amount: sdk.NewInt(10)},
			}}
		default:
			msgs = []sdk.Msg{&tokenfactorytypes.MsgBurn{
				Sender: denomCreatorAddr,
				Amount: sdk.Coin{Denom: denom, Amount: sdk.NewInt(1)},
			}}
		}
	case FailureBankMalformed:
		var denom string
		if r.Float64() < 0.5 {
			denom = "unknown"
		} else {
			denom = "other"
		}
		msgs = []sdk.Msg{&banktypes.MsgSend{
			FromAddress: sdk.AccAddress(key.PubKey().Address()).String(),
			ToAddress:   sdk.AccAddress(key.PubKey().Address()).String(),
			Amount: sdk.NewCoins(sdk.Coin{
				Denom:  denom,
				Amount: sdk.NewInt(1),
			}),
		}}
	case FailureBankInvalid:
		var amountUsei int64
		amountUsei = 1000000000000000000
		msgs = []sdk.Msg{&banktypes.MsgSend{
			FromAddress: sdk.AccAddress(key.PubKey().Address()).String(),
			ToAddress:   sdk.AccAddress(key.PubKey().Address()).String(),
			Amount: sdk.NewCoins(sdk.Coin{
				Denom:  "usei",
				Amount: sdk.NewInt(amountUsei),
			}),
		}}
	case FailureDexMalformed:
		price := config.PriceDistr.InvalidSample()
		quantity := config.QuantityDistr.InvalidSample()
		contract := config.ContractDistr.Sample()
		orderPlacements := generateDexOrderPlacements(config, key, msgPerTx, price, quantity)
		amount, err := sdk.ParseCoinsNormalized(fmt.Sprintf("%d%s", price.Mul(quantity).Ceil().RoundInt64(), "usei"))
		if err != nil {
			panic(err)
		}
		msgs = []sdk.Msg{&dextypes.MsgPlaceOrders{
			Creator:      sdk.AccAddress(key.PubKey().Address()).String(),
			Orders:       orderPlacements,
			ContractAddr: contract,
			Funds:        amount,
		}}
	case FailureDexInvalid:
		price := config.PriceDistr.Sample()
		quantity := config.QuantityDistr.Sample()
		contract := config.ContractDistr.Sample()
		orderPlacements := generateDexOrderPlacements(config, key, msgPerTx, price, quantity)
		var amountUsei int64
		if r.Float64() < 0.5 {
			amountUsei = 10000 * price.Mul(quantity).Ceil().RoundInt64()
		} else {
			amountUsei = 0
		}
		amount, err := sdk.ParseCoinsNormalized(fmt.Sprintf("%d%s", amountUsei, "usei"))
		if err != nil {
			panic(err)
		}
		msgs = []sdk.Msg{&dextypes.MsgPlaceOrders{
			Creator:      sdk.AccAddress(key.PubKey().Address()).String(),
			Orders:       orderPlacements,
			ContractAddr: contract,
			Funds:        amount,
		}}
	case WasmOccIteratorWrite:
		// generate some values for indices 1-100
		indices := []int{}
		for i := 0; i < 100; i++ {
			indices = append(indices, i)
		}
		rand.Shuffle(100, func(i, j int) {
			indices[i], indices[j] = indices[j], indices[i]
		})
		values := [][]uint64{}
		num_values := rand.Int()%100 + 1
		for x := 0; x < num_values; x++ {
			values = append(values, []uint64{uint64(indices[x]), rand.Uint64() % 12345})
		}
		contract := config.SeiTesterAddress
		msgData := WasmIteratorWriteMsg{
			Values: values,
		}
		jsonData, err := json.Marshal(msgData)
		if err != nil {
			panic(err)
		}
		msgs = []sdk.Msg{&wasmtypes.MsgExecuteContract{
			Sender:   sdk.AccAddress(key.PubKey().Address()).String(),
			Contract: contract,
			Msg:      wasmtypes.RawContractMessage([]byte(fmt.Sprintf("{\"test_occ_iterator_write\":%s}", jsonData))),
		}}
	case WasmOccIteratorRange:
		contract := config.SeiTesterAddress
		start := rand.Uint32() % 100
		end := rand.Uint32() % 100
		if start > end {
			start, end = end, start
		}
		msgs = []sdk.Msg{&wasmtypes.MsgExecuteContract{
			Sender:   sdk.AccAddress(key.PubKey().Address()).String(),
			Contract: contract,
			Msg:      wasmtypes.RawContractMessage([]byte(fmt.Sprintf("{\"test_occ_iterator_range\":{\"start\": %d, \"end\": %d}}", start, end))),
		}}
	case WasmOccParallelWrite:
		contract := config.SeiTesterAddress
		// generate random value
		value := rand.Uint64()
		msgs = []sdk.Msg{&wasmtypes.MsgExecuteContract{
			Sender:   sdk.AccAddress(key.PubKey().Address()).String(),
			Contract: contract,
			Msg:      wasmtypes.RawContractMessage([]byte(fmt.Sprintf("{\"test_occ_parallelism\":{\"value\": %d}}", value))),
		}}
	default:
		fmt.Printf("Unrecognized message type %s", msgType)
	}

	if strings.Contains(msgType, "failure") {
		return msgs, true, signer, gas, int64(fee)
	}
	return msgs, false, signer, gas, int64(fee)
}

func sampleDexOrderType(config Config) (orderType dextypes.OrderType) {
	if len(config.MessageTypes) == 1 && config.MessageTypes[0] == "failure_bank_malformed" {
		orderType = -1
	} else {
		msgType := config.MsgTypeDistr.SampleDexMsgs()
		switch msgType {
		case Limit:
			orderType = dextypes.OrderType_LIMIT
		case Market:
			orderType = dextypes.OrderType_MARKET
		default:
			panic(fmt.Sprintf("Unknown message type %s\n", msgType))
		}
	}
	return orderType
}

func generateDexOrderPlacements(config Config, key cryptotypes.PrivKey, msgPerTx uint64, price sdk.Dec, quantity sdk.Dec) (orderPlacements []*dextypes.Order) {
	orderType := sampleDexOrderType(config)

	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	var direction dextypes.PositionDirection
	if r.Float64() < 0.5 {
		direction = dextypes.PositionDirection_LONG
	} else {
		direction = dextypes.PositionDirection_SHORT
	}

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
	return orderPlacements
}

func (c *LoadTestClient) generateStakingMsg(delegatorAddr string, chosenValidator string, srcAddr string) sdk.Msg {
	c.mtx.Lock()
	defer c.mtx.Unlock()
	// Randomly unbond, redelegate or delegate
	// However, if there are no delegations, do so first
	var msg sdk.Msg
	msgType := c.LoadTestConfig.MsgTypeDistr.SampleStakingMsgs()
	if _, ok := c.DelegationMap[delegatorAddr]; !ok || msgType == "delegate" || srcAddr == "" {
		msg = &stakingtypes.MsgDelegate{
			DelegatorAddress: delegatorAddr,
			ValidatorAddress: chosenValidator,
			Amount:           sdk.Coin{Denom: "usei", Amount: sdk.NewInt(1)},
		}
		c.DelegationMap[delegatorAddr] = map[string]int{}
		c.DelegationMap[delegatorAddr][chosenValidator] = 1
	} else {
		if msgType == "redelegate" {
			msg = &stakingtypes.MsgBeginRedelegate{
				DelegatorAddress:    delegatorAddr,
				ValidatorSrcAddress: srcAddr,
				ValidatorDstAddress: chosenValidator,
				Amount:              sdk.Coin{Denom: "usei", Amount: sdk.NewInt(1)},
			}
			c.DelegationMap[delegatorAddr][chosenValidator]++
		} else {
			msg = &stakingtypes.MsgUndelegate{
				DelegatorAddress: delegatorAddr,
				ValidatorAddress: srcAddr,
				Amount:           sdk.Coin{Denom: "usei", Amount: sdk.NewInt(1)},
			}
		}
		// Update delegation map
		c.DelegationMap[delegatorAddr][srcAddr]--
		if c.DelegationMap[delegatorAddr][srcAddr] == 0 {
			delete(c.DelegationMap, delegatorAddr)
		}
	}
	return msg
}

// generateVortexOrder generates Vortex order messages. If short order, creates a deposit message first
func (c *LoadTestClient) generateVortexOrder(config Config, key cryptotypes.PrivKey, numOrders int64, price sdk.Dec, quantity sdk.Dec) []sdk.Msg {
	var msgs []sdk.Msg
	contract := config.WasmMsgTypes.Vortex.ContractAddr

	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	// Randomly select Position Direction
	var direction dextypes.PositionDirection
	if r.Float64() < 0.5 {
		direction = dextypes.PositionDirection_LONG
	} else {
		direction = dextypes.PositionDirection_SHORT
	}

	orderType := sampleDexOrderType(config)

	// If placing short order on vortex, first deposit for buying power
	if direction == dextypes.PositionDirection_SHORT {
		// TODO: Considering depositing more up front when numOrders > 1
		amountDeposit, err := sdk.ParseCoinsNormalized(fmt.Sprintf("%d%s", price.Mul(quantity).Ceil().RoundInt64(), "usei"))
		if err != nil {
			panic(err)
		}
		vortexDeposit := &wasmtypes.MsgExecuteContract{
			Sender:   sdk.AccAddress(key.PubKey().Address()).String(),
			Contract: contract,
			Msg:      wasmtypes.RawContractMessage([]byte("{\"deposit\":{}}")),
			Funds:    amountDeposit,
		}
		msgs = append(msgs, vortexDeposit)
	}

	// Create a MsgPlaceOrders with numOrders Orders
	var orderPlacements []*dextypes.Order
	for j := 0; j < int(numOrders); j++ {
		vortexOrder := &dextypes.Order{
			Account:           sdk.AccAddress(key.PubKey().Address()).String(),
			ContractAddr:      contract,
			PositionDirection: direction,
			Price:             price.Quo(FromMili),
			Quantity:          quantity.Quo(FromMili),
			PriceDenom:        "SEI",
			AssetDenom:        "ATOM",
			OrderType:         orderType,
			Data:              VortexData,
		}
		orderPlacements = append(orderPlacements, vortexOrder)
	}

	amount, err := sdk.ParseCoinsNormalized(fmt.Sprintf("%d%s", price.Mul(quantity).Ceil().RoundInt64(), "usei"))
	if err != nil {
		panic(err)
	}
	vortexOrderMsg := &dextypes.MsgPlaceOrders{
		Creator:      sdk.AccAddress(key.PubKey().Address()).String(),
		Orders:       orderPlacements,
		ContractAddr: contract,
		Funds:        amount,
	}

	msgs = append(msgs, vortexOrderMsg)

	return msgs
}

// nolint
func getLastHeight(blockchainEndpoint string) int {
	out, err := exec.Command("curl", blockchainEndpoint+"/blockchain").Output()
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

func getTxBlockInfo(blockchainEndpoint string, height string) (int, string, error) {

	resp, err := http.Get(blockchainEndpoint + "/block?height=" + height)
	if err != nil {
		fmt.Printf("Error query block data: %s\n", err)
		return 0, "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		fmt.Printf("Error reading block data: %s\n", err)
		return 0, "", err
	}

	var blockResponse struct {
		Block struct {
			Header BlockHeader `json:"header"`
			Data   BlockData   `json:"data"`
		} `json:"block"`
	}
	err = json.Unmarshal(body, &blockResponse)
	if err != nil {
		fmt.Printf("Error reading block data: %s\n", err)
		return 0, "", err
	}

	return len(blockResponse.Block.Data.Txs), blockResponse.Block.Header.Time, nil
}

func GetDefaultConfigFilePath() string {
	pwd, _ := os.Getwd()
	return pwd + "/loadtest/config.json"
}

func ReadConfig(path string) Config {
	config := Config{}
	file, _ := os.ReadFile(path)
	if err := json.Unmarshal(file, &config); err != nil {
		panic(err)
	}
	return config
}

func main() {
	configFilePath := flag.String("config-file", GetDefaultConfigFilePath(), "Path to the config.json file to use for this run")
	flag.Parse()

	config := ReadConfig(*configFilePath)
	fmt.Printf("Using config file: %s\n", *configFilePath)
	run(&config)
}
