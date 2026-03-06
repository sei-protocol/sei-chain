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

func rpcCall(endpoint, method string, params []interface{}) (*RPCResponse, time.Duration, error) {
	body, err := json.Marshal(RPCRequest{
		JSONRPC: "2.0",
		Method:  method,
		Params:  params,
		ID:      int(reqID.Add(1)),
	})
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

// benchMethod defines a single RPC method to benchmark.
type benchMethod struct {
	name   string
	params func() []interface{}
	weight int
	heavy  bool // heavy methods get dedicated sequential + concurrent phases
}

func (m *benchMethod) call(endpoint string) (string, time.Duration, error) {
	resp, lat, err := rpcCall(endpoint, m.name, m.params())
	if err == nil && resp != nil && resp.Error != nil {
		err = fmt.Errorf("rpc: %s", resp.Error.Message)
	}
	return m.name, lat, err
}

type storageSlot struct {
	Address string
	Slot    string
}

type BlockInfo struct {
	Number       int64
	Hash         string
	Transactions []string
	Addresses    []string
}

func discoverStorageSlots(endpoint string, txHashes []string, maxTxs int) []storageSlot {
	if maxTxs <= 0 || len(txHashes) == 0 {
		return nil
	}
	if maxTxs > len(txHashes) {
		maxTxs = len(txHashes)
	}

	var slots []storageSlot
	seen := make(map[string]bool)
	tracer := "prestateTracer"

	for i := 0; i < maxTxs; i++ {
		resp, _, err := rpcCall(endpoint, "debug_traceTransaction", []interface{}{
			txHashes[i],
			map[string]string{"tracer": tracer},
		})
		if err != nil || resp == nil || resp.Error != nil {
			continue
		}

		var prestate map[string]struct {
			Storage map[string]json.RawMessage `json:"storage"`
		}
		if err := json.Unmarshal(resp.Result, &prestate); err != nil {
			continue
		}
		for addr, acct := range prestate {
			for slot := range acct.Storage {
				key := addr + "|" + slot
				if !seen[key] {
					seen[key] = true
					slots = append(slots, storageSlot{Address: addr, Slot: slot})
				}
			}
		}
	}
	return slots
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
	var (
		endpoint      string
		concurrency   int
		blockCount    int
		requestsPer   int
		methodsFlag   string
		traceDiscover int
	)
	flag.StringVar(&endpoint, "endpoint", "", "RPC endpoint URL (required)")
	flag.IntVar(&concurrency, "concurrency", 16, "number of concurrent workers")
	flag.IntVar(&blockCount, "blocks", 20, "number of recent blocks to sample")
	flag.IntVar(&requestsPer, "requests", 100, "requests per method per phase")
	flag.StringVar(&methodsFlag, "methods", "", "comma-separated methods to run (default: all)")
	flag.IntVar(&traceDiscover, "trace-discover", 5, "txs to trace for storage slot discovery (0 to disable)")
	flag.Parse()

	if endpoint == "" {
		fmt.Fprintf(os.Stderr, "Usage: go run main.go -endpoint <rpc-url> [-concurrency 16] [-blocks 20] [-requests 100] [-methods debug_traceBlockByNumber,eth_getBalance]\n")
		os.Exit(1)
	}

	// =========================================================================
	// Discover recent blocks, transactions, and addresses
	// =========================================================================
	fmt.Printf("RPC Read Benchmark\n")
	fmt.Printf("  endpoint:    %s\n", endpoint)
	fmt.Printf("  concurrency: %d\n", concurrency)
	fmt.Printf("  blocks:      %d\n", blockCount)
	fmt.Printf("  requests:    %d per method per phase\n", requestsPer)

	fmt.Printf("\n--- Discovering recent blocks ---\n")
	latestBlock, err := getLatestBlockNumber(endpoint)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to get latest block: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Latest block: %d\n", latestBlock)

	var blocks []*BlockInfo
	var allTxHashes []string
	var allAddresses []string
	addrSeen := make(map[string]bool)

	for i := 0; i < blockCount; i++ {
		blockNum := latestBlock - int64(i)
		if blockNum < 1 {
			break
		}
		info, err := getBlockInfo(endpoint, blockNum)
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
	if len(allAddresses) == 0 {
		fmt.Fprintf(os.Stderr, "No addresses found in recent blocks\n")
		os.Exit(1)
	}
	fmt.Printf("Discovered %d blocks, %d transactions, %d unique addresses\n",
		len(blocks), len(allTxHashes), len(allAddresses))

	// Discover real storage slots from traced transactions
	var allStorageSlots []storageSlot
	if traceDiscover > 0 && len(allTxHashes) > 0 {
		fmt.Printf("\n--- Discovering storage slots (tracing %d txs) ---\n", min(traceDiscover, len(allTxHashes)))
		allStorageSlots = discoverStorageSlots(endpoint, allTxHashes, traceDiscover)
		fmt.Printf("Discovered %d unique storage slots\n", len(allStorageSlots))
	}

	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	latestHex := fmt.Sprintf("0x%x", latestBlock)
	randBlock := func() *BlockInfo { return blocks[rng.Intn(len(blocks))] }
	randAddr := func() string { return allAddresses[rng.Intn(len(allAddresses))] }
	randTxHash := func() string {
		if len(allTxHashes) == 0 {
			return ""
		}
		return allTxHashes[rng.Intn(len(allTxHashes))]
	}
	randStorageParams := func() []interface{} {
		if len(allStorageSlots) > 0 {
			s := allStorageSlots[rng.Intn(len(allStorageSlots))]
			return []interface{}{s.Address, s.Slot, latestHex}
		}
		return []interface{}{randAddr(), fmt.Sprintf("0x%064x", rng.Intn(10)), latestHex}
	}

	// =========================================================================
	// Method registry — add new methods here (one line each)
	// =========================================================================
	allMethods := []benchMethod{
		{"debug_traceBlockByNumber", func() []interface{} { return []interface{}{fmt.Sprintf("0x%x", randBlock().Number)} }, 10, true},
		{"debug_traceTransaction", func() []interface{} { return []interface{}{randTxHash()} }, 10, true},
		{"eth_getBalance", func() []interface{} { return []interface{}{randAddr(), latestHex} }, 25, false},
		{"eth_getTransactionCount", func() []interface{} { return []interface{}{randAddr(), latestHex} }, 15, false},
		{"eth_getCode", func() []interface{} { return []interface{}{randAddr(), latestHex} }, 15, false},
		{"eth_getStorageAt", func() []interface{} { return randStorageParams() }, 25, false},
	}

	// Skip debug_traceTransaction if no txs discovered
	if len(allTxHashes) == 0 {
		filtered := allMethods[:0]
		for _, m := range allMethods {
			if m.name != "debug_traceTransaction" {
				filtered = append(filtered, m)
			}
		}
		allMethods = filtered
	}

	// Filter by -methods flag if provided
	if methodsFlag != "" {
		allowed := make(map[string]bool)
		for _, m := range strings.Split(methodsFlag, ",") {
			allowed[strings.TrimSpace(m)] = true
		}
		filtered := allMethods[:0]
		for _, m := range allMethods {
			if allowed[m.name] {
				filtered = append(filtered, m)
			}
		}
		allMethods = filtered
	}

	if len(allMethods) == 0 {
		fmt.Fprintf(os.Stderr, "No methods selected\n")
		os.Exit(1)
	}

	fmt.Printf("  methods:     ")
	for i, m := range allMethods {
		if i > 0 {
			fmt.Printf(", ")
		}
		fmt.Printf("%s", m.name)
	}
	fmt.Printf("\n")

	// =========================================================================
	// Phase 1: Per-block trace — one trace per discovered block, prints each result
	// =========================================================================
	fmt.Printf("\n--- Per-block trace (1 req per block, %d blocks) ---\n", len(blocks))
	fmt.Printf("  %-12s  %-6s  %s\n", "BLOCK", "TXS", "LATENCY")
	fmt.Printf("  %-12s  %-6s  %s\n", "-----", "---", "-------")
	perBlockStats := &LatencyStats{Method: "debug_traceBlockByNumber"}
	for _, b := range blocks {
		hexNum := fmt.Sprintf("0x%x", b.Number)
		resp, lat, err := rpcCall(endpoint, "debug_traceBlockByNumber", []interface{}{hexNum})
		if err == nil && resp != nil && resp.Error != nil {
			err = fmt.Errorf("rpc: %s", resp.Error.Message)
		}
		perBlockStats.Total++
		perBlockStats.Latencies = append(perBlockStats.Latencies, lat)
		errStr := ""
		if err != nil {
			perBlockStats.Errors++
			errStr = fmt.Sprintf("  ERR: %v", err)
		}
		fmt.Printf("  %-12d  %-6d  %s%s\n", b.Number, len(b.Transactions), lat.Round(time.Millisecond), errStr)
	}
	perBlockStats.Duration = perBlockStats.Latencies[len(perBlockStats.Latencies)-1] // just for rps calc
	for _, l := range perBlockStats.Latencies {
		perBlockStats.Duration = max(perBlockStats.Duration, l)
	}
	totalTime := time.Duration(0)
	for _, l := range perBlockStats.Latencies {
		totalTime += l
	}
	perBlockStats.Duration = totalTime
	printStats("Per-block trace summary", map[string]*LatencyStats{"debug_traceBlockByNumber": perBlockStats})

	// =========================================================================
	// Phase 2: Heavy methods — concurrent blast
	// =========================================================================
	for i := range allMethods {
		m := &allMethods[i]
		if !m.heavy {
			continue
		}
		title := fmt.Sprintf("%s (concurrent x%d)", m.name, concurrency)
		fmt.Printf("\n--- %s ---\n", title)
		s := runConcurrent(concurrency, requestsPer, func(_ int) (string, time.Duration, error) {
			return m.call(endpoint)
		})
		printStats(title, s)
	}

	// =========================================================================
	// Phase 3: Light methods — concurrent per-method
	// =========================================================================
	lightStats := make(map[string]*LatencyStats)
	hasLight := false
	for i := range allMethods {
		m := &allMethods[i]
		if m.heavy {
			continue
		}
		hasLight = true
		s := runConcurrent(concurrency, requestsPer, func(_ int) (string, time.Duration, error) {
			return m.call(endpoint)
		})
		for k, v := range s {
			lightStats[k] = v
		}
	}
	if hasLight {
		printStats(fmt.Sprintf("State reads (concurrent x%d, %d reqs each)", concurrency, requestsPer), lightStats)
	}

	// =========================================================================
	// Phase 4: Mixed workload — all methods, weighted random
	// =========================================================================
	totalWeight := 0
	for _, m := range allMethods {
		totalWeight += m.weight
	}

	totalMixed := requestsPer * 3
	fmt.Printf("\n--- Mixed workload (concurrent x%d, %d total reqs) ---\n", concurrency, totalMixed)
	stats := runConcurrent(concurrency, totalMixed, func(_ int) (string, time.Duration, error) {
		r := rng.Intn(totalWeight)
		cumulative := 0
		for i := range allMethods {
			cumulative += allMethods[i].weight
			if r < cumulative {
				return allMethods[i].call(endpoint)
			}
		}
		return allMethods[len(allMethods)-1].call(endpoint)
	})
	printStats("Mixed workload", stats)

	fmt.Printf("\nBenchmark complete.\n")
}
