package main

import (
	"context"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	typestx "github.com/cosmos/cosmos-sdk/types/tx"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"

	"crypto/tls"

	"github.com/k0kubun/pp/v3"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

type LoadTestClient struct {
	LoadTestConfig     Config
	TestConfig         EncodingConfig
	TxClients          []typestx.ServiceClient
	TxHashFile         *os.File
	SignerClient       *SignerClient
	ChainID            string
	TxHashList         []string
	TxResponseChan     chan *string
	TxHashListMutex    *sync.Mutex
	GrpcConns          []*grpc.ClientConn
	StakingQueryClient stakingtypes.QueryClient
	// Staking specific variables
	Validators []stakingtypes.Validator
	// DelegationMap is a map of delegator -> validator -> delegated amount
	DelegationMap map[string]map[string]int
	// Tokenfactory specific variables
	TokenFactoryDenomOwner map[string]string
	// Only one admin message can go in per block
	generatedAdminMessageForBlock bool
	// Messages that has to be sent from the admin account
	isAdminMessageMapping map[string]bool
}

func NewLoadTestClient(config Config) *LoadTestClient {
	var dialOptions []grpc.DialOption

	// NOTE: Will likely need to whitelist node from elb rate limits - add ip to producer ip set
	if config.TLS {
		dialOptions = append(dialOptions, grpc.WithTransportCredentials(credentials.NewTLS(&tls.Config{InsecureSkipVerify: true}))) //nolint:gosec // Use insecure skip verify.
	} else {
		dialOptions = append(dialOptions, grpc.WithInsecure())
	}
	endpoints := strings.Split(config.GrpcEndpoints, ",")
	TxClients := make([]typestx.ServiceClient, len(endpoints))
	GrpcConns := make([]*grpc.ClientConn, len(endpoints))
	for i, endpoint := range endpoints {
		grpcConn, _ := grpc.Dial(
			endpoint,
			dialOptions...)
		TxClients[i] = typestx.NewServiceClient(grpcConn)
		GrpcConns[i] = grpcConn
	}

	// setup output files
	userHomeDir, _ := os.UserHomeDir()
	_ = os.Mkdir(filepath.Join(userHomeDir, "outputs"), os.ModePerm)
	filename := filepath.Join(userHomeDir, "outputs", "test_tx_hash")
	_ = os.Remove(filename)
	outputFile, _ := os.OpenFile(filename, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	return &LoadTestClient{
		LoadTestConfig:                config,
		TestConfig:                    TestConfig,
		TxClients:                     TxClients,
		TxHashFile:                    outputFile,
		SignerClient:                  NewSignerClient(config.NodeURI),
		ChainID:                       config.ChainID,
		TxHashList:                    []string{},
		TxResponseChan:                make(chan *string),
		TxHashListMutex:               &sync.Mutex{},
		GrpcConns:                     GrpcConns,
		StakingQueryClient:            stakingtypes.NewQueryClient(GrpcConns[0]),
		DelegationMap:                 map[string]map[string]int{},
		TokenFactoryDenomOwner:        map[string]string{},
		generatedAdminMessageForBlock: false,
		isAdminMessageMapping:         map[string]bool{CollectRewards: true, DistributeRewards: true},
	}
}

func (c *LoadTestClient) SetValidators() {
	if strings.Contains(c.LoadTestConfig.MessageType, "staking") {
		resp, err := c.StakingQueryClient.Validators(context.Background(), &stakingtypes.QueryValidatorsRequest{})
		if err != nil {
			panic(err)
		}
		c.Validators = resp.Validators
	}
}

func (c *LoadTestClient) Close() {
	for _, grpcConn := range c.GrpcConns {
		_ = grpcConn.Close()
	}
}

func (c *LoadTestClient) AppendTxHash(txHash string) {
	c.TxResponseChan <- &txHash
}

func (c *LoadTestClient) WriteTxHashToFile() {
	fmt.Printf("Writing Tx Hashes to: %s \n", c.TxHashFile.Name())
	file := c.TxHashFile
	for _, txHash := range c.TxHashList {
		txHashLine := fmt.Sprintf("%s\n", txHash)
		if _, err := file.WriteString(txHashLine); err != nil {
			panic(err)
		}
	}
}

func (c *LoadTestClient) BuildTxs(txQueue chan<- []byte, producerId int, numTxsPerProducerPerSecond int, wg *sync.WaitGroup, done <-chan struct{}, producedCount *int64) {
	defer wg.Done()
	ticker := time.NewTicker(1 * time.Second) // Fires every 100ms
	config := c.LoadTestConfig
	accountIdentifier := fmt.Sprint(producerId)
	accountKeyPath := c.SignerClient.GetTestAccountKeyPath(uint64(producerId))
	key := c.SignerClient.GetKey(accountIdentifier, "test", accountKeyPath)
	count := 0
	for {
		select {
		case <-done:
			fmt.Printf("Stopping producer %d\n", producerId)
			return
		case <-ticker.C:
			count = 0
		default:
			if count < numTxsPerProducerPerSecond {
				msgs, _, _, gas, fee := c.generateMessage(config, key, config.MsgsPerTx)
				txBuilder := TestConfig.TxConfig.NewTxBuilder()
				_ = txBuilder.SetMsgs(msgs...)
				txBuilder.SetGasLimit(gas)
				txBuilder.SetFeeAmount([]types.Coin{
					types.NewCoin("usei", types.NewInt(fee)),
				})
				// Use random seqno to get around txs that might already be seen in mempool

				c.SignerClient.SignTx(c.ChainID, &txBuilder, key, uint64(time.Now().Unix()+int64(count)))
				txBytes, _ := TestConfig.TxConfig.TxEncoder()(txBuilder.GetTx())
				txQueue <- txBytes
				atomic.AddInt64(producedCount, 1)
				count++
			}
		}
	}
}

func (c *LoadTestClient) SendTxs(txQueue <-chan []byte, consumerId int, wg *sync.WaitGroup, done <-chan struct{}, sentCount *int64) {
	defer wg.Done()

	for {
		select {
		case <-done:
			fmt.Printf("Stopping consumer %d\n", consumerId)
			return
		case tx, ok := <-txQueue:
			if !ok {
				fmt.Printf("Stopping consumer %d\n", consumerId)
			}
			SendTx(tx, typestx.BroadcastMode_BROADCAST_MODE_SYNC, false, *c, c.LoadTestConfig.Constant, sentCount)
		}
	}
}

func (c *LoadTestClient) GatherTxHashes() {
	for txHash := range c.TxResponseChan {
		c.TxHashList = append(c.TxHashList, *txHash)
	}
	fmt.Printf("Transactions Sent=%d\n", len(c.TxHashList))
}

func (c *LoadTestClient) ValidateTxs() {
	defer c.Close()
	numTxs := len(c.TxHashList)
	resultChan := make(chan *types.TxResponse, numTxs)
	var waitGroup sync.WaitGroup

	if numTxs == 0 {
		return
	}

	for _, txHash := range c.TxHashList {
		waitGroup.Add(1)
		go func(txHash string) {
			defer waitGroup.Done()
			resultChan <- c.GetTxResponse(txHash)
		}(txHash)
	}

	go func() {
		waitGroup.Wait()
		close(resultChan)
	}()

	fmt.Printf("Validating %d Transactions... \n", len(c.TxHashList))
	waitGroup.Wait()

	notCommittedTxs := 0
	responseCodeMap := map[int]int{}
	responseStringMap := map[string]int{}
	for result := range resultChan {
		// If the result is nil then that means the transaction was not committed
		if result == nil {
			notCommittedTxs++
			continue
		}
		code := result.Code
		codeString := "ok"
		if code != 0 {
			codespace := result.Codespace
			err := sdkerrors.ABCIError(codespace, code, fmt.Sprintf("Error code=%d ", code))
			codeString = err.Error()
		}
		responseStringMap[codeString]++
		responseCodeMap[int(code)]++
	}

	fmt.Printf("Transactions not committed: %d\n", notCommittedTxs)
	pp.Printf("Response Code Mapping: \n %s \n", responseStringMap)
	IncrTxNotCommitted(notCommittedTxs)
	for reason, count := range responseStringMap {
		IncrTxProcessCode(reason, count)
	}
}

func (c *LoadTestClient) GetTxResponse(hash string) *types.TxResponse {
	grpcRes, err := c.GetTxClient().GetTx(
		context.Background(),
		&typestx.GetTxRequest{
			Hash: hash,
		},
	)
	fmt.Printf("Validated: %s\n", hash)
	if err != nil {
		fmt.Println(err)
		return nil
	}
	return grpcRes.TxResponse
}

func (c *LoadTestClient) GetTxClient() typestx.ServiceClient {
	rand.Seed(time.Now().Unix())
	return c.TxClients[rand.Int()%len(c.TxClients)]
}
