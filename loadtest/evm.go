package main

import (
	"context"
	"crypto/ecdsa"
	"encoding/hex"
	"fmt"
	"math"
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

type EvmTxSender struct {
	nonceMap sync.Map
	chainId  *big.Int
	gasPrice *big.Int
	clients  []*ethclient.Client
}

func NewEvmTxSender(clients []*ethclient.Client) *EvmTxSender {
	return &EvmTxSender{
		nonceMap: sync.Map{},
		clients:  clients,
	}
}

// PrefillNonce is a function to fill starting nonce, this needs to be called at the beginning
func (txSender *EvmTxSender) Setup(keys []cryptotypes.PrivKey) {
	client := txSender.GetNextClient()
	chainID, err := client.NetworkID(context.Background())
	if err != nil {
		panic(fmt.Sprintf("Failed to get chain ID: %v \n", err))
	}
	txSender.chainId = chainID
	gasPrice, err := client.SuggestGasPrice(context.Background())
	if err != nil {
		panic(fmt.Sprintf("Failed to suggest gas price: %v\n", err))
	}
	txSender.gasPrice = gasPrice
	for _, key := range keys {
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

		// Get starting nonce
		fromAddress := crypto.PubkeyToAddress(*publicKeyECDSA)
		nextNonce, err := client.PendingNonceAt(context.Background(), fromAddress)
		if err != nil {
			panic(err)
		}
		txSender.nonceMap.Store(fromAddress.String(), &nextNonce)
	}

}

// GenerateEvmSignedTx takes a private key and generate a signed bank send TX
//
//nolint:staticcheck
func (txSender *EvmTxSender) GenerateEvmSignedTx(privKey cryptotypes.PrivKey) *ethtypes.Transaction {
	privKeyHex := hex.EncodeToString(privKey.Bytes())
	privateKey, err := crypto.HexToECDSA(privKeyHex)
	if err != nil {
		fmt.Printf("Failed to load private key: %v \n", err)
	}
	publicKey := privateKey.Public()
	publicKeyECDSA, ok := publicKey.(*ecdsa.PublicKey)
	if !ok {
		fmt.Printf("Cannot assert type: publicKey is not of type *ecdsa.PublicKey \n")
		return nil
	}
	fromAddress := crypto.PubkeyToAddress(*publicKeyECDSA)
	fromAddressStr := fromAddress.String()
	n, _ := txSender.nonceMap.Load(fromAddressStr)
	nextNonce := atomic.AddUint64(n.(*uint64), 1) - 1
	rand.Seed(time.Now().Unix())
	value := big.NewInt(rand.Int63n(math.MaxInt64 - 1))
	gasLimit := uint64(200000)
	tx := ethtypes.NewTransaction(nextNonce, fromAddress, value, gasLimit, txSender.gasPrice, nil)
	signedTx, err := ethtypes.SignTx(tx, ethtypes.NewEIP155Signer(txSender.chainId), privateKey)
	fmt.Printf("Bank Send from %s with nonce %d and hash %s \n", fromAddressStr, nextNonce, signedTx.Hash().String())
	if err != nil {
		fmt.Printf("Failed to sign evm tx: %v \n", err)
		return nil
	}
	return signedTx
}

// SendEvmTx takes any signed evm tx and send it out
func (txSender *EvmTxSender) SendEvmTx(signedTx *ethtypes.Transaction, onSuccess func()) {
	err := txSender.GetNextClient().SendTransaction(context.Background(), signedTx)
	if err != nil {
		fmt.Printf("Failed to send evm transaction: %v \n", err)
	}
	checkTxSuccessFunc := func() bool {
		return txSender.GetTxReceipt(signedTx.Hash()) == nil
	}
	go func() {
		initialDelay := 1 * time.Second
		maxDelay := 10 * time.Second
		success := exponentialRetry(checkTxSuccessFunc, initialDelay, maxDelay, 2.0)
		if success {
			onSuccess()
		}
	}()

}

// GetNextClient return the next available eth client randomly
//
//nolint:staticcheck
func (txSender *EvmTxSender) GetNextClient() *ethclient.Client {
	numClients := len(txSender.clients)
	if numClients <= 0 {
		panic("There's no ETH client available, make sure your connection are valid")
	}
	rand.Seed(time.Now().Unix())
	return txSender.clients[rand.Int()%numClients]
}

func (txSender *EvmTxSender) GetTxReceipt(txHash common.Hash) *ethtypes.Receipt {
	receipt, err := txSender.GetNextClient().TransactionReceipt(context.Background(), txHash)
	if err != nil {
		fmt.Printf("Failed to get evm transaction receipt for hash %s: %v \n", txHash, err)
		return nil
	}
	fmt.Printf("Got tx receipt for hash %s, block %s \n", receipt.TxHash.String(), receipt.BlockNumber.String())
	return receipt
}

func exponentialRetry(callFunc func() bool, initialDelay time.Duration, totalMaxDelay time.Duration, backoffFactor float64) bool {
	delay := initialDelay
	var totalDelay time.Duration = 0
	for {
		success := callFunc()
		if success {
			return true
		}

		// Check if the next delay will exceed totalMaxDelay.
		if totalDelay+delay > totalMaxDelay {
			return false
		}

		time.Sleep(delay)
		totalDelay += delay

		// Calculate the next delay.
		delay = time.Duration(math.Min(float64(delay)*backoffFactor, float64(totalMaxDelay-totalDelay)))
	}
}
