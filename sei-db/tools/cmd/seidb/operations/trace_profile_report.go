package operations

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/spf13/cobra"
)

const defaultTraceProfileTimeout = 120 * time.Second

type traceProfileRPCRequest struct {
	JSONRPC string        `json:"jsonrpc"`
	Method  string        `json:"method"`
	Params  []interface{} `json:"params"`
	ID      int64         `json:"id"`
}

type traceProfileRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int64           `json:"id"`
	Result  json.RawMessage `json:"result"`
	Error   *traceRPCError  `json:"error,omitempty"`
}

type traceRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type blockByNumberResult struct {
	Number       string   `json:"number"`
	Hash         string   `json:"hash"`
	Transactions []string `json:"transactions"`
}

type traceProfileResponse struct {
	Trace   json.RawMessage      `json:"trace"`
	Profile traceProfileEnvelope `json:"profile"`
}

type traceProfileEnvelope struct {
	TotalNanos              int64              `json:"totalNanos"`
	HistoricalDBLookupNanos int64              `json:"historicalDbLookupNanos"`
	OtherNanos              int64              `json:"otherNanos"`
	Phases                  traceProfilePhases `json:"phases"`
	Store                   *traceStoreDump    `json:"store,omitempty"`
}

type traceProfilePhases struct {
	LookupTransactionNanos   int64 `json:"lookupTransactionNanos"`
	LoadBlockNanos           int64 `json:"loadBlockNanos"`
	ReplayHistoricalTxsNanos int64 `json:"replayHistoricalTxsNanos"`
	BuildBlockContextNanos   int64 `json:"buildBlockContextNanos"`
	PrepareTxNanos           int64 `json:"prepareTxNanos"`
	ExecutionNanos           int64 `json:"executionNanos"`
	TraceResultNanos         int64 `json:"traceResultNanos"`
}

type traceStoreDump struct {
	Modules map[string]traceStoreModule `json:"modules"`
}

type traceStoreModule struct {
	Stats map[string]traceOperationSummary `json:"stats"`
}

type traceOperationSummary struct {
	Count      int   `json:"count"`
	TotalNanos int64 `json:"totalNanos"`
}

type traceJob struct {
	BlockNumber int64
	BlockHash   string
	TxHash      string
}

type traceRecord struct {
	BlockNumber int64                 `json:"blockNumber"`
	BlockHash   string                `json:"blockHash"`
	TxHash      string                `json:"txHash"`
	Result      *traceProfileResponse `json:"result,omitempty"`
	Error       string                `json:"error,omitempty"`
}

type aggregateOp struct {
	Name       string `json:"name"`
	Count      int    `json:"count"`
	TotalNanos int64  `json:"totalNanos"`
}

type txSummary struct {
	TxHash          string `json:"txHash"`
	BlockNumber     int64  `json:"blockNumber"`
	TotalNanos      int64  `json:"totalNanos"`
	HistoricalNanos int64  `json:"historicalNanos"`
	ExecutionNanos  int64  `json:"executionNanos"`
}

type blockSummary struct {
	BlockNumber int64 `json:"blockNumber"`
	TxCount     int   `json:"txCount"`
	TotalNanos  int64 `json:"totalNanos"`
}

type traceSummary struct {
	Endpoint               string         `json:"endpoint"`
	StartBlock             int64          `json:"startBlock"`
	EndBlock               int64          `json:"endBlock"`
	BlockCount             int            `json:"blockCount"`
	TxCount                int            `json:"txCount"`
	SuccessCount           int            `json:"successCount"`
	ErrorCount             int            `json:"errorCount"`
	GeneratedAt            time.Time      `json:"generatedAt"`
	AverageTotalNanos      int64          `json:"averageTotalNanos"`
	AverageHistoricalNanos int64          `json:"averageHistoricalNanos"`
	AverageExecutionNanos  int64          `json:"averageExecutionNanos"`
	P50TotalNanos          int64          `json:"p50TotalNanos"`
	P95TotalNanos          int64          `json:"p95TotalNanos"`
	P50HistoricalNanos     int64          `json:"p50HistoricalNanos"`
	P95HistoricalNanos     int64          `json:"p95HistoricalNanos"`
	PhaseTotals            []aggregateOp  `json:"phaseTotals"`
	StoreTotals            []aggregateOp  `json:"storeTotals"`
	TopTransactions        []txSummary    `json:"topTransactions"`
	TopBlocks              []blockSummary `json:"topBlocks"`
}

func TraceProfileReportCmd() *cobra.Command {
	var (
		endpoint        string
		startBlock      int64
		endBlock        int64
		outputDir       string
		concurrency     int
		traceConfigJSON string
		maxTransactions int
	)

	cmd := &cobra.Command{
		Use:   "trace-profile-report",
		Short: "Run debug_traceTransactionProfile across a block range and generate a report",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if endpoint == "" {
				return fmt.Errorf("must provide --endpoint")
			}
			if startBlock <= 0 || endBlock <= 0 {
				return fmt.Errorf("must provide positive --start-block and --end-block")
			}
			if endBlock < startBlock {
				return fmt.Errorf("--end-block must be >= --start-block")
			}
			if outputDir == "" {
				return fmt.Errorf("must provide --output-dir")
			}
			if concurrency <= 0 {
				concurrency = 1
			}
			var traceConfig map[string]interface{}
			if traceConfigJSON != "" {
				if err := json.Unmarshal([]byte(traceConfigJSON), &traceConfig); err != nil {
					return fmt.Errorf("invalid --trace-config-json: %w", err)
				}
			} else {
				traceConfig = map[string]interface{}{}
			}
			return runTraceProfileReport(endpoint, startBlock, endBlock, outputDir, concurrency, maxTransactions, traceConfig)
		},
	}

	cmd.Flags().StringVar(&endpoint, "endpoint", "", "RPC endpoint, e.g. http://localhost:8545")
	cmd.Flags().Int64Var(&startBlock, "start-block", 0, "Starting block number")
	cmd.Flags().Int64Var(&endBlock, "end-block", 0, "Ending block number")
	cmd.Flags().StringVarP(&outputDir, "output-dir", "o", "", "Directory for raw_profiles.jsonl and summary.json")
	cmd.Flags().IntVarP(&concurrency, "concurrency", "c", 4, "Concurrent traceTransactionProfile requests")
	cmd.Flags().StringVar(&traceConfigJSON, "trace-config-json", "{}", "JSON object passed as the trace config")
	cmd.Flags().IntVar(&maxTransactions, "max-transactions", 0, "Optional cap on the number of transactions processed")
	return cmd
}

func runTraceProfileReport(endpoint string, startBlock, endBlock int64, outputDir string, concurrency, maxTransactions int, traceConfig map[string]interface{}) error {
	if err := os.MkdirAll(outputDir, 0o750); err != nil {
		return err
	}

	rawPath := filepath.Join(outputDir, "raw_profiles.jsonl")
	rawFile, err := os.OpenFile(rawPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600) //nolint:gosec // outputDir is an operator-supplied CLI flag for this offline seidb tool
	if err != nil {
		return err
	}
	defer func() { _ = rawFile.Close() }()

	jobs, blockCount, err := collectTraceJobs(endpoint, startBlock, endBlock, maxTransactions)
	if err != nil {
		return err
	}
	if len(jobs) == 0 {
		return fmt.Errorf("no transactions found in block range %d-%d", startBlock, endBlock)
	}

	fmt.Printf("Collected %d transactions across %d blocks\n", len(jobs), blockCount)
	results, err := runTraceWorkers(endpoint, jobs, concurrency, traceConfig)
	if err != nil {
		return err
	}

	summary, err := writeAndSummarize(results, rawFile, endpoint, startBlock, endBlock, blockCount)
	if err != nil {
		return err
	}

	summaryPath := filepath.Join(outputDir, "summary.json")
	summaryBytes, err := json.MarshalIndent(summary, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(summaryPath, summaryBytes, 0o600); err != nil {
		return err
	}

	fmt.Printf("Wrote raw profiles to %s\n", rawPath)
	fmt.Printf("Wrote summary to %s\n", summaryPath)
	return nil
}

func collectTraceJobs(endpoint string, startBlock, endBlock int64, maxTransactions int) ([]traceJob, int, error) {
	jobs := make([]traceJob, 0)
	blockCount := 0
	for blockNumber := startBlock; blockNumber <= endBlock; blockNumber++ {
		block, err := fetchBlockByNumber(endpoint, blockNumber)
		if err != nil {
			return nil, 0, err
		}
		blockCount++
		for _, txHash := range block.Transactions {
			jobs = append(jobs, traceJob{
				BlockNumber: blockNumber,
				BlockHash:   block.Hash,
				TxHash:      txHash,
			})
			if maxTransactions > 0 && len(jobs) >= maxTransactions {
				return jobs, blockCount, nil
			}
		}
	}
	return jobs, blockCount, nil
}

func runTraceWorkers(endpoint string, jobs []traceJob, concurrency int, traceConfig map[string]interface{}) ([]traceRecord, error) {
	jobCh := make(chan traceJob)
	resultCh := make(chan traceRecord, len(jobs))
	var wg sync.WaitGroup

	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for job := range jobCh {
				resultCh <- fetchTraceProfile(endpoint, job, traceConfig)
			}
		}()
	}

	go func() {
		for _, job := range jobs {
			jobCh <- job
		}
		close(jobCh)
		wg.Wait()
		close(resultCh)
	}()

	results := make([]traceRecord, 0, len(jobs))
	for record := range resultCh {
		results = append(results, record)
		if len(results)%50 == 0 || len(results) == len(jobs) {
			fmt.Printf("Processed %d/%d transactions\n", len(results), len(jobs))
		}
	}
	sort.Slice(results, func(i, j int) bool {
		if results[i].BlockNumber == results[j].BlockNumber {
			return results[i].TxHash < results[j].TxHash
		}
		return results[i].BlockNumber < results[j].BlockNumber
	})
	return results, nil
}

func writeAndSummarize(results []traceRecord, rawFile *os.File, endpoint string, startBlock, endBlock int64, blockCount int) (traceSummary, error) {
	summary := traceSummary{
		Endpoint:    endpoint,
		StartBlock:  startBlock,
		EndBlock:    endBlock,
		BlockCount:  blockCount,
		GeneratedAt: time.Now().UTC(),
	}

	totalLatencies := make([]int64, 0, len(results))
	historicalLatencies := make([]int64, 0, len(results))
	phaseTotals := map[string]traceOperationSummary{}
	storeTotals := map[string]traceOperationSummary{}
	blockTotals := map[int64]*blockSummary{}
	topTransactions := make([]txSummary, 0, len(results))

	for _, record := range results {
		line, err := json.Marshal(record)
		if err != nil {
			return summary, err
		}
		if _, err := rawFile.Write(append(line, '\n')); err != nil {
			return summary, err
		}

		summary.TxCount++
		block := blockTotals[record.BlockNumber]
		if block == nil {
			block = &blockSummary{BlockNumber: record.BlockNumber}
			blockTotals[record.BlockNumber] = block
		}
		block.TxCount++

		if record.Error != "" || record.Result == nil {
			summary.ErrorCount++
			continue
		}

		summary.SuccessCount++
		p := record.Result.Profile
		totalLatencies = append(totalLatencies, p.TotalNanos)
		historicalLatencies = append(historicalLatencies, p.HistoricalDBLookupNanos)
		summary.AverageTotalNanos += p.TotalNanos
		summary.AverageHistoricalNanos += p.HistoricalDBLookupNanos
		summary.AverageExecutionNanos += p.Phases.ExecutionNanos
		block.TotalNanos += p.TotalNanos
		topTransactions = append(topTransactions, txSummary{
			TxHash:          record.TxHash,
			BlockNumber:     record.BlockNumber,
			TotalNanos:      p.TotalNanos,
			HistoricalNanos: p.HistoricalDBLookupNanos,
			ExecutionNanos:  p.Phases.ExecutionNanos,
		})

		addOp(phaseTotals, "lookupTransaction", p.Phases.LookupTransactionNanos)
		addOp(phaseTotals, "loadBlock", p.Phases.LoadBlockNanos)
		addOp(phaseTotals, "replayHistoricalTxs", p.Phases.ReplayHistoricalTxsNanos)
		addOp(phaseTotals, "buildBlockContext", p.Phases.BuildBlockContextNanos)
		addOp(phaseTotals, "prepareTx", p.Phases.PrepareTxNanos)
		addOp(phaseTotals, "execution", p.Phases.ExecutionNanos)
		addOp(phaseTotals, "traceResult", p.Phases.TraceResultNanos)

		if p.Store == nil {
			continue
		}
		for moduleName, module := range p.Store.Modules {
			for op, stats := range module.Stats {
				addNamedOp(storeTotals, moduleName+"."+op, stats)
			}
		}
	}

	if summary.SuccessCount > 0 {
		summary.AverageTotalNanos /= int64(summary.SuccessCount)
		summary.AverageHistoricalNanos /= int64(summary.SuccessCount)
		summary.AverageExecutionNanos /= int64(summary.SuccessCount)
	}
	summary.P50TotalNanos = percentile(totalLatencies, 0.50)
	summary.P95TotalNanos = percentile(totalLatencies, 0.95)
	summary.P50HistoricalNanos = percentile(historicalLatencies, 0.50)
	summary.P95HistoricalNanos = percentile(historicalLatencies, 0.95)
	summary.PhaseTotals = sortedOps(phaseTotals, 7)
	summary.StoreTotals = sortedOps(storeTotals, 20)
	summary.TopTransactions = topNTxs(topTransactions, 20)
	summary.TopBlocks = topNBlocks(blockTotals, 20)
	return summary, nil
}

func topNTxs(items []txSummary, n int) []txSummary {
	sort.Slice(items, func(i, j int) bool { return items[i].TotalNanos > items[j].TotalNanos })
	if len(items) > n {
		items = items[:n]
	}
	return items
}

func topNBlocks(items map[int64]*blockSummary, n int) []blockSummary {
	out := make([]blockSummary, 0, len(items))
	for _, item := range items {
		out = append(out, *item)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].TotalNanos > out[j].TotalNanos })
	if len(out) > n {
		out = out[:n]
	}
	return out
}

func addOp(m map[string]traceOperationSummary, name string, nanos int64) {
	s := m[name]
	s.Count++
	s.TotalNanos += nanos
	m[name] = s
}

func addNamedOp(m map[string]traceOperationSummary, name string, stats traceOperationSummary) {
	s := m[name]
	s.Count += stats.Count
	s.TotalNanos += stats.TotalNanos
	m[name] = s
}

func sortedOps(m map[string]traceOperationSummary, limit int) []aggregateOp {
	out := make([]aggregateOp, 0, len(m))
	for name, item := range m {
		out = append(out, aggregateOp{Name: name, Count: item.Count, TotalNanos: item.TotalNanos})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].TotalNanos > out[j].TotalNanos })
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}

func percentile(values []int64, pct float64) int64 {
	if len(values) == 0 {
		return 0
	}
	cp := append([]int64(nil), values...)
	sort.Slice(cp, func(i, j int) bool { return cp[i] < cp[j] })
	idx := int(math.Ceil(float64(len(cp))*pct)) - 1
	if idx < 0 {
		idx = 0
	}
	if idx >= len(cp) {
		idx = len(cp) - 1
	}
	return cp[idx]
}

var traceReqID atomic.Int64

func fetchBlockByNumber(endpoint string, blockNumber int64) (*blockByNumberResult, error) {
	var result blockByNumberResult
	if err := doRPC(endpoint, "eth_getBlockByNumber", []interface{}{fmt.Sprintf("0x%x", blockNumber), false}, &result); err != nil {
		return nil, fmt.Errorf("fetch block %d: %w", blockNumber, err)
	}
	return &result, nil
}

func fetchTraceProfile(endpoint string, job traceJob, traceConfig map[string]interface{}) traceRecord {
	record := traceRecord{
		BlockNumber: job.BlockNumber,
		BlockHash:   job.BlockHash,
		TxHash:      job.TxHash,
	}
	var result traceProfileResponse
	if err := doRPC(endpoint, "debug_traceTransactionProfile", []interface{}{job.TxHash, traceConfig}, &result); err != nil {
		record.Error = err.Error()
		return record
	}
	record.Result = &result
	return record
}

func doRPC(endpoint, method string, params []interface{}, out interface{}) error {
	body, err := json.Marshal(traceProfileRPCRequest{
		JSONRPC: "2.0",
		Method:  method,
		Params:  params,
		ID:      traceReqID.Add(1),
	})
	if err != nil {
		return err
	}
	resp, err := (&http.Client{Timeout: defaultTraceProfileTimeout}).Post(endpoint, "application/json", bytes.NewReader(body))
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	var rpcResp traceProfileRPCResponse
	if err := json.NewDecoder(resp.Body).Decode(&rpcResp); err != nil {
		return err
	}
	if rpcResp.Error != nil {
		return fmt.Errorf("rpc error %d: %s", rpcResp.Error.Code, rpcResp.Error.Message)
	}
	if len(rpcResp.Result) == 0 || string(rpcResp.Result) == "null" {
		return fmt.Errorf("empty result for %s", method)
	}
	return json.Unmarshal(rpcResp.Result, out)
}
