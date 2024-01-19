package main

import (
	"context"
	"crypto/tls"
	"fmt"
	"math"
	"math/rand"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	cryptotypes "github.com/cosmos/cosmos-sdk/crypto/types"
	"github.com/cosmos/cosmos-sdk/types"
	typestx "github.com/cosmos/cosmos-sdk/types/tx"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
	"golang.org/x/sync/semaphore"
	"golang.org/x/time/rate"
	"google.golang.org/grpc"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/credentials"
)

type LoadTestClient struct {
	LoadTestConfig     Config
	TestConfig         EncodingConfig
	TxClients          []typestx.ServiceClient
	EthClients         []*ethclient.Client
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
	txClients, grpcConns := BuildGrpcClients(config)
	ethClients := BuildEvmRpcClients(config)

	return &LoadTestClient{
		LoadTestConfig:                config,
		TestConfig:                    TestConfig,
		TxClients:                     txClients,
		EthClients:                    ethClients,
		SignerClient:                  NewSignerClient(config.NodeURI),
		ChainID:                       config.ChainID,
		GrpcConns:                     grpcConns,
		StakingQueryClient:            stakingtypes.NewQueryClient(grpcConns[0]),
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

// BuildGrpcClients build a list of grpc clients
func BuildGrpcClients(config Config) ([]typestx.ServiceClient, []*grpc.ClientConn) {
	grpcEndpoints := strings.Split(config.GrpcEndpoints, ",")
	txClients := make([]typestx.ServiceClient, len(grpcEndpoints))
	grpcConns := make([]*grpc.ClientConn, len(grpcEndpoints))
	var dialOptions []grpc.DialOption
	dialOptions = append(dialOptions, grpc.WithDefaultCallOptions(
		grpc.MaxCallRecvMsgSize(20*1024*1024),
		grpc.MaxCallSendMsgSize(20*1024*1024)),
	)
	dialOptions = append(dialOptions, grpc.WithBlock())
	if config.TLS {
		dialOptions = append(dialOptions, grpc.WithTransportCredentials(credentials.NewTLS(&tls.Config{InsecureSkipVerify: true}))) //nolint:gosec // Use insecure skip verify.
	} else {
		dialOptions = append(dialOptions, grpc.WithInsecure())
	}
	for i, endpoint := range grpcEndpoints {
		grpcConn, _ := grpc.Dial(
			endpoint,
			dialOptions...)
		txClients[i] = typestx.NewServiceClient(grpcConn)
		grpcConns[i] = grpcConn
		// spin up goroutine for monitoring and reconnect purposes
		go func() {
			for {
				state := grpcConn.GetState()
				if state == connectivity.TransientFailure || state == connectivity.Shutdown {
					fmt.Println("GRPC Connection lost, attempting to reconnect...")
					for {
						if grpcConn.WaitForStateChange(context.Background(), state) {
							break
						}
						time.Sleep(10 * time.Second)
					}
				}
				time.Sleep(10 * time.Second)
			}
		}()
	}
	return txClients, grpcConns
}

// BuildEvmRpcClients build a list of EVM RPC clients using go-ethereum client
func BuildEvmRpcClients(config Config) []*ethclient.Client {
	ethEndpoints := strings.Split(config.EvmRpcEndpoints, ",")
	ethClients := make([]*ethclient.Client, len(ethEndpoints))
	for i, endpoint := range ethEndpoints {
		client, err := ethclient.Dial(endpoint)
		if err != nil {
			fmt.Printf("Failed to connect to endpoint %s with error %s", endpoint, err.Error())
		}
		ethClients[i] = client
	}
	return ethClients
}

func (c *LoadTestClient) Close() {
	for _, grpcConn := range c.GrpcConns {
		_ = grpcConn.Close()
	}
}

func (c *LoadTestClient) BuildTxs(
	txQueue chan<- SignedTx,
	producerId int,
	keys []cryptotypes.PrivKey,
	wg *sync.WaitGroup,
	done <-chan struct{},
	producedCount *atomic.Int64,
) {
	defer wg.Done()
	config := c.LoadTestConfig

	for {
		select {
		case <-done:
			fmt.Printf("Stopping producer %d\n", producerId)
			return
		default:
			nextKey := keys[producedCount.Load()%int64(len(keys))]
			// Generate a message type first
			messageTypes := strings.Split(config.MessageType, ",")
			messageType := c.getRandomMessageType(messageTypes)
			signedTx := SignedTx{}
			// Sign EVM and Cosmos TX differently
			if messageType == EVM {
				signedTx = SignedTx{EvmTx: c.generatedSignedEvmTxs(nextKey)}
			} else {
				signedTx = SignedTx{TxBytes: c.generateSignedCosmosTxs(nextKey, messageType)}
			}
			select {
			case txQueue <- signedTx:
				producedCount.Add(1)
			case <-done:
				// Exit if done signal is received while trying to send to txQueue
				return
			}
		}
	}
}

func (c *LoadTestClient) generateSignedCosmosTxs(key cryptotypes.PrivKey, msgType string) []byte {
	msgs, _, _, gas, fee := c.generateMessage(key, msgType)
	txBuilder := TestConfig.TxConfig.NewTxBuilder()
	_ = txBuilder.SetMsgs(msgs...)
	txBuilder.SetGasLimit(gas)
	txBuilder.SetFeeAmount([]types.Coin{
		types.NewCoin("usei", types.NewInt(fee)),
	})
	// Use random seqno to get around txs that might already be seen in mempool
	c.SignerClient.SignTx(c.ChainID, &txBuilder, key, uint64(rand.Intn(math.MaxInt)))
	txBytes, _ := TestConfig.TxConfig.TxEncoder()(txBuilder.GetTx())
	return txBytes
}

func (c *LoadTestClient) generatedSignedEvmTxs(key cryptotypes.PrivKey) *ethtypes.Transaction {
	return GenerateEvmSignedTx(c.GetEthClient(), key)
}

func (c *LoadTestClient) SendTxs(
	txQueue <-chan SignedTx,
	done <-chan struct{},
	sentCount *atomic.Int64,
	rateLimit int,
	wg *sync.WaitGroup,
) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	rateLimiter := rate.NewLimiter(rate.Limit(rateLimit), rateLimit)
	maxConcurrent := rateLimit // Set the maximum number of concurrent SendTx calls
	sem := semaphore.NewWeighted(int64(maxConcurrent))

	for {
		select {
		case <-done:
			fmt.Printf("Stopping consumers\n")
			return
		case tx, ok := <-txQueue:
			if !ok {
				fmt.Printf("Stopping consumers\n")
				return
			}

			if err := sem.Acquire(ctx, 1); err != nil {
				fmt.Printf("Failed to acquire semaphore: %v", err)
				break
			}
			wg.Add(1)
			go func(tx SignedTx) {
				localCtx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
				defer cancel()
				defer wg.Done()
				defer sem.Release(1)

				if err := rateLimiter.Wait(localCtx); err != nil {
					return
				}
				if tx.TxBytes != nil && len(tx.TxBytes) > 0 {
					// Send Cosmos Transactions
					SendTx(ctx, tx.TxBytes, typestx.BroadcastMode_BROADCAST_MODE_BLOCK, *c)
					sentCount.Add(1)
				} else if tx.EvmTx != nil {
					// Send EVM Transactions
					SendEvmTx(c.GetEthClient(), tx.EvmTx)
					sentCount.Add(1)
				}
			}(tx)
		}
	}
}

//nolint:staticcheck
func (c *LoadTestClient) GetTxClient() typestx.ServiceClient {
	numClients := len(c.TxClients)
	if numClients <= 0 {
		return nil
	}
	rand.Seed(time.Now().Unix())
	return c.TxClients[rand.Int()%len(c.TxClients)]
}

//nolint:staticcheck
func (c *LoadTestClient) GetEthClient() *ethclient.Client {
	numClients := len(c.EthClients)
	if numClients <= 0 {
		return nil
	}
	rand.Seed(time.Now().Unix())
	return c.EthClients[rand.Int()%numClients]
}
