package main

import (
	"context"
	"crypto/ecdsa"
	"encoding/hex"
	"fmt"
	"math/big"
	"math/rand"
	"sync"
	"sync/atomic"
	"time"

	cryptotypes "github.com/cosmos/cosmos-sdk/crypto/types"
	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
)

type EvmTxClient struct {
	privateKey       cryptotypes.PrivKey
	accountAddress   common.Address
	nonce            atomic.Uint64
	shouldResetNonce atomic.Bool
	chainId          *big.Int
	gasPrice         *big.Int
	ethClients       []*ethclient.Client
	mtx              sync.RWMutex
}

func NewEvmTxClient(
	key cryptotypes.PrivKey,
	chainId *big.Int,
	gasPrice *big.Int,
	ethClients []*ethclient.Client,
) *EvmTxClient {
	txClient := &EvmTxClient{
		privateKey: key,
		chainId:    chainId,
		gasPrice:   gasPrice,
		ethClients: ethClients,
		mtx:        sync.RWMutex{},
	}
	privKeyHex := hex.EncodeToString(key.Bytes())
	privateKey, err := crypto.HexToECDSA(privKeyHex)
	if err != nil {
		fmt.Printf("Failed to load private key: %v \n", err)
	}

	publicKey := privateKey.Public()
	publicKeyECDSA, ok := publicKey.(*ecdsa.PublicKey)
	if !ok {
		panic("Cannot assert type: publicKey is not of type *ecdsa.PublicKey \n")
	}

	// Set starting nonce
	fromAddress := crypto.PubkeyToAddress(*publicKeyECDSA)
	nextNonce, err := ethClients[0].PendingNonceAt(context.Background(), fromAddress)
	if err != nil {
		panic(err)
	}
	txClient.nonce.Store(nextNonce)
	txClient.accountAddress = fromAddress
	return txClient
}

// GenerateEvmSignedTx takes a private key and generate a signed bank send TX
//
//nolint:staticcheck
func (txClient *EvmTxClient) GenerateEvmSignedTx() *ethtypes.Transaction {
	txClient.mtx.RLock()
	defer txClient.mtx.RUnlock()

	privKeyHex := hex.EncodeToString(txClient.privateKey.Bytes())
	privateKey, err := crypto.HexToECDSA(privKeyHex)
	if err != nil {
		fmt.Printf("Failed to load private key: %v \n", err)
	}

	// Get the next nonce
	nextNonce := txClient.nonce.Add(1) - 1

	// Generate random amount to send
	rand.Seed(time.Now().Unix())
	value := big.NewInt(rand.Int63n(9000000) * 1000000000000)
	gasLimit := uint64(21000)
	tx := ethtypes.NewTransaction(nextNonce, txClient.accountAddress, value, gasLimit, txClient.gasPrice, nil)
	signedTx, err := ethtypes.SignTx(tx, ethtypes.NewEIP155Signer(txClient.chainId), privateKey)
	if err != nil {
		fmt.Printf("Failed to sign evm tx: %v \n", err)
		return nil
	}
	return signedTx
}

// SendEvmTx takes any signed evm tx and send it out
func (txClient *EvmTxClient) SendEvmTx(signedTx *ethtypes.Transaction, onSuccess func()) {
	err := GetNextEthClient(txClient.ethClients).SendTransaction(context.Background(), signedTx)
	if err != nil {
		fmt.Printf("Failed to send evm transaction: %v \n", err)
	} else {
		// We choose not to GetTxReceipt because we assume the EVM RPC would be running with broadcast mode = block
		onSuccess()
	}
}

// GetNextEthClient return the next available eth client randomly
//
//nolint:staticcheck
func GetNextEthClient(clients []*ethclient.Client) *ethclient.Client {
	numClients := len(clients)
	if numClients <= 0 {
		panic("There's no ETH client available, make sure your connection are valid")
	}
	rand.Seed(time.Now().Unix())
	return clients[rand.Int()%numClients]
}

// GetTxReceipt query the transaction receipt to check if the tx succeed or not
func (txClient *EvmTxClient) GetTxReceipt(txHash common.Hash) error {
	_, err := GetNextEthClient(txClient.ethClients).TransactionReceipt(context.Background(), txHash)
	if err != nil {
		return err
	}
	return nil
}

// ResetNonce need to be called when tx failed
func (txClient *EvmTxClient) ResetNonce() error {
	txClient.mtx.Lock()
	defer txClient.mtx.Unlock()
	client := GetNextEthClient(txClient.ethClients)
	newNonce, err := client.PendingNonceAt(context.Background(), txClient.accountAddress)
	if err != nil {
		return err
	}
	txClient.nonce.Store(newNonce)
	fmt.Printf("Resetting nonce to %d for addr: %s\n ", newNonce, txClient.accountAddress.String())
	return nil
}
<<<<<<< Updated upstream

// nolint
func withRetry(callFunc func() error) (bool, error) {
	retryCount := 0
	for {
		err := callFunc()
		if err != nil {
			retryCount++
			if retryCount >= 15 {
				return false, err
			}
			time.Sleep(300 * time.Millisecond)
			continue
		} else {
			return true, nil
		}
	}
}
=======
>>>>>>> Stashed changes
