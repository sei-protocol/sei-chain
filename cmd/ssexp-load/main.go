// Command ssexp-load sends a slow stream of EVM transfers and contract deploys to the
// local Autobahn EVM-only cluster. Experiment tooling only.
package main

import (
	"context"
	"crypto/ecdsa"
	"flag"
	"fmt"
	"log"
	"math/big"
	"os"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
)

// runtime returns 32 bytes holding 1; init code copies it out and returns it as the contract code.
var deployCode = common.FromHex("0x600a600c600039600a6000f3600160005260206000f3")

func main() {
	endpoints := flag.String("rpc", "http://127.0.0.1:8545,http://127.0.0.1:8547,http://127.0.0.1:8549,http://127.0.0.1:8551", "comma-separated EVM RPC endpoints")
	tps := flag.Float64("tps", 3, "transactions per second")
	deployEvery := flag.Int("deploy-every", 50, "deploy a contract every N transactions")
	deployLog := flag.String("deploy-log", "/tmp/ssexp/deploys.txt", "file that records deployed contract addresses")
	flag.Parse()

	ctx := context.Background()
	var clients []*ethclient.Client
	for _, e := range strings.Split(*endpoints, ",") {
		c, err := ethclient.DialContext(ctx, e)
		if err != nil {
			log.Fatalf("dial %s: %v", e, err)
		}
		clients = append(clients, c)
	}
	chainID, err := clients[0].ChainID(ctx)
	if err != nil {
		log.Fatalf("chain id: %v", err)
	}
	signer := ethtypes.LatestSignerForChainID(chainID)
	logFile, err := os.OpenFile(*deployLog, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		log.Fatalf("open deploy log: %v", err)
	}
	defer func() { _ = logFile.Close() }()

	interval := time.Duration(float64(time.Second) / *tps)
	to := common.HexToAddress("0x000000000000000000000000000000000000dEaD")
	for i := 0; ; i++ {
		key, err := crypto.GenerateKey()
		if err != nil {
			log.Fatal(err)
		}
		deploy := *deployEvery > 0 && i%*deployEvery == 0
		tx, err := buildTx(key, signer, to, deploy)
		if err != nil {
			log.Fatal(err)
		}
		c := clients[i%len(clients)]
		if err := c.SendTransaction(ctx, tx); err != nil {
			log.Printf("send %d: %v", i, err)
		} else if deploy {
			from := crypto.PubkeyToAddress(key.PublicKey)
			addr := crypto.CreateAddress(from, 0)
			_, _ = fmt.Fprintf(logFile, "%s %s %s\n", time.Now().UTC().Format(time.RFC3339), tx.Hash(), addr)
		}
		if i%100 == 0 {
			log.Printf("sent %d", i)
		}
		time.Sleep(interval)
	}
}

func buildTx(key *ecdsa.PrivateKey, signer ethtypes.Signer, to common.Address, deploy bool) (*ethtypes.Transaction, error) {
	inner := &ethtypes.LegacyTx{
		Nonce:    0,
		GasPrice: big.NewInt(1_000_000_000),
		Gas:      21_000,
		To:       &to,
		Value:    big.NewInt(1),
	}
	if deploy {
		inner.To = nil
		inner.Value = big.NewInt(0)
		inner.Gas = 200_000
		inner.Data = deployCode
	}
	return ethtypes.SignNewTx(key, signer, inner)
}
