package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Default ERC20 contract on Sei V2
const DefaultERC20Contract = "0xe15fc38f6d8c56af07bbcbe3baf5708a2bf42392"

// Default from address (token holder) on Sei V2
const DefaultFromAddress = "0xf055616bFC551a314F24ab6C09e8e1582B49cb44"

// ERC20 function selectors
const (
	// balanceOf(address) with zero address
	ERC20BalanceOf = "0x70a082310000000000000000000000000000000000000000000000000000000000000000"
	// totalSupply()
	ERC20TotalSupply = "0x18160ddd"
	// decimals()
	ERC20Decimals = "0x313ce567"
	// name()
	ERC20Name = "0x06fdde03"
	// symbol()
	ERC20Symbol = "0x95d89b41"
	// transfer(address,uint256) function selector
	ERC20TransferSelector = "0xa9059cbb"
	// 1 unit (smallest amount, works for any token regardless of decimals)
	ERC20TransferAmount = "0000000000000000000000000000000000000000000000000000000000000001"
)

// JSONRPCRequest represents a JSON-RPC 2.0 request
type JSONRPCRequest struct {
	JSONRPC string        `json:"jsonrpc"`
	ID      int           `json:"id"`
	Method  string        `json:"method"`
	Params  []interface{} `json:"params"`
}

// JSONRPCResponse represents a JSON-RPC 2.0 response
type JSONRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int             `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *RPCError       `json:"error,omitempty"`
}

// RPCError represents a JSON-RPC error
type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// Config holds the test configuration
type Config struct {
	RPCURL       string
	Iterations   int
	Concurrency  int
	Method       string
	BlockTag     string
	ContractAddr string
	CallData     string
	ERC20Func    string
	FromAddr     string
	Delay        time.Duration
	Verbose      bool
	DryRun       bool
}

// Stats holds test statistics
type Stats struct {
	TotalRequests   int64
	SuccessCount    int64
	FailureCount    int64
	EVMModuleErrors int64
	OtherErrors     int64
	mu              sync.Mutex
	ErrorMessages   map[string]int64
}

func NewStats() *Stats {
	return &Stats{
		ErrorMessages: make(map[string]int64),
	}
}

func (s *Stats) RecordSuccess() {
	atomic.AddInt64(&s.TotalRequests, 1)
	atomic.AddInt64(&s.SuccessCount, 1)
}

func (s *Stats) RecordError(errMsg string) {
	atomic.AddInt64(&s.TotalRequests, 1)
	atomic.AddInt64(&s.FailureCount, 1)

	if strings.Contains(errMsg, "evm module does not exist on height") {
		atomic.AddInt64(&s.EVMModuleErrors, 1)
	} else {
		atomic.AddInt64(&s.OtherErrors, 1)
	}

	s.mu.Lock()
	s.ErrorMessages[errMsg]++
	s.mu.Unlock()
}

func (s *Stats) Print() {
	fmt.Println("\n========== Test Results ==========")
	fmt.Printf("Total Requests:      %d\n", atomic.LoadInt64(&s.TotalRequests))
	fmt.Printf("Successful:          %d\n", atomic.LoadInt64(&s.SuccessCount))
	fmt.Printf("Failed:              %d\n", atomic.LoadInt64(&s.FailureCount))
	fmt.Printf("EVM Module Errors:   %d\n", atomic.LoadInt64(&s.EVMModuleErrors))
	fmt.Printf("Other Errors:        %d\n", atomic.LoadInt64(&s.OtherErrors))

	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.ErrorMessages) > 0 {
		fmt.Println("\n--- Error Breakdown ---")
		for msg, count := range s.ErrorMessages {
			displayMsg := msg
			if len(displayMsg) > 100 {
				displayMsg = displayMsg[:100] + "..."
			}
			fmt.Printf("  [%d] %s\n", count, displayMsg)
		}
	}
	fmt.Println("===================================")
}

// buildTransferCallData builds the calldata for transfer(address,uint256)
// The recipient is the same as the from address (transfer to self)
func buildTransferCallData(fromAddr string) string {
	// Remove 0x prefix if present and pad to 32 bytes (64 hex chars)
	addr := strings.TrimPrefix(strings.ToLower(fromAddr), "0x")
	// Pad address to 32 bytes (addresses are 20 bytes, need 12 bytes of leading zeros)
	paddedAddr := fmt.Sprintf("%064s", addr)
	// transfer(address,uint256): selector + padded recipient + amount
	return ERC20TransferSelector + paddedAddr + ERC20TransferAmount
}

// getERC20CallData returns the calldata for a given ERC20 function preset
func getERC20CallData(funcName string, fromAddr string) string {
	switch strings.ToLower(funcName) {
	case "balanceof", "balance":
		return ERC20BalanceOf
	case "totalsupply", "supply":
		return ERC20TotalSupply
	case "decimals":
		return ERC20Decimals
	case "name":
		return ERC20Name
	case "symbol":
		return ERC20Symbol
	case "transfer":
		return buildTransferCallData(fromAddr)
	default:
		return ERC20BalanceOf // default to balanceOf
	}
}

// buildRequest builds the JSON-RPC request
func buildRequest(cfg *Config, id int) JSONRPCRequest {
	return JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      id,
		Method:  cfg.Method,
		Params:  buildParams(cfg),
	}
}

// generateCurlCommand generates a curl command for manual testing
func generateCurlCommand(cfg *Config) string {
	req := buildRequest(cfg, 1)
	reqBody, _ := json.Marshal(req)

	// Escape single quotes in the JSON for shell safety
	jsonStr := string(reqBody)

	return fmt.Sprintf("curl -X POST '%s' \\\n  -H 'Content-Type: application/json' \\\n  -d '%s'",
		cfg.RPCURL, jsonStr)
}

// generateCurlCommandPretty generates a curl command with pretty-printed JSON
func generateCurlCommandPretty(cfg *Config) string {
	req := buildRequest(cfg, 1)
	reqBody, _ := json.MarshalIndent(req, "", "  ")

	return fmt.Sprintf("curl -X POST '%s' \\\n  -H 'Content-Type: application/json' \\\n  -d '\n%s\n'",
		cfg.RPCURL, string(reqBody))
}

// buildCallObject builds the call object for eth_call and eth_estimateGas
func buildCallObject(cfg *Config) map[string]interface{} {
	callObject := map[string]interface{}{
		"to":   cfg.ContractAddr,
		"data": cfg.CallData,
	}
	if cfg.FromAddr != "" {
		callObject["from"] = cfg.FromAddr
	}
	return callObject
}

// buildParams builds parameters based on the method
func buildParams(cfg *Config) []interface{} {
	switch cfg.Method {
	case "eth_call":
		return []interface{}{buildCallObject(cfg), cfg.BlockTag}
	case "eth_getBalance":
		return []interface{}{cfg.ContractAddr, cfg.BlockTag}
	case "eth_getCode":
		return []interface{}{cfg.ContractAddr, cfg.BlockTag}
	case "eth_getStorageAt":
		return []interface{}{cfg.ContractAddr, "0x0", cfg.BlockTag}
	case "eth_blockNumber":
		return []interface{}{}
	case "eth_getTransactionCount":
		return []interface{}{cfg.ContractAddr, cfg.BlockTag}
	case "eth_estimateGas":
		return []interface{}{buildCallObject(cfg), cfg.BlockTag}
	default:
		return []interface{}{buildCallObject(cfg), cfg.BlockTag}
	}
}

// makeRequest sends a JSON-RPC request to the endpoint
func makeRequest(client *http.Client, cfg *Config, id int) (*JSONRPCResponse, error) {
	req := buildRequest(cfg, id)
	reqBody, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	httpReq, err := http.NewRequest("POST", cfg.RPCURL, bytes.NewBuffer(reqBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	var rpcResp JSONRPCResponse
	if err := json.Unmarshal(body, &rpcResp); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w (body: %s)", err, string(body))
	}

	return &rpcResp, nil
}

// worker processes requests in a goroutine
func worker(id int, cfg *Config, stats *Stats, jobs <-chan int, wg *sync.WaitGroup) {
	defer wg.Done()

	client := &http.Client{
		Timeout: 30 * time.Second,
	}

	for requestID := range jobs {
		resp, err := makeRequest(client, cfg, requestID)
		if err != nil {
			stats.RecordError(fmt.Sprintf("HTTP Error: %v", err))
			if cfg.Verbose {
				fmt.Printf("[Worker %d] Request %d - HTTP Error: %v\n", id, requestID, err)
			}
			continue
		}

		if resp.Error != nil {
			stats.RecordError(resp.Error.Message)
			if cfg.Verbose || strings.Contains(resp.Error.Message, "evm module does not exist") {
				fmt.Printf("[Worker %d] Request %d - RPC Error: %s (code: %d)\n",
					id, requestID, resp.Error.Message, resp.Error.Code)
			}
		} else {
			stats.RecordSuccess()
			if cfg.Verbose {
				fmt.Printf("[Worker %d] Request %d - Success\n", id, requestID)
			}
		}

		if cfg.Delay > 0 {
			time.Sleep(cfg.Delay)
		}
	}
}

func main() {
	cfg := Config{}

	flag.StringVar(&cfg.RPCURL, "rpc-url", "http://localhost:8545", "RPC endpoint URL")
	flag.IntVar(&cfg.Iterations, "iterations", 1000, "Number of requests to make")
	flag.IntVar(&cfg.Concurrency, "concurrency", 10, "Number of concurrent workers")
	flag.StringVar(&cfg.Method, "method", "eth_call", "RPC method to test (eth_call, eth_getBalance, eth_getCode, eth_getStorageAt, eth_estimateGas, eth_getTransactionCount)")
	flag.StringVar(&cfg.BlockTag, "block", "latest", "Block tag (latest, pending, earliest, safe, finalized, or block number)")
	flag.StringVar(&cfg.ContractAddr, "contract", DefaultERC20Contract, "Contract address for the call")
	flag.StringVar(&cfg.CallData, "data", "", "Call data (hex encoded). If empty, uses -erc20-func preset")
	flag.StringVar(&cfg.ERC20Func, "erc20-func", "balanceOf", "ERC20 function preset (balanceOf, totalSupply, decimals, name, symbol, transfer)")
	flag.StringVar(&cfg.FromAddr, "from", DefaultFromAddress, "From address for the call (needed for transfer simulation)")
	flag.DurationVar(&cfg.Delay, "delay", 0, "Delay between requests per worker (e.g., 100ms)")
	flag.BoolVar(&cfg.Verbose, "verbose", false, "Enable verbose logging")
	flag.BoolVar(&cfg.DryRun, "dry-run", false, "Print the curl command and exit without making requests")

	flag.Parse()

	// If no explicit calldata provided, use ERC20 function preset
	if cfg.CallData == "" {
		cfg.CallData = getERC20CallData(cfg.ERC20Func, cfg.FromAddr)
	}

	// Dry-run mode: print curl command and exit
	if cfg.DryRun {
		fmt.Println("========== Dry Run Mode ==========")
		fmt.Println("\nCurl command (compact):")
		fmt.Println(generateCurlCommand(&cfg))
		fmt.Println("\nCurl command (pretty):")
		fmt.Println(generateCurlCommandPretty(&cfg))
		fmt.Println("\n===================================")
		return
	}

	fmt.Println("========== Watermark Tester ==========")
	fmt.Printf("RPC URL:        %s\n", cfg.RPCURL)
	fmt.Printf("Method:         %s\n", cfg.Method)
	fmt.Printf("Block Tag:      %s\n", cfg.BlockTag)
	fmt.Printf("Contract:       %s\n", cfg.ContractAddr)
	fmt.Printf("From:           %s\n", cfg.FromAddr)
	fmt.Printf("Call Data:      %s\n", cfg.CallData)
	fmt.Printf("Iterations:     %d\n", cfg.Iterations)
	fmt.Printf("Concurrency:    %d\n", cfg.Concurrency)
	fmt.Printf("Delay:          %v\n", cfg.Delay)
	fmt.Println("=======================================")
	fmt.Println()

	stats := NewStats()

	// Create job channel
	jobs := make(chan int, cfg.Iterations)

	// Start workers
	var wg sync.WaitGroup
	for i := 0; i < cfg.Concurrency; i++ {
		wg.Add(1)
		go worker(i, &cfg, stats, jobs, &wg)
	}

	// Send jobs
	startTime := time.Now()
	for i := 1; i <= cfg.Iterations; i++ {
		jobs <- i
	}
	close(jobs)

	// Wait for all workers to complete
	wg.Wait()
	elapsed := time.Since(startTime)

	stats.Print()
	fmt.Printf("\nCompleted in %v (%.2f req/s)\n", elapsed, float64(cfg.Iterations)/elapsed.Seconds())

	// Exit with error code if we found EVM module errors
	if atomic.LoadInt64(&stats.EVMModuleErrors) > 0 {
		fmt.Println("\n⚠️  EVM module errors detected! Issue reproduced.")
		os.Exit(1)
	}
}
