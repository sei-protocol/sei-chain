package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"math"
	"math/rand"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"
	"golang.org/x/image/math/fixed"
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

func configureOutput(outputFile string) (func() error, error) {
	if outputFile == "" {
		return func() error { return nil }, nil
	}

	cleanPath := filepath.Clean(outputFile)
	if err := os.MkdirAll(filepath.Dir(cleanPath), 0o750); err != nil {
		return nil, err
	}

	file, err := os.OpenFile(cleanPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, err
	}

	originalStdout := os.Stdout
	originalStderr := os.Stderr

	stdoutReader, stdoutPipe, err := os.Pipe()
	if err != nil {
		_ = file.Close()
		return nil, err
	}
	stderrReader, stderrPipe, err := os.Pipe()
	if err != nil {
		_ = stdoutReader.Close()
		_ = stdoutPipe.Close()
		_ = file.Close()
		return nil, err
	}

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		_, _ = io.Copy(io.MultiWriter(originalStdout, file), stdoutReader)
	}()
	go func() {
		defer wg.Done()
		_, _ = io.Copy(io.MultiWriter(originalStderr, file), stderrReader)
	}()

	os.Stdout = stdoutPipe
	os.Stderr = stderrPipe

	return func() error {
		var closeErr error

		if err := stdoutPipe.Close(); err != nil && closeErr == nil {
			closeErr = err
		}
		if err := stderrPipe.Close(); err != nil && closeErr == nil {
			closeErr = err
		}

		os.Stdout = originalStdout
		os.Stderr = originalStderr

		wg.Wait()

		if err := stdoutReader.Close(); err != nil && closeErr == nil {
			closeErr = err
		}
		if err := stderrReader.Close(); err != nil && closeErr == nil {
			closeErr = err
		}
		if err := file.Close(); err != nil && closeErr == nil {
			closeErr = err
		}

		return closeErr
	}, nil
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

	respBody, err := io.ReadAll(resp.Body)
	closeErr := resp.Body.Close()
	if err != nil {
		return nil, elapsed, err
	}
	if closeErr != nil {
		return nil, elapsed, closeErr
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
	heavy  bool // heavy methods get dedicated concurrent phases
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
	GasUsed      uint64
	Transactions []string
	Addresses    []string
}

type PerBlockTraceSample struct {
	Block   int64
	Txs     int
	GasUsed uint64
	Latency time.Duration
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
	if _, err := fmt.Sscanf(hex, "0x%x", &num); err != nil {
		return 0, fmt.Errorf("parse latest block number %q: %w", hex, err)
	}
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
		GasUsed      string `json:"gasUsed"`
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
	if block.GasUsed != "" {
		if _, err := fmt.Sscanf(block.GasUsed, "0x%x", &info.GasUsed); err != nil {
			return nil, fmt.Errorf("parse block gas used for block %d: %w", blockNum, err)
		}
	}
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

func buildBlockNumbers(latestBlock int64, blockCount int, startBlock, endBlock int64) ([]int64, error) {
	if startBlock > 0 || endBlock > 0 {
		if startBlock <= 0 {
			return nil, fmt.Errorf("start-block is required when selecting an explicit block range")
		}
		if endBlock == 0 {
			endBlock = startBlock
		}
		if startBlock > endBlock {
			return nil, fmt.Errorf("start-block (%d) cannot be greater than end-block (%d)", startBlock, endBlock)
		}
		if startBlock < 1 {
			return nil, fmt.Errorf("start-block must be >= 1")
		}
		if endBlock > latestBlock {
			return nil, fmt.Errorf("end-block (%d) cannot exceed latest block (%d)", endBlock, latestBlock)
		}

		blockNums := make([]int64, 0, endBlock-startBlock+1)
		for blockNum := startBlock; blockNum <= endBlock; blockNum++ {
			blockNums = append(blockNums, blockNum)
		}
		return blockNums, nil
	}

	blockNums := make([]int64, 0, blockCount)
	for i := 0; i < blockCount; i++ {
		blockNum := latestBlock - int64(i)
		if blockNum < 1 {
			break
		}
		blockNums = append(blockNums, blockNum)
	}
	return blockNums, nil
}

func writeLabel(img *image.RGBA, x, y int, text string, col color.Color) {
	d := &font.Drawer{
		Dst:  img,
		Src:  image.NewUniform(col),
		Face: basicfont.Face7x13,
		Dot:  fixed.P(x, y),
	}
	d.DrawString(text)
}

func setPixel(img *image.RGBA, x, y int, col color.Color) {
	if image.Pt(x, y).In(img.Bounds()) {
		img.Set(x, y, col)
	}
}

func drawLine(img *image.RGBA, x1, y1, x2, y2 int, col color.Color) {
	dx := float64(x2 - x1)
	dy := float64(y2 - y1)
	steps := int(math.Max(math.Abs(dx), math.Abs(dy)))
	if steps == 0 {
		setPixel(img, x1, y1, col)
		return
	}
	for i := 0; i <= steps; i++ {
		t := float64(i) / float64(steps)
		x := int(math.Round(float64(x1) + dx*t))
		y := int(math.Round(float64(y1) + dy*t))
		setPixel(img, x, y, col)
	}
}

func fillCircle(img *image.RGBA, cx, cy, r int, col color.Color) {
	for dx := -r; dx <= r; dx++ {
		for dy := -r; dy <= r; dy++ {
			if dx*dx+dy*dy <= r*r {
				setPixel(img, cx+dx, cy+dy, col)
			}
		}
	}
}

func scaleValue(value, minValue, maxValue, start, span float64) float64 {
	if maxValue == minValue {
		return start + span/2
	}
	return start + ((value - minValue) / (maxValue - minValue) * span)
}

func formatTick(value float64) string {
	absValue := math.Abs(value)
	switch {
	case absValue >= 1_000_000_000:
		return fmt.Sprintf("%.1fB", value/1_000_000_000)
	case absValue >= 1_000_000:
		return fmt.Sprintf("%.1fM", value/1_000_000)
	case absValue >= 1_000:
		return fmt.Sprintf("%.1fk", value/1_000)
	case absValue >= 100:
		return fmt.Sprintf("%.0f", value)
	case absValue >= 10:
		return fmt.Sprintf("%.1f", value)
	default:
		return fmt.Sprintf("%.2f", value)
	}
}

func writePlotPNG(path, title, xLabel, yLabel string, points [][2]float64, connectPoints bool) error {
	if len(points) == 0 {
		return nil
	}

	const (
		width        = 1400
		height       = 900
		leftMargin   = 120
		rightMargin  = 40
		topMargin    = 70
		bottomMargin = 110
	)

	img := image.NewRGBA(image.Rect(0, 0, width, height))
	background := color.RGBA{255, 255, 255, 255}
	plotBackground := color.RGBA{250, 250, 252, 255}
	axisColor := color.RGBA{60, 60, 67, 255}
	gridColor := color.RGBA{226, 232, 240, 255}
	seriesColor := color.RGBA{37, 99, 235, 255}
	textColor := color.RGBA{17, 24, 39, 255}

	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			img.Set(x, y, background)
		}
	}

	plotLeft := leftMargin
	plotTop := topMargin
	plotRight := width - rightMargin
	plotBottom := height - bottomMargin
	plotWidth := plotRight - plotLeft
	plotHeight := plotBottom - plotTop

	for y := plotTop; y <= plotBottom; y++ {
		for x := plotLeft; x <= plotRight; x++ {
			img.Set(x, y, plotBackground)
		}
	}

	xMin, xMax := points[0][0], points[0][0]
	yMax := points[0][1]
	for _, point := range points[1:] {
		xMin = min(xMin, point[0])
		xMax = max(xMax, point[0])
		yMax = max(yMax, point[1])
	}
	yMin := 0.0
	if yMax == yMin {
		yMax = yMin + 1
	}
	yMax *= 1.05

	for i := 0; i <= 5; i++ {
		ratio := float64(i) / 5
		yValue := yMin + (yMax-yMin)*ratio
		y := int(math.Round(float64(plotBottom) - ratio*float64(plotHeight)))
		drawLine(img, plotLeft, y, plotRight, y, gridColor)
		writeLabel(img, 12, y+5, formatTick(yValue), textColor)
	}

	for i := 0; i <= 5; i++ {
		ratio := float64(i) / 5
		xValue := xMin + (xMax-xMin)*ratio
		x := int(math.Round(float64(plotLeft) + ratio*float64(plotWidth)))
		drawLine(img, x, plotTop, x, plotBottom, gridColor)
		writeLabel(img, x-20, plotBottom+24, formatTick(xValue), textColor)
	}

	drawLine(img, plotLeft, plotBottom, plotRight, plotBottom, axisColor)
	drawLine(img, plotLeft, plotTop, plotLeft, plotBottom, axisColor)

	scaled := make([]image.Point, 0, len(points))
	for _, point := range points {
		x := int(math.Round(scaleValue(point[0], xMin, xMax, float64(plotLeft), float64(plotWidth))))
		y := int(math.Round(float64(plotBottom) - scaleValue(point[1], yMin, yMax, 0, float64(plotHeight))))
		scaled = append(scaled, image.Pt(x, y))
	}

	if connectPoints {
		for i := 1; i < len(scaled); i++ {
			drawLine(img, scaled[i-1].X, scaled[i-1].Y, scaled[i].X, scaled[i].Y, seriesColor)
		}
	}
	for _, point := range scaled {
		fillCircle(img, point.X, point.Y, 4, seriesColor)
	}

	writeLabel(img, width/2-len(title)*3, 30, title, textColor)
	writeLabel(img, width/2-len(xLabel)*3, height-30, xLabel, textColor)
	writeLabel(img, 20, 30, yLabel, textColor)

	cleanPath := filepath.Clean(path)
	// The output filename is fixed by the caller and joined onto a cleaned plot directory.
	file, err := os.OpenFile(cleanPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if err := png.Encode(file, img); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return nil
}

func writePerBlockTracePlots(plotDir string, samples []PerBlockTraceSample) ([]string, error) {
	plotDir = filepath.Clean(plotDir)
	if err := os.MkdirAll(plotDir, 0o750); err != nil {
		return nil, err
	}

	ordered := append([]PerBlockTraceSample(nil), samples...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].Block < ordered[j].Block })

	blockPoints := make([][2]float64, 0, len(ordered))
	txPoints := make([][2]float64, 0, len(samples))
	gasPoints := make([][2]float64, 0, len(samples))
	for _, sample := range ordered {
		latencyMs := float64(sample.Latency) / float64(time.Millisecond)
		blockPoints = append(blockPoints, [2]float64{float64(sample.Block), latencyMs})
	}
	for _, sample := range samples {
		latencyMs := float64(sample.Latency) / float64(time.Millisecond)
		txPoints = append(txPoints, [2]float64{float64(sample.Txs), latencyMs})
		gasPoints = append(gasPoints, [2]float64{float64(sample.GasUsed), latencyMs})
	}

	var written []string

	blockPath := filepath.Join(plotDir, "latency_vs_block.png")
	if err := writePlotPNG(blockPath, "Block Number vs Debug Trace Latency", "Block number", "Latency (ms)", blockPoints, true); err != nil {
		return nil, err
	}
	written = append(written, blockPath)

	txPath := filepath.Join(plotDir, "latency_vs_txs.png")
	if err := writePlotPNG(txPath, "Transaction Count vs Debug Trace Latency", "Transactions per block", "Latency (ms)", txPoints, false); err != nil {
		return nil, err
	}
	written = append(written, txPath)

	gasPath := filepath.Join(plotDir, "latency_vs_gas.png")
	if err := writePlotPNG(gasPath, "Block Gas Used vs Debug Trace Latency", "Gas used per block", "Latency (ms)", gasPoints, false); err != nil {
		return nil, err
	}
	written = append(written, gasPath)

	return written, nil
}

func main() {
	var (
		endpoint      string
		concurrency   int
		blockCount    int
		startBlock    int64
		endBlock      int64
		requestsPer   int
		methodsFlag   string
		traceDiscover int
		plotDir       string
		outputFile    string
	)
	flag.StringVar(&endpoint, "endpoint", "", "RPC endpoint URL (required)")
	flag.IntVar(&concurrency, "concurrency", 16, "number of concurrent workers")
	flag.IntVar(&blockCount, "blocks", 20, "number of recent blocks to sample")
	flag.Int64Var(&startBlock, "start-block", 0, "explicit starting block number to benchmark (inclusive)")
	flag.Int64Var(&endBlock, "end-block", 0, "explicit ending block number to benchmark (inclusive); defaults to start-block when omitted")
	flag.IntVar(&requestsPer, "requests", 100, "requests per method per phase")
	flag.StringVar(&methodsFlag, "methods", "", "comma-separated methods to run (default: all)")
	flag.IntVar(&traceDiscover, "trace-discover", 5, "txs to trace for storage slot discovery (0 to disable)")
	flag.StringVar(&plotDir, "plot-dir", "", "directory to write per-block trace PNG charts (empty disables plots)")
	flag.StringVar(&outputFile, "output-file", "", "file to write benchmark output to in addition to stdout")
	flag.Parse()

	if endpoint == "" {
		fmt.Fprintf(os.Stderr, "Usage: go run main.go -endpoint <rpc-url> [-concurrency 16] [-blocks 20] [-start-block 100 -end-block 200] [-requests 100] [-methods debug_traceBlockByNumber,eth_getLogs] [-output-file bench.txt]\n")
		os.Exit(1)
	}
	closeOutput, err := configureOutput(outputFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to configure output file: %v\n", err)
		os.Exit(1)
	}
	defer func() {
		if err := closeOutput(); err != nil {
			fmt.Fprintf(os.Stderr, "Failed to close output file: %v\n", err)
		}
	}()

	// =========================================================================
	// Discover recent blocks, transactions, and addresses
	// =========================================================================
	fmt.Printf("RPC Read Benchmark\n")
	fmt.Printf("  endpoint:    %s\n", endpoint)
	fmt.Printf("  concurrency: %d\n", concurrency)
	if startBlock > 0 || endBlock > 0 {
		effectiveEndBlock := endBlock
		if effectiveEndBlock == 0 {
			effectiveEndBlock = startBlock
		}
		fmt.Printf("  range:       %d-%d\n", startBlock, effectiveEndBlock)
	} else {
		fmt.Printf("  blocks:      %d recent blocks\n", blockCount)
	}
	fmt.Printf("  requests:    %d per method per phase\n", requestsPer)
	if outputFile != "" {
		fmt.Printf("  output file: %s\n", filepath.Clean(outputFile))
	}

	fmt.Printf("\n--- Discovering blocks ---\n")
	latestBlock, err := getLatestBlockNumber(endpoint)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to get latest block: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Latest block: %d\n", latestBlock)

	blockNums, err := buildBlockNumbers(latestBlock, blockCount, startBlock, endBlock)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Invalid block selection: %v\n", err)
		os.Exit(1)
	}

	var blocks []*BlockInfo
	var allTxHashes []string
	var allAddresses []string
	addrSeen := make(map[string]bool)

	for _, blockNum := range blockNums {
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
		avgGasPerTx := 0.0
		if len(info.Transactions) > 0 {
			avgGasPerTx = float64(info.GasUsed) / float64(len(info.Transactions))
		}
		fmt.Printf("  block %d: %d txs, gas=%d, avg_gas/tx=%.1f, %d addresses\n",
			blockNum, len(info.Transactions), info.GasUsed, avgGasPerTx, len(info.Addresses))
	}

	if len(blocks) == 0 {
		fmt.Fprintf(os.Stderr, "No blocks discovered\n")
		os.Exit(1)
	}
	if len(allAddresses) == 0 {
		fmt.Fprintf(os.Stderr, "No addresses found in selected blocks\n")
		os.Exit(1)
	}
	fmt.Printf("Discovered %d blocks, %d transactions, %d unique addresses\n",
		len(blocks), len(allTxHashes), len(allAddresses))

	var allStorageSlots []storageSlot

	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	var rngMu sync.Mutex
	randomIntn := func(n int) int {
		rngMu.Lock()
		defer rngMu.Unlock()
		return rng.Intn(n)
	}
	referenceBlock := latestBlock
	if startBlock > 0 || endBlock > 0 {
		referenceBlock = blocks[len(blocks)-1].Number
	}
	referenceHex := fmt.Sprintf("0x%x", referenceBlock)
	randBlock := func() *BlockInfo { return blocks[randomIntn(len(blocks))] }
	randAddr := func() string { return allAddresses[randomIntn(len(allAddresses))] }
	randTxHash := func() string {
		if len(allTxHashes) == 0 {
			return ""
		}
		return allTxHashes[randomIntn(len(allTxHashes))]
	}
	randStorageParams := func() []interface{} {
		if len(allStorageSlots) > 0 {
			s := allStorageSlots[randomIntn(len(allStorageSlots))]
			return []interface{}{s.Address, s.Slot, referenceHex}
		}
		return []interface{}{randAddr(), fmt.Sprintf("0x%064x", randomIntn(10)), referenceHex}
	}
	randLogsParams := func() []interface{} {
		first := randBlock().Number
		second := randBlock().Number
		fromBlock := min(first, second)
		toBlock := max(first, second)
		return []interface{}{map[string]interface{}{
			"fromBlock": fmt.Sprintf("0x%x", fromBlock),
			"toBlock":   fmt.Sprintf("0x%x", toBlock),
		}}
	}

	// =========================================================================
	// Method registry — add new methods here (one line each)
	// =========================================================================
	allMethods := []benchMethod{
		{"debug_traceBlockByNumber", func() []interface{} { return []interface{}{fmt.Sprintf("0x%x", randBlock().Number)} }, 10, true},
		{"debug_traceTransaction", func() []interface{} { return []interface{}{randTxHash()} }, 10, true},
		{"eth_getLogs", func() []interface{} { return randLogsParams() }, 20, true},
		{"eth_getBalance", func() []interface{} { return []interface{}{randAddr(), referenceHex} }, 25, false},
		{"eth_getTransactionCount", func() []interface{} { return []interface{}{randAddr(), referenceHex} }, 15, false},
		{"eth_getCode", func() []interface{} { return []interface{}{randAddr(), referenceHex} }, 15, false},
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
	hasMethod := func(name string) bool {
		for _, m := range allMethods {
			if m.name == name {
				return true
			}
		}
		return false
	}

	if hasMethod("eth_getStorageAt") && traceDiscover > 0 && len(allTxHashes) > 0 {
		fmt.Printf("\n--- Discovering storage slots (tracing %d txs) ---\n", min(traceDiscover, len(allTxHashes)))
		allStorageSlots = discoverStorageSlots(endpoint, allTxHashes, traceDiscover)
		fmt.Printf("Discovered %d unique storage slots\n", len(allStorageSlots))
	}

	fmt.Printf("  reference:   block %d\n", referenceBlock)
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
	if hasMethod("debug_traceBlockByNumber") {
		fmt.Printf("\n--- Per-block trace (1 req per block, %d blocks) ---\n", len(blocks))
		fmt.Printf("  %-12s  %-6s  %-12s  %-12s  %s\n", "BLOCK", "TXS", "GAS_USED", "AVG_GAS/TX", "LATENCY")
		fmt.Printf("  %-12s  %-6s  %-12s  %-12s  %s\n", "-----", "---", "--------", "----------", "-------")
		perBlockStats := &LatencyStats{Method: "debug_traceBlockByNumber"}
		perBlockSamples := make([]PerBlockTraceSample, 0, len(blocks))
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
			avgGasPerTx := 0.0
			if len(b.Transactions) > 0 {
				avgGasPerTx = float64(b.GasUsed) / float64(len(b.Transactions))
			}
			perBlockSamples = append(perBlockSamples, PerBlockTraceSample{
				Block:   b.Number,
				Txs:     len(b.Transactions),
				GasUsed: b.GasUsed,
				Latency: lat,
			})
			fmt.Printf("  %-12d  %-6d  %-12d  %-12.1f  %s%s\n",
				b.Number, len(b.Transactions), b.GasUsed, avgGasPerTx, lat.Round(time.Millisecond), errStr)
		}
		totalTime := time.Duration(0)
		for _, lat := range perBlockStats.Latencies {
			totalTime += lat
		}
		perBlockStats.Duration = totalTime
		printStats("Per-block trace summary", map[string]*LatencyStats{"debug_traceBlockByNumber": perBlockStats})
		if plotDir != "" {
			paths, err := writePerBlockTracePlots(plotDir, perBlockSamples)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Failed to write plots: %v\n", err)
			} else {
				fmt.Printf("\nWrote per-block trace plots:\n")
				for _, path := range paths {
					fmt.Printf("  %s\n", path)
				}
			}
		}
	}

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
		r := randomIntn(totalWeight)
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
