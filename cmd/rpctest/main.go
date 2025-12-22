package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"math/big"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"
)

func main() {
	ip := flag.String("ip", "127.0.0.1", "IP address of the RPC endpoint")
	port := flag.String("port", "8545", "Port of the RPC endpoint")
	exp := flag.String("exp", "logs", "Experiment to run (currently only 'logs')")
	concurrency := flag.Int("concurrency", 1, "Number of concurrent queries")
	addrStr := flag.String("addr", "", "Contract address to filter logs (optional)")
	topicStr := flag.String("topic", "0xddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef", "Topic0 to filter (default: Transfer)")

	flag.Parse()

	if *exp != "logs" {
		log.Fatalf("Unknown experiment: %s", *exp)
	}

	url := fmt.Sprintf("http://%s:%s", *ip, *port)
	fmt.Printf("Testing RPC endpoint: %s\n", url)
	fmt.Printf("Concurrency: %d\n\n", *concurrency)

	client, err := ethclient.Dial(url)
	if err != nil {
		log.Fatalf("Failed to connect to the Ethereum client: %v", err)
	}

	// Get latest block
	header, err := client.HeaderByNumber(context.Background(), nil)
	if err != nil {
		log.Fatalf("Failed to get latest block: %v", err)
	}
	latestBlock := header.Number.Int64()
	fmt.Printf("Latest block: %d\n\n", latestBlock)

	fmt.Println("=== LOGS READ EXPERIMENT ===")
	fmt.Printf("Querying logs over varying block ranges [latest-X, latest] with address filter\n")
	fmt.Printf("Latest block: %d\n", latestBlock)
	if *addrStr != "" {
		fmt.Printf("Filter address: %s\n", *addrStr)
	} else {
		fmt.Printf("Filter address: <none>\n")
	}
	fmt.Println()

	// Fixed-width formatting with detailed stats
	// Range | Logs | Min | Max | Avg | Total Time | Status
	fmt.Printf("%-10s | %-10s | %-12s | %-12s | %-12s | %-12s | %-8s\n",
		"Range", "Logs", "Min Latency", "Max Latency", "Avg Latency", "Total Time", "Status")
	fmt.Printf("%-10s | %-10s | %-12s | %-12s | %-12s | %-12s | %-8s\n",
		"----------", "----------", "------------", "------------", "------------", "------------", "--------")

	ranges := []int64{1, 2, 5, 10, 20, 50, 100, 200, 500, 1000, 2000, 10000}

	var filterAddr []common.Address
	if *addrStr != "" {
		if !common.IsHexAddress(*addrStr) {
			log.Fatalf("Invalid address: %s", *addrStr)
		}
		filterAddr = []common.Address{common.HexToAddress(*addrStr)}
	}

	var topics [][]common.Hash
	if *topicStr != "" {
		topics = [][]common.Hash{{common.HexToHash(*topicStr)}}
	}

	for _, r := range ranges {
		fromBlock := latestBlock - r
		if fromBlock < 0 {
			fromBlock = 0
		}

		query := ethereum.FilterQuery{
			FromBlock: big.NewInt(fromBlock),
			ToBlock:   big.NewInt(latestBlock),
			Addresses: filterAddr,
			Topics:    topics,
		}

		var wg sync.WaitGroup
		var logsFound int
		var failures int
		var latencies []time.Duration
		var mu sync.Mutex

		batchStart := time.Now()

		for i := 0; i < *concurrency; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()

				callStart := time.Now()
				logs, err := client.FilterLogs(context.Background(), query)
				callDuration := time.Since(callStart)

				mu.Lock()
				defer mu.Unlock()

				if err != nil {
					failures++
				} else {
					latencies = append(latencies, callDuration)
					// Assuming all consistent queries return same count
					logsFound = len(logs)
				}
			}()
		}

		wg.Wait()
		totalBatchDuration := time.Since(batchStart)

		status := "OK"
		if failures > 0 {
			status = fmt.Sprintf("ERR(%d)", failures)
		}

		var minLat, maxLat, avgLat time.Duration
		if len(latencies) > 0 {
			minLat = latencies[0]
			maxLat = latencies[0]
			var sum time.Duration
			for _, d := range latencies {
				if d < minLat {
					minLat = d
				}
				if d > maxLat {
					maxLat = d
				}
				sum += d
			}
			avgLat = sum / time.Duration(len(latencies))
		}

		fmt.Printf("%-10d | %-10d | %-12v | %-12v | %-12v | %-12v | %-8s\n",
			r, logsFound, minLat, maxLat, avgLat, totalBatchDuration, status)
	}
}
