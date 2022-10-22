package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"

	typestx "github.com/cosmos/cosmos-sdk/types/tx"
	"google.golang.org/grpc"
)

type LoadTestClient struct {
	TestConfig 	EncodingConfig
	TxClient   typestx.ServiceClient
	ChainID    string
	TxHashList []string
	TxHashListMutex *sync.Mutex
	GrpcConn *grpc.ClientConn
}

func NewLoadTestClient(chainId string) *LoadTestClient {
	grpcConn, _ := grpc.Dial(
		"127.0.0.1:9090",
		grpc.WithInsecure(),
	)
	TxClient := typestx.NewServiceClient(grpcConn)

	return &LoadTestClient{
		TestConfig: TestConfig,
		TxClient: TxClient,
		ChainID: chainId,
		TxHashList: []string{},
		TxHashListMutex: &sync.Mutex{},
		GrpcConn: grpcConn,
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


func (c *LoadTestClient) WriteTxHashToFile(txHash string) {
	userHomeDir, _ := os.UserHomeDir()
	_ = os.Mkdir(filepath.Join(userHomeDir, "outputs"), os.ModePerm)
	filename := filepath.Join(userHomeDir, "outputs", "test_tx_hash")
	_ = os.Remove(filename)
	file, _ := os.OpenFile(filename, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	for txHash := range c.TxHashList {
		if _, err := file.WriteString(fmt.Sprintf("%s\n", txHash)); err != nil {
			panic(err)
		}
	}

}
