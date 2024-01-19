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
	nonceMap map[string]*uint64
	mtx      *sync.Mutex
	client   *ethclient.Client
}

func NewEvmTxSender(client *ethclient.Client) EvmTxSender {
	return EvmTxSender{
		nonceMap: make(map[string]*uint64),
		mtx:      &sync.Mutex{},
		client:   client,
	}
}

func (txSender EvmTxSender) InitializeNonce(key cryptotypes.PrivKey) {
	txSender.mtx.Lock()
	defer txSender.mtx.Unlock()
	if _, ok := txSender.nonceMap[key.String()]; ok {
		return
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

	// Get starting nonce
	fromAddress := crypto.PubkeyToAddress(*publicKeyECDSA)
	nextNonce, err := txSender.client.PendingNonceAt(context.Background(), fromAddress)
	if err != nil {
		panic(err)
	}
	txSender.nonceMap[fromAddress.String()] = &nextNonce
}

func (txSender EvmTxSender) GenerateEvmSignedTx(privKey cryptotypes.PrivKey) *ethtypes.Transaction {
	txSender.InitializeNonce(privKey)
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
	n, _ := txSender.nonceMap[fromAddressStr]
	nextNonce := atomic.AddUint64(n, 1) - 1

	rand.Seed(time.Now().Unix())
	value := big.NewInt(rand.Int63n(math.MaxInt64 - 1))
	gasPrice, err := txSender.client.SuggestGasPrice(context.Background())
	if err != nil {
		fmt.Printf("Failed to suggest gas price: %v \n", err)
		return nil
	}
	gasLimit := uint64(200000)
	tx := ethtypes.NewTransaction(nextNonce, fromAddress, value, gasLimit, gasPrice, nil)
	chainID, err := txSender.client.NetworkID(context.Background())
	if err != nil {
		fmt.Printf("Failed to get chain ID: %v \n", err)
		return nil
	}
	signedTx, err := ethtypes.SignTx(tx, ethtypes.NewEIP155Signer(chainID), privateKey)
	if err != nil {
		fmt.Printf("Failed to sign evm tx: %v \n", err)
		return nil
	}
	return signedTx
}

func (txSender EvmTxSender) SendEvmTx(signedTx *ethtypes.Transaction) bool {
	err := txSender.client.SendTransaction(context.Background(), signedTx)
	if err != nil {
		fmt.Printf("Failed to send evm transaction: %v \n", err)
		return false
	}
	return true
}
