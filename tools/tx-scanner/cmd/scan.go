package cmd

import (
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/sei-protocol/sei-chain/tools/tx-scanner/client"
	"github.com/sei-protocol/sei-chain/tools/tx-scanner/query"
	"github.com/sei-protocol/sei-chain/tools/tx-scanner/state"
	"github.com/spf13/cobra"
	"golang.org/x/time/rate"
)

func ScanCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "scan-tx",
		Short: "A tool to scan missing transactions",
		Run:   execute,
	}
	cmd.PersistentFlags().String("endpoint", "127.0.0.1", "GRPC server endpoint")
	cmd.PersistentFlags().Int("port", 9090, "GRPC server port")
	cmd.PersistentFlags().Int("batch-size", 100, "Batch size to query")
	cmd.PersistentFlags().Int("bps-limit", 400, "Blocks per second limit")
	cmd.PersistentFlags().Int64("start-height", 0, "Start height")
	cmd.PersistentFlags().String("state-dir", "", "State file directory, the scanner will record the last scanned offset and scan results")
	return cmd
}

func execute(cmd *cobra.Command, _ []string) {
	endpoint, _ := cmd.Flags().GetString("endpoint")
	port, _ := cmd.Flags().GetInt("port")
	bpsLimit, _ := cmd.Flags().GetInt("bps-limit")
	batchSize, _ := cmd.Flags().GetInt("batch-size")
	stateDir, _ := cmd.Flags().GetString("state-dir")
	startHeight, _ := cmd.Flags().GetInt64("start-height")
	var badBlocks []int64
	var currentState = state.State{}
	if batchSize > bpsLimit {
		batchSize = bpsLimit
	}
	if startHeight <= 0 {
		startHeight = 1
	}
	if stateDir != "" {
		scanState, _ := state.ReadState(stateDir)
		if scanState.LastProcessedHeight > 0 {
			fmt.Printf("Detected last processed height: %d\n", scanState.LastProcessedHeight)
			currentState = scanState
			startHeight = currentState.LastProcessedHeight
			badBlocks = currentState.BlocksMissingTxs
		}
	}
	fmt.Printf("Starting the scan from height: %d\n", startHeight)
	client.InitializeGRPCClient(endpoint, port)
	rateLimiter := rate.NewLimiter(rate.Limit(bpsLimit), bpsLimit)
	latestHeight := getLatestBlockHeight()
	var currBlockHeight = startHeight
	for {
		if currBlockHeight >= latestHeight {
			time.Sleep(10 * time.Second)
			latestHeight = getLatestBlockHeight()
			continue
		}
		if !rateLimiter.AllowN(time.Now(), batchSize) {
			time.Sleep(10 * time.Millisecond)
			continue
		}

		wg := sync.WaitGroup{}

		// Handle ALL the queries in a batch concurrently
		var adjustedBatchSize = int(math.Min(float64(batchSize), float64(latestHeight-currBlockHeight)))
		var mtx = sync.Mutex{}
		var errors []error
		for i := 0; i < adjustedBatchSize; i++ {
			height := currBlockHeight + int64(i)
			wg.Add(1)
			go func(height int64) {
				defer wg.Done()
				isBad, err := processBlock(height)
				mtx.Lock()
				defer mtx.Unlock()
				if err != nil {
					errors = append(errors, err)
					return
				}
				if isBad {
					badBlocks = append(badBlocks, height)
				}
			}(height)
		}
		// Wait for ALL queries in this batch to finish and then check any failures
		wg.Wait()
		if len(errors) > 0 {
			fmt.Printf("Failed to process some blocks between heights %d and %d\n", currBlockHeight, currBlockHeight+int64(adjustedBatchSize))
		} else {
			// update the state
			currBlockHeight += int64(adjustedBatchSize)
			currentState.LastProcessedHeight = currBlockHeight
			currentState.BlocksMissingTxs = badBlocks
			if stateDir != "" {
				err := state.WriteState(stateDir, currentState)
				if err != nil {
					panic(err)
				}
			}
		}
	}
}

// processBlock processes a single block to find missing transactions
func processBlock(height int64) (bool, error) {
	if height%1000 == 0 {
		fmt.Printf("Processing block height %d\n", height)
	}
	// Query the block to get the number of TXs
	blockResp, err := query.GetBlockByHeight(height)
	if err != nil {
		return false, err
	}
	numTxInBlock := len(blockResp.Block.Data.Txs)
	// Get all indexed TXs events
	txResp, err := query.GetTxsEvent(height)
	if err != nil {
		return false, err
	}
	numTxIndexed := len(txResp.Txs)
	// Check if the number matches
	if numTxIndexed == 0 && numTxIndexed != numTxInBlock {
		fmt.Printf("[Fatal] Missing TXs at block height %d\n", height)
		return true, nil
	}
	// Now make sure each TX does exist
	for _, resp := range txResp.TxResponses {
		hash := resp.TxHash
		txByHashResponse, err := query.GetTxByHash(hash)
		if err != nil || txByHashResponse.TxResponse == nil || txByHashResponse.TxResponse.TxHash != hash {
			fmt.Printf("[Fatal] Failed to find transaction hash %s at block height %d \n", hash, height)
			return true, nil
		}
	}
	return false, nil
}

func getLatestBlockHeight() int64 {
	response, err := query.GetLatestBlock()
	if err != nil {
		return -1
	}
	return response.GetBlock().Header.Height
}
