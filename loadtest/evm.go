package main

import (
	"context"
	"crypto/ecdsa"
	"encoding/hex"
	"fmt"
	"math"
	"math/rand"
	"sync"
	"sync/atomic"
	"time"

	"math/big"

	cryptotypes "github.com/cosmos/cosmos-sdk/crypto/types"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
)

var nonce_cache = sync.Map{}

func GenerateEvmSignedTx(client *ethclient.Client, privKey cryptotypes.PrivKey) *ethtypes.Transaction {
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
	n, _ := nonce_cache.Load(fromAddressStr)
	nextNonce := atomic.AddUint64(n.(*uint64), 1) - 1

	rand.Seed(time.Now().Unix())
	value := big.NewInt(rand.Int63n(math.MaxInt64 - 1))
	gasPrice, err := client.SuggestGasPrice(context.Background())
	if err != nil {
		fmt.Printf("Failed to suggest gas price: %v \n", err)
		return nil
	}
	gasLimit := uint64(200000)
	tx := ethtypes.NewTransaction(nextNonce, fromAddress, value, gasLimit, gasPrice, nil)
	chainID, err := client.NetworkID(context.Background())
	if err != nil {
		fmt.Printf("Failed to get chain ID: %v \n", err)
		return nil
	}
	signedTx, err := ethtypes.SignTx(tx, ethtypes.NewEIP155Signer(chainID), privateKey)
	if err != nil {
		fmt.Printf("Failed to sign evm tx: %v \n", err)
		return nil
	}
	//fmt.Printf("Created new signed transaction with nonce %d, address %s and hash %s\n", signedTx.Nonce(), fromAddressStr, signedTx.Hash())
	return signedTx
}

func SendEvmTx(client *ethclient.Client, signedTx *ethtypes.Transaction) bool {
	err := client.SendTransaction(context.Background(), signedTx)
	if err != nil {
		fmt.Printf("Failed to send evm transaction with nonce %d and hash %s: %v \n", signedTx.Nonce(), signedTx.Hash(), err)
		return false
	}
	//fmt.Printf("Successfully sent evm transaction: %v \n", signedTx.Hash())
	return true
}
