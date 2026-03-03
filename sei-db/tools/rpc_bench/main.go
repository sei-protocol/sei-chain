package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type Config struct {
	Endpoint    string
	Concurrency int
	BlockCount  int
	RequestsPer int
}

type RPCRequest struct {
	JSONRPC string        `json:"jsonrpc"`
	Method  string        `json:"method"`
	Params  []interface{} `json:"params"`
	ID      int           `json:"id"`
}

type RPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int             `json:"id"`
	Result  json.RawMessage `json:"result"`
	Error   *RPCError       `json:"error"`
}

type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type LatencyStats struct {
	Method    string
	Total     int
	Errors    int
	Duration  time.Duration
	Latencies []time.Duration
}

func (s *LatencyStats) Report() {
	if s.Total == 0 {
		fmt.Printf("  %-35s  no requests\n", s.Method)
		return
	}
	sort.Slice(s.Latencies, func(i, j int) bool { return s.Latencies[i] < s.Latencies[j] })
	p := func(pct float64) time.Duration {
		if len(s.Latencies) == 0 {
			return 0
		}
		idx := int(float64(len(s.Latencies)) * pct)
		if idx >= len(s.Latencies) {
			idx = len(s.Latencies) - 1
		}
		return s.Latencies[idx]
	}
	rps := float64(s.Total) / s.Duration.Seconds()
	fmt.Printf("  %-35s  reqs=%-6d errs=%-4d rps=%-8.1f p50=%-10s p95=%-10s p99=%-10s\n",
		s.Method, s.Total, s.Errors, rps, p(0.50), p(0.95), p(0.99))
}

var httpClient = &http.Client{
	Timeout: 120 * time.Second,
	Transport: &http.Transport{
		MaxIdleConns:        200,
		MaxIdleConnsPerHost: 200,
		IdleConnTimeout:     90 * time.Second,
	},
}

var reqID atomic.Int64

func rpcCall(endpoint string, method string, params []interface{}) (*RPCResponse, time.Duration, error) {
	req := RPCRequest{
		JSONRPC: "2.0",
		Method:  method,
		Params:  params,
		ID:      int(reqID.Add(1)),
	}
	body, err := json.Marshal(req)
	if err != nil {
		return nil, 0, err
	}

	start := time.Now()
	resp, err := httpClient.Post(endpoint, "application/json", bytes.NewReader(body))
	elapsed := time.Since(start)
	if err != nil {
		return nil, elapsed, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, elapsed, err
	}

	var rpcResp RPCResponse
	if err := json.Unmarshal(respBody, &rpcResp); err != nil {
		return nil, elapsed, fmt.Errorf("bad response: %s", string(respBody[:min(len(respBody), 200)]))
	}
	return &rpcResp, elapsed, nil
}

func getLatestBlockNumber(endpoint string) (int64, error) {
	resp, _, err := rpcCall(endpoint, "eth_blockNumber", []interface{}{})
	if err != nil {
		return 0, err
	}
	if resp.Error != nil {
		return 0, fmt.Errorf("rpc error: %s", resp.Error.Message)
	}
	var hex string
	if err := json.Unmarshal(resp.Result, &hex); err != nil {
		return 0, err
	}
	var num int64
	fmt.Sscanf(hex, "0x%x", &num)
	return num, nil
}

type BlockInfo struct {
	Number       int64
	Hash         string
	Transactions []string // tx hashes
	Addresses    []string // from/to addresses found
}

func getBlockInfo(endpoint string, blockNum int64) (*BlockInfo, error) {
	hexNum := fmt.Sprintf("0x%x", blockNum)
	resp, _, err := rpcCall(endpoint, "eth_getBlockByNumber", []interface{}{hexNum, true})
	if err != nil {
		return nil, err
	}
	if resp.Error != nil {
		return nil, fmt.Errorf("rpc error: %s", resp.Error.Message)
	}

	var block struct {
		Hash         string `json:"hash"`
		Transactions []struct {
			Hash string `json:"hash"`
			From string `json:"from"`
			To   string `json:"to"`
		} `json:"transactions"`
	}
	if err := json.Unmarshal(resp.Result, &block); err != nil {
		return nil, err
	}

	info := &BlockInfo{Number: blockNum, Hash: block.Hash}
	addrSet := make(map[string]bool)
	for _, tx := range block.Transactions {
		info.Transactions = append(info.Transactions, tx.Hash)
		if tx.From != "" {
			addrSet[tx.From] = true
		}
		if tx.To != "" {
			addrSet[tx.To] = true
		}
	}
	for addr := range addrSet {
		info.Addresses = append(info.Addresses, addr)
	}
	return info, nil
}

// runConcurrent fires `total` requests across `concurrency` goroutines.
// workFn returns the method name, latency, and any error for one request.
func runConcurrent(concurrency, total int, workFn func(i int) (string, time.Duration, error)) map[string]*LatencyStats {
	stats := make(map[string]*LatencyStats)
	var mu sync.Mutex

	record := func(method string, lat time.Duration, err error) {
		mu.Lock()
		defer mu.Unlock()
		s, ok := stats[method]
		if !ok {
			s = &LatencyStats{Method: method}
			stats[method] = s
		}
		s.Total++
		s.Latencies = append(s.Latencies, lat)
		if err != nil {
			s.Errors++
		}
	}

	var wg sync.WaitGroup
	work := make(chan int, total)
	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for idx := range work {
				method, lat, err := workFn(idx)
				record(method, lat, err)
			}
		}()
	}

	start := time.Now()
	for i := 0; i < total; i++ {
		work <- i
	}
	close(work)
	wg.Wait()
	elapsed := time.Since(start)

	for _, s := range stats {
		s.Duration = elapsed
	}
	return stats
}

func printStats(title string, stats map[string]*LatencyStats) {
	fmt.Printf("\n%s\n%s\n", title, strings.Repeat("=", len(title)))

	keys := make([]string, 0, len(stats))
	for k := range stats {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var totalReqs, totalErrs int
	var totalDuration time.Duration
	for _, k := range keys {
		stats[k].Report()
		totalReqs += stats[k].Total
		totalErrs += stats[k].Errors
		if stats[k].Duration > totalDuration {
			totalDuration = stats[k].Duration
		}
	}
	rps := float64(totalReqs) / totalDuration.Seconds()
	fmt.Printf("  %-35s  reqs=%-6d errs=%-4d rps=%-8.1f duration=%s\n",
		"TOTAL", totalReqs, totalErrs, rps, totalDuration.Round(time.Millisecond))
}

func main() {
	cfg := Config{}
	flag.StringVar(&cfg.Endpoint, "endpoint", "", "RPC endpoint URL (required)")
	flag.IntVar(&cfg.Concurrency, "concurrency", 16, "number of concurrent workers")
	flag.IntVar(&cfg.BlockCount, "blocks", 20, "number of recent blocks to sample")
	flag.IntVar(&cfg.RequestsPer, "requests", 100, "requests per method per phase")
	flag.Parse()

	if cfg.Endpoint == "" {
		fmt.Fprintf(os.Stderr, "Usage: go run main.go -endpoint <rpc-url> [-concurrency 16] [-blocks 20] [-requests 100]\n")
		os.Exit(1)
	}

	fmt.Printf("RPC Read Benchmark\n")
	fmt.Printf("  endpoint:    %s\n", cfg.Endpoint)
	fmt.Printf("  concurrency: %d\n", cfg.Concurrency)
	fmt.Printf("  blocks:      %d\n", cfg.BlockCount)
	fmt.Printf("  requests:    %d per method per phase\n", cfg.RequestsPer)

	// =========================================================================
	// Phase 0: Discover recent blocks, transactions, and addresses
	// =========================================================================
	fmt.Printf("\n--- Discovering recent blocks ---\n")

	latestBlock, err := getLatestBlockNumber(cfg.Endpoint)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to get latest block: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Latest block: %d\n", latestBlock)

	var blocks []*BlockInfo
	var allTxHashes []string
	var allAddresses []string
	addrSeen := make(map[string]bool)

	for i := 0; i < cfg.BlockCount; i++ {
		blockNum := latestBlock - int64(i)
		if blockNum < 1 {
			break
		}
		info, err := getBlockInfo(cfg.Endpoint, blockNum)
		if err != nil {
			fmt.Printf("  block %d: error %v\n", blockNum, err)
			continue
		}
		blocks = append(blocks, info)
		allTxHashes = append(allTxHashes, info.Transactions...)
		for _, addr := range info.Addresses {
			if !addrSeen[addr] {
				addrSeen[addr] = true
				allAddresses = append(allAddresses, addr)
			}
		}
		fmt.Printf("  block %d: %d txs, %d addresses\n", blockNum, len(info.Transactions), len(info.Addresses))
	}

	if len(blocks) == 0 {
		fmt.Fprintf(os.Stderr, "No blocks discovered\n")
		os.Exit(1)
	}
	fmt.Printf("Discovered %d blocks, %d transactions, %d unique addresses\n",
		len(blocks), len(allTxHashes), len(allAddresses))

	if len(allAddresses) == 0 {
		fmt.Fprintf(os.Stderr, "No addresses found in recent blocks, cannot run state queries\n")
		os.Exit(1)
	}

	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	randBlock := func() *BlockInfo { return blocks[rng.Intn(len(blocks))] }
	randAddr := func() string { return allAddresses[rng.Intn(len(allAddresses))] }
	randTxHash := func() string {
		if len(allTxHashes) == 0 {
			return ""
		}
		return allTxHashes[rng.Intn(len(allTxHashes))]
	}

	latestHex := fmt.Sprintf("0x%x", latestBlock)

	// =========================================================================
	// Phase 1: debug_traceBlockByNumber — sequential (1 at a time)
	// =========================================================================
	fmt.Printf("\n--- Phase 1: debug_traceBlockByNumber (sequential) ---\n")
	seqCount := min(cfg.RequestsPer, len(blocks))
	stats1 := runConcurrent(1, seqCount, func(i int) (string, time.Duration, error) {
		b := blocks[i%len(blocks)]
		hexNum := fmt.Sprintf("0x%x", b.Number)
		resp, lat, err := rpcCall(cfg.Endpoint, "debug_traceBlockByNumber", []interface{}{hexNum})
		if err == nil && resp != nil && resp.Error != nil {
			err = fmt.Errorf("rpc: %s", resp.Error.Message)
		}
		return "debug_traceBlockByNumber", lat, err
	})
	printStats("Phase 1: debug_traceBlockByNumber (sequential)", stats1)

	// =========================================================================
	// Phase 2: debug_traceBlockByNumber — concurrent blast
	// =========================================================================
	fmt.Printf("\n--- Phase 2: debug_traceBlockByNumber (concurrent x%d) ---\n", cfg.Concurrency)
	stats2 := runConcurrent(cfg.Concurrency, cfg.RequestsPer, func(i int) (string, time.Duration, error) {
		b := randBlock()
		hexNum := fmt.Sprintf("0x%x", b.Number)
		resp, lat, err := rpcCall(cfg.Endpoint, "debug_traceBlockByNumber", []interface{}{hexNum})
		if err == nil && resp != nil && resp.Error != nil {
			err = fmt.Errorf("rpc: %s", resp.Error.Message)
		}
		return "debug_traceBlockByNumber", lat, err
	})
	printStats("Phase 2: debug_traceBlockByNumber (concurrent)", stats2)

	// =========================================================================
	// Phase 3: debug_traceTransaction — concurrent blast
	// =========================================================================
	if len(allTxHashes) > 0 {
		fmt.Printf("\n--- Phase 3: debug_traceTransaction (concurrent x%d) ---\n", cfg.Concurrency)
		stats3 := runConcurrent(cfg.Concurrency, cfg.RequestsPer, func(i int) (string, time.Duration, error) {
			txHash := randTxHash()
			resp, lat, err := rpcCall(cfg.Endpoint, "debug_traceTransaction", []interface{}{txHash})
			if err == nil && resp != nil && resp.Error != nil {
				err = fmt.Errorf("rpc: %s", resp.Error.Message)
			}
			return "debug_traceTransaction", lat, err
		})
		printStats("Phase 3: debug_traceTransaction (concurrent)", stats3)
	}

	// =========================================================================
	// Phase 4: State read methods — concurrent blast
	// Each method gets `requestsPer` calls at `concurrency` parallelism.
	// =========================================================================
	type stateMethod struct {
		name   string
		params func() []interface{}
	}
	stateMethods := []stateMethod{
		{"eth_getBalance", func() []interface{} { return []interface{}{randAddr(), latestHex} }},
		{"eth_getTransactionCount", func() []interface{} { return []interface{}{randAddr(), latestHex} }},
		{"eth_getCode", func() []interface{} { return []interface{}{randAddr(), latestHex} }},
		{"eth_getStorageAt", func() []interface{} {
			slot := fmt.Sprintf("0x%064x", rng.Intn(10))
			return []interface{}{randAddr(), slot, latestHex}
		}},
	}

	fmt.Printf("\n--- Phase 4: State reads (concurrent x%d, %d reqs each) ---\n", cfg.Concurrency, cfg.RequestsPer)
	allStats4 := make(map[string]*LatencyStats)
	for _, sm := range stateMethods {
		method := sm // capture
		s := runConcurrent(cfg.Concurrency, cfg.RequestsPer, func(i int) (string, time.Duration, error) {
			params := method.params()
			resp, lat, err := rpcCall(cfg.Endpoint, method.name, params)
			if err == nil && resp != nil && resp.Error != nil {
				err = fmt.Errorf("rpc: %s", resp.Error.Message)
			}
			return method.name, lat, err
		})
		for k, v := range s {
			allStats4[k] = v
		}
	}
	printStats("Phase 4: State reads (per-method)", allStats4)

	// =========================================================================
	// Phase 5: Mixed workload — all methods at once
	// =========================================================================
	type weightedMethod struct {
		name   string
		weight int
		call   func() (time.Duration, error)
	}
	mixed := []weightedMethod{
		{"debug_traceBlockByNumber", 10, func() (time.Duration, error) {
			b := randBlock()
			hexNum := fmt.Sprintf("0x%x", b.Number)
			resp, lat, err := rpcCall(cfg.Endpoint, "debug_traceBlockByNumber", []interface{}{hexNum})
			if err == nil && resp != nil && resp.Error != nil {
				err = fmt.Errorf("rpc: %s", resp.Error.Message)
			}
			return lat, err
		}},
		{"eth_getBalance", 25, func() (time.Duration, error) {
			resp, lat, err := rpcCall(cfg.Endpoint, "eth_getBalance", []interface{}{randAddr(), latestHex})
			if err == nil && resp != nil && resp.Error != nil {
				err = fmt.Errorf("rpc: %s", resp.Error.Message)
			}
			return lat, err
		}},
		{"eth_getStorageAt", 25, func() (time.Duration, error) {
			slot := fmt.Sprintf("0x%064x", rng.Intn(10))
			resp, lat, err := rpcCall(cfg.Endpoint, "eth_getStorageAt", []interface{}{randAddr(), slot, latestHex})
			if err == nil && resp != nil && resp.Error != nil {
				err = fmt.Errorf("rpc: %s", resp.Error.Message)
			}
			return lat, err
		}},
		{"eth_getCode", 15, func() (time.Duration, error) {
			resp, lat, err := rpcCall(cfg.Endpoint, "eth_getCode", []interface{}{randAddr(), latestHex})
			if err == nil && resp != nil && resp.Error != nil {
				err = fmt.Errorf("rpc: %s", resp.Error.Message)
			}
			return lat, err
		}},
		{"eth_getTransactionCount", 15, func() (time.Duration, error) {
			resp, lat, err := rpcCall(cfg.Endpoint, "eth_getTransactionCount", []interface{}{randAddr(), latestHex})
			if err == nil && resp != nil && resp.Error != nil {
				err = fmt.Errorf("rpc: %s", resp.Error.Message)
			}
			return lat, err
		}},
	}
	if len(allTxHashes) > 0 {
		mixed = append(mixed, weightedMethod{"debug_traceTransaction", 10, func() (time.Duration, error) {
			resp, lat, err := rpcCall(cfg.Endpoint, "debug_traceTransaction", []interface{}{randTxHash()})
			if err == nil && resp != nil && resp.Error != nil {
				err = fmt.Errorf("rpc: %s", resp.Error.Message)
			}
			return lat, err
		}})
	}

	totalWeight := 0
	for _, m := range mixed {
		totalWeight += m.weight
	}

	totalMixed := cfg.RequestsPer * 3
	fmt.Printf("\n--- Phase 5: Mixed workload (concurrent x%d, %d total reqs) ---\n", cfg.Concurrency, totalMixed)
	stats5 := runConcurrent(cfg.Concurrency, totalMixed, func(i int) (string, time.Duration, error) {
		r := rng.Intn(totalWeight)
		cumulative := 0
		for _, m := range mixed {
			cumulative += m.weight
			if r < cumulative {
				lat, err := m.call()
				return m.name, lat, err
			}
		}
		last := mixed[len(mixed)-1]
		lat, err := last.call()
		return last.name, lat, err
	})
	printStats("Phase 5: Mixed workload", stats5)

	fmt.Printf("\nBenchmark complete.\n")
}
