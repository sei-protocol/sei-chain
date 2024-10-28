package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"math/big"
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
	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"
	"golang.org/x/sync/semaphore"
	"golang.org/x/time/rate"

	"github.com/sei-protocol/sei-chain/app"
	"github.com/sei-protocol/sei-chain/utils/metrics"
	tokenfactorytypes "github.com/sei-protocol/sei-chain/x/tokenfactory/types"
)

var TestConfig EncodingConfig

const (
	VortexData = "{\"position_effect\":\"Open\",\"leverage\":\"1\"}"
)

var (
	FromMili                  = sdk.NewDec(1000000)
	producedCountPerMsgType   = make(map[string]*int64)
	sentCountPerMsgType       = make(map[string]*int64)
	prevSentCounterPerMsgType = make(map[string]*int64)

	BlockHeightsWithTxs = []int{}
	EvmTxHashes         = []common.Hash{}
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
	runEvmQueries(*config)
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
		go client.BuildTxs(txQueues[i], i, &wg, done, producerRateLimiter, producedCountPerMsgType)
		go client.SendTxs(txQueues[i], i, done, sentCountPerMsgType, consumerSemaphore, &wg)
	}

	// Statistics reporting goroutine
	ticker := time.NewTicker(10 * time.Second)
	ticks := 0
	go func() {
		start := time.Now()
		for {
			select {
			case <-ticker.C:
				currHeight := getLastHeight(config.BlockchainEndpoint)
				for i := startHeight; i <= currHeight; i++ {
					txCnt, blockTime, err := getTxBlockInfo(config.BlockchainEndpoint, strconv.Itoa(i))
					if err != nil {
						fmt.Printf("Encountered error scraping data: %s\n", err)
						return
					}
					blockHeights = append(blockHeights, i)
					blockTimes = append(blockTimes, blockTime)
					if txCnt > 0 {
						BlockHeightsWithTxs = append(BlockHeightsWithTxs, i)
					}
				}

				printStats(start, producedCountPerMsgType, sentCountPerMsgType, prevSentCounterPerMsgType, blockHeights, blockTimes)
				startHeight = currHeight
				blockHeights, blockTimes = nil, nil
				start = time.Now()

				for msgType := range sentCountPerMsgType {
					count := atomic.LoadInt64(sentCountPerMsgType[msgType])
					atomic.StoreInt64(prevSentCounterPerMsgType[msgType], count)
				}
				ticks++
				if config.Ticks > 0 && ticks >= int(config.Ticks) {
					close(done)
				}
			case <-done:
				ticker.Stop()
				return
			}
		}
	}()

	// Wait for a termination signal
	if config.Ticks == 0 {
		<-signals
		fmt.Println("SIGINT received, shutting down producers and consumers...")
		close(done)
	}

	fmt.Println("Waiting for wait groups...")

	wg.Wait()
	fmt.Println("Closing channels...")
	for i := range txQueues {
		close(txQueues[i])
	}
}

func printStats(
	startTime time.Time,
	producedCountPerMsgType map[string]*int64,
	sentCountPerMsgType map[string]*int64,
	prevSentPerCounterPerMsgType map[string]*int64,
	blockHeights []int,
	blockTimes []string,
) {
	elapsed := time.Since(startTime)

	totalSent := int64(0)
	totalProduced := int64(0)
	//nolint:gosec
	for msg_type := range sentCountPerMsgType {
		totalSent += atomic.LoadInt64(sentCountPerMsgType[msg_type])
	}
	//nolint:gosec
	for msg_type := range producedCountPerMsgType {
		totalProduced += atomic.LoadInt64(producedCountPerMsgType[msg_type])
	}

	var totalTps float64 = 0
	for msgType := range sentCountPerMsgType {
		sentCount := atomic.LoadInt64(sentCountPerMsgType[msgType])
		prevTotalSent := atomic.LoadInt64(prevSentPerCounterPerMsgType[msgType])
		//nolint:gosec
		tps := float64(sentCount-prevTotalSent) / elapsed.Seconds()
		totalTps += tps
		defer metrics.SetThroughputMetricByType("tps", float32(tps), msgType)
	}

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
		fmt.Printf("High Level - Time Elapsed: %v, Produced: %d, Sent: %d, TPS: %f, Avg Block Time: %d ms\nBlock Heights %v\n\n", elapsed, totalProduced, totalSent, totalTps, avgDuration, blockHeights)
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

	defaultMessageTypeConfig := config.PerMessageConfigs["default"]
	gas := defaultMessageTypeConfig.Gas
	fee := defaultMessageTypeConfig.Fee

	messageTypeConfig, ok := config.PerMessageConfigs[msgType]
	if ok {
		gas = messageTypeConfig.Gas
		fee = messageTypeConfig.Fee
	}

	switch msgType {
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

func runEvmQueries(config Config) {
	ethEndpoints := strings.Split(config.EvmRpcEndpoints, ",")
	if len(ethEndpoints) == 0 {
		return
	}
	ethClients := make([]*ethclient.Client, len(ethEndpoints))
	for i, endpoint := range ethEndpoints {
		client, err := ethclient.Dial(endpoint)
		if err != nil {
			fmt.Printf("Failed to connect to endpoint %s with error %s", endpoint, err.Error())
		}
		ethClients[i] = client
	}
	wg := sync.WaitGroup{}
	start := time.Now()
	for i := 0; i < config.PostTxEvmQueries.BlockByNumber; i++ {
		wg.Add(1)
		i := i
		go func() {
			defer func() { wg.Done() }()
			height := int64(BlockHeightsWithTxs[i%len(BlockHeightsWithTxs)])
			_, err := ethClients[i%len(ethClients)].BlockByNumber(context.Background(), big.NewInt(height))
			if err != nil {
				fmt.Printf("Failed to get full block of height %d due to %s\n", height, err)
			}
		}()
	}
	wg.Wait()
	fmt.Printf("Querying %d blocks in parallel took %fs\n", config.PostTxEvmQueries.BlockByNumber, time.Since(start).Seconds())

	wg = sync.WaitGroup{}
	start = time.Now()
	for i := 0; i < config.PostTxEvmQueries.Receipt; i++ {
		wg.Add(1)
		i := i
		go func() {
			defer func() { wg.Done() }()
			hash := EvmTxHashes[i%len(EvmTxHashes)]
			_, err := ethClients[i%len(ethClients)].TransactionReceipt(context.Background(), hash)
			if err != nil {
				fmt.Printf("Failed to get receipt of tx %s due to %s\n", hash.Hex(), err)
			}
		}()
	}
	wg.Wait()
	fmt.Printf("Querying %d receipts in parallel took %fs\n", config.PostTxEvmQueries.Receipt, time.Since(start).Seconds())

	if config.EVMAddresses.ERC20.Cmp(common.Address{}) != 0 {
		wg = sync.WaitGroup{}
		start = time.Now()
		for i := 0; i < config.PostTxEvmQueries.Filters; i++ {
			wg.Add(1)
			i := i
			go func() {
				defer func() { wg.Done() }()
				_, err := ethClients[i%len(ethClients)].FilterLogs(context.Background(), ethereum.FilterQuery{
					Addresses: []common.Address{config.EVMAddresses.ERC20},
				})
				if err != nil {
					fmt.Printf("Failed to get logs due to %s\n", err)
				}
			}()
		}
		wg.Wait()
		fmt.Printf("Querying %d filter logs in parallel took %fs\n", config.PostTxEvmQueries.Filters, time.Since(start).Seconds())
	}
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
