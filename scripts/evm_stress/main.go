package main

import (
	"context"
	"crypto/ecdsa"
	"flag"
	"fmt"
	"math/big"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"

	seibech32 "github.com/sei-protocol/sei-chain/sei-cosmos/types/bech32"
)

const (
	evmRPC     = "http://127.0.0.1:8545"
	chainID    = 713714 // default EVM chain ID for "sei-chain"
	targetTPS  = 500
	numWorkers = 250

	// Total unique sender accounts pre-funded in genesis. Every tx sent by
	// the stress test has a distinct sender with nonce=0. At targetTPS, the
	// pool lasts totalAccounts/targetTPS seconds.
	totalAccounts = 50_000
)

var (
	bigChainID  = big.NewInt(chainID)
	signer      = types.NewLondonSigner(bigChainID)
	maxFee      = big.NewInt(1_000_000_000_000) // 1000 gwei
	priorityFee = big.NewInt(1_000_000_000)     // 1 gwei
	txValue     = big.NewInt(1_000_000_000_001) // 10^12+1 wei: touches both usei balance and wei remainder
)

// nextKey returns a unique deterministic private key for the given index.
func nextKey(idx uint64) *ecdsa.PrivateKey {
	seed := make([]byte, 32)
	// use upper 8 bytes for the index so seed is never all-zero
	seed[0] = 0x01
	for i := 0; i < 8; i++ {
		seed[1+i] = byte(idx >> (56 - 8*i))
	}
	key, err := crypto.ToECDSA(seed)
	if err != nil {
		panic(fmt.Sprintf("bad key seed %d: %v", idx, err))
	}
	return key
}

func keyAddr(key *ecdsa.PrivateKey) common.Address {
	return crypto.PubkeyToAddress(key.PublicKey)
}

func evmToSei(addr common.Address) string {
	s, err := seibech32.ConvertAndEncode("sei", addr.Bytes())
	if err != nil {
		panic(fmt.Sprintf("bech32 encode: %v", err))
	}
	return s
}

func signTx(tx *types.Transaction, key *ecdsa.PrivateKey) *types.Transaction {
	signed, err := types.SignTx(tx, signer, key)
	if err != nil {
		panic(err)
	}
	return signed
}

func transfer(nonce uint64, to common.Address, key *ecdsa.PrivateKey) *types.Transaction {
	return signTx(types.NewTx(&types.DynamicFeeTx{
		ChainID:   bigChainID,
		Nonce:     nonce,
		GasTipCap: priorityFee,
		GasFeeCap: maxFee,
		Gas:       21_000,
		To:        &to,
		Value:     txValue,
	}), key)
}

func waitForBalance(ctx context.Context, client *ethclient.Client, addr common.Address) {
	fmt.Printf("waiting for %s to have balance...\n", addr.Hex())
	for {
		bal, err := client.BalanceAt(ctx, addr, nil)
		if err == nil && bal.Sign() > 0 {
			fmt.Printf("  %s: %s wei\n", addr.Hex(), bal.String())
			return
		}
		time.Sleep(300 * time.Millisecond)
	}
}

func main() {
	dumpSeiAddrs := flag.Bool("dump-sei-addrs", false, "print sender sei bech32 addresses for genesis funding and exit")
	flag.Parse()

	// Key 0 = recipient; keys 1..totalAccounts = one-time genesis-funded senders.
	recipient := keyAddr(nextKey(0))

	if *dumpSeiAddrs {
		for i := uint64(1); i <= totalAccounts; i++ {
			fmt.Println(evmToSei(keyAddr(nextKey(i))))
		}
		return
	}

	ctx := context.Background()
	client, err := ethclient.Dial(evmRPC)
	if err != nil {
		panic(fmt.Sprintf("dial %s: %v", evmRPC, err))
	}
	defer client.Close()

	fmt.Printf("recipient: %s\n", recipient.Hex())

	// Wait for genesis accounts to have balance — confirms the node is live.
	waitForBalance(ctx, client, keyAddr(nextKey(1)))

	// Pre-fill the work queue. Each key is used for exactly one tx (nonce=0).
	funded := make(chan *ecdsa.PrivateKey, totalAccounts)
	for i := uint64(1); i <= totalAccounts; i++ {
		funded <- nextKey(i)
	}
	close(funded)

	// Shared rate limiter across all workers: one tick per tx slot.
	ticker := time.NewTicker(time.Second / time.Duration(targetTPS))
	defer ticker.Stop()

	fmt.Printf("starting %d workers, %d unique senders, target %d TPS\n",
		numWorkers, totalAccounts, targetTPS)

	var wg sync.WaitGroup
	for range numWorkers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for key := range funded {
				<-ticker.C
				tx := transfer(0, recipient, key)
				_ = client.SendTransaction(ctx, tx)
			}
		}()
	}

	wg.Wait()
	fmt.Printf("all %d accounts exhausted\n", totalAccounts)
}
