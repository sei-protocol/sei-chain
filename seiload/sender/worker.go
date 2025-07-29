package sender

import (
	"bytes"
	"context"
	"fmt"
	"github.com/ethereum/go-ethereum/ethclient"
	"io"
	"net/http"
	"time"

	"seiload/stats"
	"seiload/types"
)

// Worker handles sending transactions to a specific endpoint
type Worker struct {
	id        int
	endpoint  string
	client    *http.Client
	txChan    chan *types.LoadTx
	sentTxs   chan *types.LoadTx
	ctx       context.Context
	cancel    context.CancelFunc
	dryRun    bool
	debug     bool
	collector *stats.Collector
	logger    *stats.Logger
	workers   int
}

// NewWorker creates a new worker for a specific endpoint
func NewWorker(id int, endpoint string, bufferSize int, workers int) *Worker {
	ctx, cancel := context.WithCancel(context.Background())

	return &Worker{
		id:       id,
		endpoint: endpoint,
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
		txChan:  make(chan *types.LoadTx, bufferSize),
		sentTxs: make(chan *types.LoadTx, bufferSize),
		ctx:     ctx,
		cancel:  cancel,
		workers: workers,
	}
}

// SetStatsCollector sets the statistics collector for this worker
func (w *Worker) SetStatsCollector(collector *stats.Collector, logger *stats.Logger) {
	w.collector = collector
	w.logger = logger
}

// Start begins the worker's processing loop
func (w *Worker) Start() {
	// Start multiple worker goroutines that share the same channel
	for i := 0; i < w.workers; i++ {
		go w.processTransactions()
	}
	go w.watchTransactions()
}

// Stop gracefully shuts down the worker
func (w *Worker) Stop() {
	w.cancel()
	close(w.txChan)
}

// Send queues a transaction for this worker to process
func (w *Worker) Send(tx *types.LoadTx) error {
	select {
	case w.txChan <- tx:
		return nil
	case <-w.ctx.Done():
		return fmt.Errorf("worker %d is shutting down", w.id)
	}
}

// SetDebug sets the dry-run mode for the worker
func (w *Worker) SetDebug(debug bool) {
	w.debug = debug
}

// SetDryRun sets the dry-run mode for the worker
func (w *Worker) SetDryRun(dryRun bool) {
	w.dryRun = dryRun
}

func (w *Worker) watchTransactions() {
	if w.dryRun {
		return
	}
	eth, err := ethclient.Dial(w.endpoint)
	if err != nil {
		panic(err)
	}

	for {
		select {
		case tx, ok := <-w.sentTxs:
			if !ok {
				return // Channel closed, worker should exit
			}
			w.waitForReceipt(eth, tx)

		case <-w.ctx.Done():
			return // Context cancelled, worker should exit
		}
	}
}

func (w *Worker) waitForReceipt(eth *ethclient.Client, tx *types.LoadTx) {
	timeout := time.After(10 * time.Second)
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-timeout:
			fmt.Printf("❌ timeout waiting for receipt for tx %s\n", tx.EthTx.Hash().Hex())
			return

		case <-ticker.C:
			receipt, err := eth.TransactionReceipt(context.Background(), tx.EthTx.Hash())
			if err != nil {
				if err.Error() == "not found" {
					continue
				}
				fmt.Printf("❌ error getting receipt for tx %s: %v\n", tx.EthTx.Hash().Hex(), err)
				continue
			}

			// Receipt found - log status and return
			if receipt.Status != 1 {
				fmt.Printf("❌ tx %s failed\n", tx.EthTx.Hash().Hex())
			} else if w.debug {
				fmt.Printf("✅ tx %s, %s, gas=%d succeeded\n", tx.Scenario.Name, tx.EthTx.Hash().Hex(), receipt.GasUsed)
			}
			return

		case <-w.ctx.Done():
			return
		}
	}
}

// processTransactions is the main worker loop that processes transactions
func (w *Worker) processTransactions() {
	for {
		select {
		case tx, ok := <-w.txChan:
			if !ok {
				// Channel closed, worker should exit
				return
			}
			w.sendTransaction(tx)
		case <-w.ctx.Done():
			// Context cancelled, worker should exit
			return
		}
	}
}

// sendTransaction sends a single transaction to the endpoint
func (w *Worker) sendTransaction(tx *types.LoadTx) {
	startTime := time.Now()
	success := false

	defer func() {
		// Record statistics if collector is available
		if w.collector != nil {
			latency := time.Since(startTime)
			w.collector.RecordTransaction(tx.Scenario.Name, w.endpoint, latency, success)
		}
	}()

	if w.dryRun {
		// In dry-run mode, simulate processing time and mark as successful
		// Use very minimal delay to avoid channel overflow
		time.Sleep(10 * time.Microsecond) // Much faster simulation
		success = true
		return
	}

	// Create HTTP request with JSON-RPC payload
	req, err := http.NewRequestWithContext(w.ctx, "POST", w.endpoint, bytes.NewReader(tx.JSONRPCPayload))
	if err != nil {
		fmt.Printf("Worker %d: Failed to create request: %v\n", w.id, err)
		return
	}

	// Set headers for JSON-RPC
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	// Send the request
	resp, err := w.client.Do(req)
	if err != nil {
		fmt.Printf("Worker %d: Failed to send transaction: %v\n", w.id, err)
		return
	}
	defer resp.Body.Close()

	// Read response (optional, for debugging)
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		fmt.Printf("Worker %d: HTTP error %d: %s\n", w.id, resp.StatusCode, string(body))
		return
	}

	// Check if request was successful
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		success = true
	} else {
		fmt.Printf("Worker %d: HTTP error %d for transaction to %s\n", w.id, resp.StatusCode, w.endpoint)
	}

	// Write to sentTxs channel without blocking
	select {
	case w.sentTxs <- tx:
	default:
	}
}

// GetChannelLength returns the current length of the worker's channel (for monitoring)
func (w *Worker) GetChannelLength() int {
	return len(w.txChan)
}

// GetEndpoint returns the worker's endpoint
func (w *Worker) GetEndpoint() string {
	return w.endpoint
}
