package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	cryptotypes "github.com/cosmos/cosmos-sdk/crypto/types"
	typestx "github.com/cosmos/cosmos-sdk/types/tx"
	"google.golang.org/grpc"
)

type LoadTestClient struct {
	LoadTestConfig  Config
	TestConfig      EncodingConfig
	TxClient        typestx.ServiceClient
	SignerClient    *SignerClient
	ChainID         string
	TxHashList      []string
	TxHashListMutex *sync.Mutex
	GrpcConn        *grpc.ClientConn
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

	return &LoadTestClient{
		LoadTestConfig:  config,
		TestConfig:      TestConfig,
		TxClient:        TxClient,
		SignerClient:    NewSignerClient(),
		ChainID:         config.ChainID,
		TxHashList:      []string{},
		TxHashListMutex: &sync.Mutex{},
		GrpcConn:        grpcConn,
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
	userHomeDir, _ := os.UserHomeDir()
	_ = os.Mkdir(filepath.Join(userHomeDir, "outputs"), os.ModePerm)
	filename := filepath.Join(userHomeDir, "outputs", "test_tx_hash")
	_ = os.Remove(filename)
	file, _ := os.OpenFile(filename, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
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
	qv := GetValidators()

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
		if config.MessageType != "none" {
			for _, account := range activeAccounts {
				key := c.SignerClient.GetKey(uint64(account))

				msg := generateMessage(config, key, config.MsgsPerTx, qv.Validators)
				txBuilder := TestConfig.TxConfig.NewTxBuilder()
				_ = txBuilder.SetMsgs(msg)
				seqDelta := uint64(i / 2)
				mode := typestx.BroadcastMode_BROADCAST_MODE_SYNC

				// Note: There is a potential race condition here with seqnos
				// in which a later seqno is delievered before an earlier seqno
				// In practice, we haven't run into this issue so we'll leave this
				// as is.
				sender := SendTx(key, &txBuilder, mode, seqDelta, *c)
				wg.Add(1)
				senders = append(senders, func() {
					defer wg.Done()
					sender()
				})
			}
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
			sender := SendTx(valKey, &txBuilder, mode, seqDelta, *c)
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
