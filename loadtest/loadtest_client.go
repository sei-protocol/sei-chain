package main

import (
	"context"
	"fmt"
	"golang.org/x/sync/semaphore"
	"math"
	"math/rand"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/time/rate"

	"github.com/cosmos/cosmos-sdk/types"
	typestx "github.com/cosmos/cosmos-sdk/types/tx"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"

	"crypto/tls"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

type LoadTestClient struct {
	LoadTestConfig     Config
	TestConfig         EncodingConfig
	TxClients          []typestx.ServiceClient
	SignerClient       *SignerClient
	ChainID            string
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

	return &LoadTestClient{
		LoadTestConfig:                config,
		TestConfig:                    TestConfig,
		TxClients:                     TxClients,
		SignerClient:                  NewSignerClient(config.NodeURI),
		ChainID:                       config.ChainID,
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

func (c *LoadTestClient) BuildTxs(txQueue chan<- []byte, producerId int, wg *sync.WaitGroup, done <-chan struct{}, producedCount *int64) {
	defer wg.Done()
	config := c.LoadTestConfig
	accountIdentifier := fmt.Sprint(producerId)
	accountKeyPath := c.SignerClient.GetTestAccountKeyPath(uint64(producerId))
	key := c.SignerClient.GetKey(accountIdentifier, "test", accountKeyPath)

	for {
		select {
		case <-done:
			fmt.Printf("Stopping producer %d\n", producerId)
			return
		default:
			msgs, _, _, gas, fee := c.generateMessage(config, key, config.MsgsPerTx)
			txBuilder := TestConfig.TxConfig.NewTxBuilder()
			_ = txBuilder.SetMsgs(msgs...)
			txBuilder.SetGasLimit(gas)
			txBuilder.SetFeeAmount([]types.Coin{
				types.NewCoin("usei", types.NewInt(fee)),
			})
			// Use random seqno to get around txs that might already be seen in mempool

			c.SignerClient.SignTx(c.ChainID, &txBuilder, key, uint64(rand.Intn(math.MaxInt)))
			txBytes, _ := TestConfig.TxConfig.TxEncoder()(txBuilder.GetTx())
			txQueue <- txBytes
			atomic.AddInt64(producedCount, 1)
		}
	}
}

func (c *LoadTestClient) SendTxs(txQueue <-chan []byte, done <-chan struct{}, sentCount *int64, rateLimit int, wg *sync.WaitGroup) {
	rateLimiter := rate.NewLimiter(rate.Limit(rateLimit), rateLimit)
	maxConcurrent := rateLimit // Set the maximum number of concurrent SendTx calls
	sem := semaphore.NewWeighted(int64(maxConcurrent))

	for {
		select {
		case <-done:
			fmt.Printf("Stopping consumers\n")
			wg.Wait()
			return
		case tx, ok := <-txQueue:
			if !ok {
				fmt.Printf("Stopping consumers\n")
				wg.Wait()
				return
			}

			if err := sem.Acquire(context.Background(), 1); err != nil {
				fmt.Printf("Failed to acquire semaphore: %s", err)
				break
			}

			wg.Add(1)
			go func(tx []byte) {
				defer wg.Done()
				defer sem.Release(1)

				if err := rateLimiter.Wait(context.Background()); err == nil {
					SendTx(tx, typestx.BroadcastMode_BROADCAST_MODE_BLOCK, false, *c, sentCount)
				}
			}(tx)
		}
	}
}

func (c *LoadTestClient) GetTxClient() typestx.ServiceClient {
	rand.Seed(time.Now().Unix())
	return c.TxClients[rand.Int()%len(c.TxClients)]
}
