package main

import (
	"context"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	cryptotypes "github.com/cosmos/cosmos-sdk/crypto/types"
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
	for _, mType := range c.LoadTestConfig.MessageTypes {
		if mType == Staking {
			resp, err := c.StakingQueryClient.Validators(context.Background(), &stakingtypes.QueryValidatorsRequest{})
			if err != nil {
				panic(err)
			}
			c.Validators = resp.Validators
		}
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

func (c *LoadTestClient) BuildTxs() (workgroups []*sync.WaitGroup, sendersList [][]func()) {
	config := c.LoadTestConfig
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

	valKeys := c.SignerClient.GetValKeys()

	for i := 0; i < int(config.Rounds); i++ {
		fmt.Printf("Preparing %d-th round\n", i)

		wg := &sync.WaitGroup{}
		var senders []func()
		workgroups = append(workgroups, wg)
		c.generatedAdminMessageForBlock = false
		for j, account := range activeAccounts {
			accountIdentifier := fmt.Sprint(account)
			accountKeyPath := c.SignerClient.GetTestAccountKeyPath(uint64(account))
			key := c.SignerClient.GetKey(accountIdentifier, "test", accountKeyPath)

			msgs, failureExpected, signer, gas, fee := c.generateMessage(config, key, config.MsgsPerTx)
			txBuilder := TestConfig.TxConfig.NewTxBuilder()
			_ = txBuilder.SetMsgs(msgs...)
			seqDelta := uint64(i / 2)
			mode := typestx.BroadcastMode_BROADCAST_MODE_SYNC
			if j == len(activeAccounts)-1 {
				mode = typestx.BroadcastMode_BROADCAST_MODE_BLOCK
			}
			// Note: There is a potential race condition here with seqnos
			// in which a later seqno is delievered before an earlier seqno
			// In practice, we haven't run into this issue so we'll leave this
			// as is.
			sender := SendTx(signer, &txBuilder, mode, seqDelta, failureExpected, *c, gas, fee)
			wg.Add(1)
			senders = append(senders, func() {
				defer wg.Done()
				sender()
			})
		}

		senders = append(senders, c.GenerateOracleSenders(i, config, valKeys, wg)...)

		sendersList = append(sendersList, senders)
		inactiveAccounts, activeAccounts = activeAccounts, inactiveAccounts
	}

	return workgroups, sendersList
}

func (c *LoadTestClient) GenerateOracleSenders(i int, config Config, valKeys []cryptotypes.PrivKey, waitGroup *sync.WaitGroup) []func() {
	senders := []func(){}
	if config.RunOracle && i%2 == 0 {
		for _, valKey := range valKeys {
			// generate oracle tx
			msg := generateOracleMessage(valKey)
			txBuilder := TestConfig.TxConfig.NewTxBuilder()
			_ = txBuilder.SetMsgs(msg)
			seqDelta := uint64(i / 2)
			mode := typestx.BroadcastMode_BROADCAST_MODE_SYNC
			sender := SendTx(valKey, &txBuilder, mode, seqDelta, false, *c, 30000, 100000)
			waitGroup.Add(1)
			senders = append(senders, func() {
				defer waitGroup.Done()
				sender()
			})
		}
	}
	return senders
}

func (c *LoadTestClient) SendTxs(workgroups []*sync.WaitGroup, sendersList [][]func()) {
	defer close(c.TxResponseChan)

	lastHeight := getLastHeight(c.LoadTestConfig.BlockchainEndpoint)
	for i := 0; i < int(c.LoadTestConfig.Rounds); i++ {
		newHeight := getLastHeight(c.LoadTestConfig.BlockchainEndpoint)
		for newHeight == lastHeight {
			time.Sleep(10 * time.Millisecond)
			newHeight = getLastHeight(c.LoadTestConfig.BlockchainEndpoint)
		}
		fmt.Printf("Sending %d-th block\n", i)
		senders := sendersList[i]
		wg := workgroups[i]
		for _, sender := range senders {
			go sender()
		}
		wg.Wait()
		lastHeight = newHeight
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
