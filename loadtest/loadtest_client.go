package main

import (
	"context"
	"crypto/tls"
	"fmt"
	"golang.org/x/time/rate"
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
	"google.golang.org/grpc"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/credentials"
)

type LoadTestClient struct {
	LoadTestConfig     Config
	TestConfig         EncodingConfig
	TxClients          []typestx.ServiceClient
	EvmTxSender        *EvmTxSender
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
	evnTxSender := BuildEvmTxSender(config)

	return &LoadTestClient{
		LoadTestConfig:                config,
		TestConfig:                    TestConfig,
		TxClients:                     txClients,
		EvmTxSender:                   evnTxSender,
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

// BuildEvmTxSender build a with EvmTxSender with a list of go-ethereum client
func BuildEvmTxSender(config Config) *EvmTxSender {
	ethEndpoints := strings.Split(config.EvmRpcEndpoints, ",")
	ethClients := make([]*ethclient.Client, len(ethEndpoints))
	for i, endpoint := range ethEndpoints {
		client, err := ethclient.Dial(endpoint)
		if err != nil {
			fmt.Printf("Failed to connect to endpoint %s with error %s", endpoint, err.Error())
		}
		ethClients[i] = client
	}
	return NewEvmTxSender(ethClients)
}

func (c *LoadTestClient) Close() {
	for _, grpcConn := range c.GrpcConns {
		_ = grpcConn.Close()
	}
}

func (c *LoadTestClient) BuildTxs(
	txQueue chan SignedTx,
	key cryptotypes.PrivKey,
	wg *sync.WaitGroup,
	done <-chan struct{},
	rateLimiter *rate.Limiter,
	producedCount *atomic.Int64,
) {
	wg.Add(1)
	defer wg.Done()
	config := c.LoadTestConfig
	for {
		select {
		case <-done:
			return
		default:
			if !rateLimiter.Allow() {
				continue
			}
			// Generate a message type first
			messageTypes := strings.Split(config.MessageType, ",")
			messageType := c.getRandomMessageType(messageTypes)
			signedTx := SignedTx{}
			// Sign EVM and Cosmos TX differently
			if messageType == EVM {
				tx := c.generatedSignedEvmTxs(key)
				if tx != nil {
					signedTx = SignedTx{EvmTx: tx}
				} else {
					continue
				}
			} else {
				signedTx = SignedTx{TxBytes: c.generateSignedCosmosTxs(key, messageType)}
			}
			select {
			case txQueue <- signedTx:
				producedCount.Add(1)
			case <-done:
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
	return c.EvmTxSender.GenerateEvmSignedTx(key)
}

func (c *LoadTestClient) SendTxs(
	txQueue chan SignedTx,
	done <-chan struct{},
	sentCount *atomic.Int64,
	semaphore *semaphore.Weighted,
	wg *sync.WaitGroup,
) {
	wg.Add(1)
	defer wg.Done()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	for {
		select {
		case <-done:
			return
		case tx, ok := <-txQueue:
			if !ok {
				fmt.Printf("Stopping consumers\n")
				return
			}
			// Acquire a semaphore
			if err := semaphore.Acquire(ctx, 1); err != nil {
				fmt.Printf("Failed to acquire semaphore: %v", err)
				break
			}
			if tx.TxBytes != nil && len(tx.TxBytes) > 0 {
				// Send Cosmos Transactions
				if SendTx(ctx, tx.TxBytes, typestx.BroadcastMode_BROADCAST_MODE_BLOCK, *c) {
					sentCount.Add(1)
				}
			} else if tx.EvmTx != nil {
				// Send EVM Transactions
				c.EvmTxSender.SendEvmTx(tx.EvmTx, func() {
					sentCount.Add(1)
				})
			}
			// Release the semaphore
			semaphore.Release(1)
		}
	}
}

//nolint:staticcheck
func (c *LoadTestClient) GetTxClient() typestx.ServiceClient {
	numClients := len(c.TxClients)
	if numClients <= 0 {
		panic("There's no Tx client available, make sure your connection are valid")
	}
	rand.Seed(time.Now().Unix())
	return c.TxClients[rand.Int()%len(c.TxClients)]
}
