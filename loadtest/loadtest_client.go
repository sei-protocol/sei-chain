package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	cryptotypes "github.com/cosmos/cosmos-sdk/crypto/types"
	typestx "github.com/cosmos/cosmos-sdk/types/tx"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	"google.golang.org/grpc"
)

type LoadTestClient struct {
	LoadTestConfig     Config
	TestConfig         EncodingConfig
	TxClient           typestx.ServiceClient
	TxHashFile         *os.File
	SignerClient       *SignerClient
	ChainID            string
	TxHashList         []string
	TxHashListMutex    *sync.Mutex
	GrpcConn           *grpc.ClientConn
	StakingQueryClient stakingtypes.QueryClient
	//// Staking specific variables
	Validators []stakingtypes.Validator
	// DelegationMap is a map of delegator -> validator -> delegated amount
	DelegationMap map[string]map[string]int
	//// Tokenfactory specific variables
	TokenFactoryDenomOwner map[string]string
}

func NewLoadTestClient() *LoadTestClient {
	grpcConn, _ := grpc.Dial(
		"127.0.0.1:9090",
		grpc.WithInsecure(),
	)
	TxClient := typestx.NewServiceClient(grpcConn)

	config := Config{}
	pwd, _ := os.Getwd()
	file, _ := os.ReadFile(pwd + "/loadtest/config.json")
	if err := json.Unmarshal(file, &config); err != nil {
		panic(err)
	}

	// setup output files
	userHomeDir, _ := os.UserHomeDir()
	_ = os.Mkdir(filepath.Join(userHomeDir, "outputs"), os.ModePerm)
	filename := filepath.Join(userHomeDir, "outputs", "test_tx_hash")
	_ = os.Remove(filename)
	outputFile, _ := os.OpenFile(filename, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)

	return &LoadTestClient{
		LoadTestConfig:         config,
		TestConfig:             TestConfig,
		TxClient:               TxClient,
		TxHashFile:             outputFile,
		SignerClient:           NewSignerClient(),
		ChainID:                config.ChainID,
		TxHashList:             []string{},
		TxHashListMutex:        &sync.Mutex{},
		GrpcConn:               grpcConn,
		StakingQueryClient:     stakingtypes.NewQueryClient(grpcConn),
		DelegationMap:          map[string]map[string]int{},
		TokenFactoryDenomOwner: map[string]string{},
	}
}

func (c *LoadTestClient) SetValidators() {
	if strings.Contains(c.LoadTestConfig.MessageType, "staking") {
		if resp, err := c.StakingQueryClient.Validators(context.Background(), &stakingtypes.QueryValidatorsRequest{}); err != nil {
			panic(err)
		} else {
			c.Validators = resp.Validators
		}
	}
}

func (c *LoadTestClient) Close() {
	c.GrpcConn.Close()
}

func (c *LoadTestClient) AppendTxHash(txHash string) {
	c.TxHashListMutex.Lock()
	defer c.TxHashListMutex.Unlock()

	c.TxHashList = append(c.TxHashList, txHash)
}

func (c *LoadTestClient) WriteTxHashToFile() {
	file := c.TxHashFile
	for _, txHash := range c.TxHashList {
		txHashLine := fmt.Sprintf("%s\n", txHash)
		if _, err := file.WriteString(txHashLine); err != nil {
			panic(err)
		}
	}
}

func (c *LoadTestClient) BuildTxs() (workgroups []*sync.WaitGroup, sendersList [][]func() string) {
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
		var senders []func() string
		workgroups = append(workgroups, wg)

		for j, account := range activeAccounts {
			key := c.SignerClient.GetKey(uint64(account))

			msg, failureExpected := c.generateMessage(config, key, config.MsgsPerTx)
			txBuilder := TestConfig.TxConfig.NewTxBuilder()
			_ = txBuilder.SetMsgs(msg)
			seqDelta := uint64(i / 2)
			mode := typestx.BroadcastMode_BROADCAST_MODE_SYNC
			if j == len(activeAccounts)-1 {
				mode = typestx.BroadcastMode_BROADCAST_MODE_BLOCK
			}
			// Note: There is a potential race condition here with seqnos
			// in which a later seqno is delievered before an earlier seqno
			// In practice, we haven't run into this issue so we'll leave this
			// as is.
			sender := SendTx(key, &txBuilder, mode, seqDelta, failureExpected, *c)
			wg.Add(1)
			senders = append(senders, func() string {
				defer wg.Done()
				return sender()
			})
		}

		senders = append(senders, c.GenerateOracleSenders(i, config, valKeys, wg)...)

		sendersList = append(sendersList, senders)
		inactiveAccounts, activeAccounts = activeAccounts, inactiveAccounts
	}

	return workgroups, sendersList
}

func (c *LoadTestClient) GenerateOracleSenders(i int, config Config, valKeys []cryptotypes.PrivKey, waitGroup *sync.WaitGroup) []func() string {
	senders := []func() string{}
	if config.RunOracle && i%2 == 0 {
		for _, valKey := range valKeys {
			// generate oracle tx
			msg := generateOracleMessage(valKey)
			txBuilder := TestConfig.TxConfig.NewTxBuilder()
			_ = txBuilder.SetMsgs(msg)
			seqDelta := uint64(i / 2)
			mode := typestx.BroadcastMode_BROADCAST_MODE_SYNC
			sender := SendTx(valKey, &txBuilder, mode, seqDelta, false, *c)
			waitGroup.Add(1)
			senders = append(senders, func() string {
				defer waitGroup.Done()
				return sender()
			})
		}
	}
	return senders
}

func (c *LoadTestClient) SendTxs(workgroups []*sync.WaitGroup, sendersList [][]func() string) {
	lastHeight := getLastHeight()
	for i := 0; i < int(c.LoadTestConfig.Rounds); i++ {
		newHeight := getLastHeight()
		for newHeight == lastHeight {
			time.Sleep(10 * time.Millisecond)
			newHeight = getLastHeight()
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
