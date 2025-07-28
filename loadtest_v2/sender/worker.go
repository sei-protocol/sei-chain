package sender

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/sei-protocol/sei-chain/loadtest_v2/types"
)

// Worker handles sending transactions to a specific endpoint
type Worker struct {
	id       int
	endpoint string
	client   *http.Client
	txChan   chan *types.LoadTx
	ctx      context.Context
	cancel   context.CancelFunc
	dryRun   bool
}

// NewWorker creates a new worker for a specific endpoint
func NewWorker(id int, endpoint string, bufferSize int) *Worker {
	ctx, cancel := context.WithCancel(context.Background())
	
	return &Worker{
		id:       id,
		endpoint: endpoint,
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
		txChan: make(chan *types.LoadTx, bufferSize),
		ctx:    ctx,
		cancel: cancel,
	}
}

// Start begins the worker's processing loop
func (w *Worker) Start() {
	go w.processTransactions()
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
	default:
		return fmt.Errorf("worker %d channel is full", w.id)
	}
}

// SetDryRun sets the dry-run mode for the worker
func (w *Worker) SetDryRun(dryRun bool) {
	w.dryRun = dryRun
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
	// In dry-run mode, log the transaction details instead of sending
	if w.dryRun {
		fmt.Printf("[DRY-RUN] Endpoint: %-30s | Scenario: %-15s | Sender: %s | Nonce: %d\n",
			w.endpoint,
			tx.Scenario.Name,
			tx.Scenario.Sender.Address.Hex(),
			tx.Scenario.Nonce)
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

	// Success - could add metrics/logging here
	// fmt.Printf("Worker %d: Transaction sent successfully\n", w.id)
}

// GetChannelLength returns the current length of the worker's channel (for monitoring)
func (w *Worker) GetChannelLength() int {
	return len(w.txChan)
}

// GetEndpoint returns the worker's endpoint
func (w *Worker) GetEndpoint() string {
	return w.endpoint
}
