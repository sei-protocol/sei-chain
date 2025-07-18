package main

import (
	"context"
	"crypto/tls"
	"fmt"
	"math/rand"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/exp/slices"
	"golang.org/x/time/rate"

	cryptotypes "github.com/cosmos/cosmos-sdk/crypto/types"
	"github.com/cosmos/cosmos-sdk/types"
	typestx "github.com/cosmos/cosmos-sdk/types/tx"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/sei-protocol/sei-chain/utils/metrics"
	"golang.org/x/sync/semaphore"
	"google.golang.org/grpc"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/credentials"
)

type LoadTestClient struct {
	LoadTestConfig     Config
	TestConfig         EncodingConfig
	AccountKeys        []cryptotypes.PrivKey
	TxClients          []typestx.ServiceClient
	EvmTxClients       []*EvmTxClient
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
	mtx                   *sync.RWMutex
}

func NewLoadTestClient(config Config) *LoadTestClient {
	signerClient := NewSignerClient(config.NodeURI)
	keys := signerClient.GetTestAccountsKeys(int(config.MaxAccounts))
	txClients, grpcConns := BuildGrpcClients(&config)
	var evmTxClients []*EvmTxClient
	if config.EvmRpcEndpoints != "" {
		if config.ContainsAnyMessageTypes(EVM, ERC20, ERC721, UNIV2) {
			evmTxClients = BuildEvmTxClients(&config, keys)
		}
	}

	// Fill message type maps with empty values
	for _, messageType := range config.MessageTypes {
		producedCountPerMsgType[messageType] = new(int64)
		sentCountPerMsgType[messageType] = new(int64)
		prevSentCounterPerMsgType[messageType] = new(int64)
	}

	return &LoadTestClient{
		LoadTestConfig:                config,
		AccountKeys:                   keys,
		TestConfig:                    TestConfig,
		TxClients:                     txClients,
		EvmTxClients:                  evmTxClients,
		SignerClient:                  signerClient,
		ChainID:                       config.ChainID,
		GrpcConns:                     grpcConns,
		StakingQueryClient:            stakingtypes.NewQueryClient(grpcConns[0]),
		DelegationMap:                 map[string]map[string]int{},
		TokenFactoryDenomOwner:        map[string]string{},
		generatedAdminMessageForBlock: false,
		isAdminMessageMapping:         map[string]bool{CollectRewards: true, DistributeRewards: true},
		mtx:                           &sync.RWMutex{},
	}
}

func (c *LoadTestClient) SetValidators() {
	if slices.Contains(c.LoadTestConfig.MessageTypes, "staking") {
		resp, err := c.StakingQueryClient.Validators(context.Background(), &stakingtypes.QueryValidatorsRequest{})
		if err != nil {
			panic(err)
		}
		c.Validators = resp.Validators
	}
}

// BuildGrpcClients build a list of grpc clients
func BuildGrpcClients(config *Config) ([]typestx.ServiceClient, []*grpc.ClientConn) {
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

// BuildEvmTxClients build a list of EvmTxClients with a list of go-ethereum client
func BuildEvmTxClients(config *Config, keys []cryptotypes.PrivKey) []*EvmTxClient {
	clients := make([]*EvmTxClient, len(keys))
	ethEndpoints := strings.Split(config.EvmRpcEndpoints, ",")
	if len(ethEndpoints) == 0 {
		return clients
	}
	ethClients := make([]*ethclient.Client, len(ethEndpoints))
	for i, endpoint := range ethEndpoints {
		client, err := ethclient.Dial(endpoint)
		if err != nil {
			fmt.Printf("Failed to connect to endpoint %s with error %s", endpoint, err.Error())
		}
		ethClients[i] = client
	}
	// Get chainId
	chainID, err := ethClients[0].NetworkID(context.Background())
	if err != nil {
		panic(fmt.Sprintf("Failed to get chain ID: %v \n", err))
	}
	// Get gas price
	gasPrice, err := ethClients[0].SuggestGasPrice(context.Background())
	if err != nil {
		panic(fmt.Sprintf("Failed to suggest gas price: %v\n", err))
	}
	// Build one client per key
	for i, key := range keys {
		clients[i] = NewEvmTxClient(key, chainID, gasPrice, ethClients, config.EVMAddresses, config.EvmUseEip1559Txs)
	}
	return clients
}

func (c *LoadTestClient) Close() {
	for _, grpcConn := range c.GrpcConns {
		_ = grpcConn.Close()
	}
}

func (c *LoadTestClient) BuildTxs(
	txQueue chan SignedTx,
	keyIndex int,
	wg *sync.WaitGroup,
	done <-chan struct{},
	rateLimiter *rate.Limiter,
	producedCountPerMsgType map[string]*int64,
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
			messageType := c.getRandomMessageType(config.MessageTypes)
			metrics.IncrProducerEventCount(messageType)
			var signedTx SignedTx
			// Sign EVM and Cosmos TX differently
			switch messageType {
			case EVM, ERC20, ERC721, UNIV2,
				DisperseETH:
				signedTx = SignedTx{EvmTx: c.generateSignedEvmTx(keyIndex, messageType), MsgType: messageType}
				EvmTxHashes = append(EvmTxHashes, signedTx.EvmTx.Hash())
			default:
				msgTypeCount := atomic.LoadInt64(producedCountPerMsgType[messageType])
				signedTx = SignedTx{TxBytes: c.generateSignedCosmosTxs(keyIndex, messageType, msgTypeCount), MsgType: messageType}
			}

			select {
			case txQueue <- signedTx:
				atomic.AddInt64(producedCountPerMsgType[messageType], 1)
			case <-done:
				return
			}
		}
	}
}

func (c *LoadTestClient) generateSignedEvmTx(keyIndex int, msgType string) *ethtypes.Transaction {
	return c.EvmTxClients[keyIndex].GetTxForMsgType(msgType)
}

func (c *LoadTestClient) generateSignedCosmosTxs(keyIndex int, msgType string, msgTypeCount int64) []byte {
	key := c.AccountKeys[keyIndex]
	msgs, _, _, gas, fee := c.generateMessage(key, msgType)
	txBuilder := TestConfig.TxConfig.NewTxBuilder()
	_ = txBuilder.SetMsgs(msgs...)
	txBuilder.SetGasLimit(gas)
	txBuilder.SetFeeAmount([]types.Coin{
		types.NewCoin("usei", types.NewInt(fee)),
	})
	// Use random seqno to get around txs that might already be seen in mempool
	c.SignerClient.SignTx(c.ChainID, &txBuilder, key, uint64(msgTypeCount))
	txBytes, _ := TestConfig.TxConfig.TxEncoder()(txBuilder.GetTx())
	return txBytes
}

func (c *LoadTestClient) SendTxs(
	txQueue chan SignedTx,
	keyIndex int,
	done <-chan struct{},
	sentCountPerMsgType map[string]*int64,
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
			atomic.AddInt64(sentCountPerMsgType[tx.MsgType], 1)
			metrics.IncrConsumerEventCount(tx.MsgType)
			if !ok {
				fmt.Printf("Stopping consumers\n")
				return
			}
			// Acquire a semaphore
			if err := semaphore.Acquire(ctx, 1); err != nil {
				fmt.Printf("Failed to acquire semaphore: %v", err)
				break
			}
			if len(tx.TxBytes) > 0 {
				// Send Cosmos Transactions
				if SendTx(ctx, tx.TxBytes, typestx.BroadcastMode_BROADCAST_MODE_BLOCK, *c) {
					atomic.AddInt64(producedCountPerMsgType[tx.MsgType], 1)
				}
			} else if tx.EvmTx != nil {
				// Send EVM Transactions
				c.EvmTxClients[keyIndex].SendEvmTx(tx.EvmTx, func() {
					atomic.AddInt64(producedCountPerMsgType[tx.MsgType], 1)
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
