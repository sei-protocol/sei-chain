package main

import (
	"context"
	"crypto/ecdsa"
	"encoding/hex"
	"fmt"
	"math"
	"math/rand"
	"time"

	cryptotypes "github.com/cosmos/cosmos-sdk/crypto/types"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
	"math/big"
)

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
	}

	fromAddress := crypto.PubkeyToAddress(*publicKeyECDSA)
	nonce, err := client.PendingNonceAt(context.Background(), fromAddress)
	if err != nil {
		fmt.Printf("Failed to get nonce: %v \n", err)
	}
	rand.Seed(time.Now().Unix())
	value := big.NewInt(rand.Int63n(math.MaxInt64))
	gasPrice, err := client.SuggestGasPrice(context.Background())
	if err != nil {
		fmt.Printf("Failed to suggest gas price: %v \n", err)
	}
	gasLimit := uint64(21000)
	tx := ethtypes.NewTransaction(nonce, fromAddress, value, gasLimit, gasPrice, nil)
	chainID, err := client.NetworkID(context.Background())
	if err != nil {
		fmt.Printf("Failed to get chain ID: %v \n", err)
	}
	signedTx, err := ethtypes.SignTx(tx, ethtypes.NewEIP155Signer(chainID), privateKey)
	if err != nil {
		fmt.Printf("Failed to sign evm tx: %v \n", err)
	}
	return signedTx
}

func SendEvmTx(client *ethclient.Client, signedTx *ethtypes.Transaction) {
	err := client.SendTransaction(context.Background(), signedTx)
	if err != nil {
		fmt.Printf("Failed to send evm transaction: %v \n", err)
	}
}
