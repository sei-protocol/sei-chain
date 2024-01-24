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
		panic("Failed to get chain ID: %v \n")
	}
	txSender.chainId = chainID
	gasPrice, err := client.SuggestGasPrice(context.Background())
	if err != nil {
		panic("Failed to suggest gas price: %v \n")
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
	if err != nil {
		fmt.Printf("Failed to sign evm tx: %v \n", err)
		return nil
	}
	return signedTx
}

// SendEvmTx takes any signed evm tx and send it out
func (txSender *EvmTxSender) SendEvmTx(signedTx *ethtypes.Transaction) bool {
	err := txSender.GetNextClient().SendTransaction(context.Background(), signedTx)
	if err != nil {
		fmt.Printf("Failed to send evm transaction: %v \n", err)
		return false
	}
	return true
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
