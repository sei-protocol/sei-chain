package operations

import (
	"bytes"
	"encoding/json"
	"fmt"
	"html/template"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
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
	Stats         map[string]traceOperationSummary `json:"stats"`
	LowLevelStats map[string]traceOperationSummary `json:"lowLevelStats"`
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
	LowLevelTotals         []aggregateOp  `json:"lowLevelTotals"`
	StoreTotals            []aggregateOp  `json:"storeTotals"`
	TopTransactions        []txSummary    `json:"topTransactions"`
	TopBlocks              []blockSummary `json:"topBlocks"`
}

type traceReportData struct {
	Summary traceSummary
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
	cmd.Flags().StringVarP(&outputDir, "output-dir", "o", "", "Directory for raw output and generated report")
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
	rawFile, err := os.OpenFile(rawPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
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

	reportPath := filepath.Join(outputDir, "report.html")
	if err := writeTraceHTMLReport(reportPath, traceReportData{Summary: summary}); err != nil {
		return err
	}

	fmt.Printf("Wrote raw profiles to %s\n", rawPath)
	fmt.Printf("Wrote summary to %s\n", summaryPath)
	fmt.Printf("Wrote report to %s\n", reportPath)
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
	lowLevelTotals := map[string]traceOperationSummary{}
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
			for op, stats := range module.LowLevelStats {
				addNamedOp(lowLevelTotals, moduleName+"."+op, stats)
			}
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
	summary.LowLevelTotals = sortedOps(lowLevelTotals, 20)
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

func writeTraceHTMLReport(path string, data traceReportData) error {
	const tpl = `<!DOCTYPE html>
<html>
<head>
  <meta charset="utf-8">
  <title>Trace Profile Report</title>
  <style>
    :root {
      --bg: #0b1020;
      --panel: #121933;
      --panel-soft: #182142;
      --text: #eef2ff;
      --muted: #9fb0d9;
      --border: #2b3766;
      --blue: #5b8cff;
      --cyan: #4fd1c5;
      --green: #68d391;
      --orange: #f6ad55;
      --red: #fc8181;
    }
    * { box-sizing: border-box; }
    body {
      font-family: Inter, Arial, sans-serif;
      margin: 0;
      background: linear-gradient(180deg, #0a0f1f 0%, #10172f 100%);
      color: var(--text);
      padding: 28px;
    }
    h1, h2, h3 { margin: 0 0 10px; }
    h1 { font-size: 28px; }
    h2 { font-size: 20px; margin-top: 28px; }
    .subtle { color: var(--muted); }
    .hero, .section, .card {
      border: 1px solid var(--border);
      background: rgba(18, 25, 51, 0.96);
      border-radius: 14px;
      box-shadow: 0 14px 36px rgba(0, 0, 0, 0.25);
    }
    .hero {
      padding: 22px 24px;
      margin-bottom: 18px;
    }
    .hero-meta {
      display: grid;
      grid-template-columns: repeat(auto-fit, minmax(220px, 1fr));
      gap: 10px 18px;
      margin-top: 12px;
      font-size: 13px;
    }
    .grid {
      display: grid;
      grid-template-columns: repeat(auto-fit, minmax(190px, 1fr));
      gap: 14px;
      margin: 16px 0 22px;
    }
    .card {
      padding: 16px;
      background: rgba(24, 33, 66, 0.96);
    }
    .label {
      color: var(--muted);
      font-size: 11px;
      text-transform: uppercase;
      letter-spacing: 0.08em;
    }
    .value {
      font-size: 24px;
      font-weight: 700;
      margin-top: 8px;
    }
    .section {
      padding: 18px 20px;
      margin-bottom: 18px;
    }
    .insights {
      display: grid;
      grid-template-columns: repeat(auto-fit, minmax(260px, 1fr));
      gap: 14px;
    }
    .insight {
      padding: 16px;
      border-radius: 12px;
      border: 1px solid var(--border);
      background: linear-gradient(180deg, rgba(26,36,72,1) 0%, rgba(18,25,51,1) 100%);
    }
    .insight .title { color: var(--muted); font-size: 12px; text-transform: uppercase; }
    .insight .main { margin-top: 8px; font-size: 18px; font-weight: 700; }
    .insight .detail { margin-top: 6px; color: var(--muted); font-size: 13px; }
    table {
      border-collapse: collapse;
      width: 100%;
      margin-top: 10px;
      margin-bottom: 4px;
      overflow: hidden;
      border-radius: 10px;
    }
    th, td {
      border-bottom: 1px solid rgba(43, 55, 102, 0.65);
      padding: 10px 8px;
      text-align: left;
      font-size: 13px;
      vertical-align: middle;
    }
    th {
      color: var(--muted);
      font-weight: 600;
      background: rgba(255,255,255,0.02);
    }
    tr:last-child td { border-bottom: none; }
    .bar-cell { min-width: 320px; }
    .bar-wrap {
      background: rgba(255,255,255,0.08);
      height: 14px;
      border-radius: 999px;
      overflow: hidden;
    }
    .bar {
      background: linear-gradient(90deg, var(--blue) 0%, var(--cyan) 100%);
      height: 100%;
      border-radius: 999px;
    }
    .mono {
      font-family: Menlo, Monaco, Consolas, monospace;
      font-size: 12px;
      word-break: break-all;
    }
    .footnote {
      color: var(--muted);
      font-size: 12px;
      margin-top: 8px;
    }
  </style>
</head>
<body>
  <div class="hero">
    <h1>debug_traceTransactionProfile Report</h1>
    <div class="subtle">A block-range view of trace latency, historical replay cost, and low-level SS / MVCC / Pebble hotspots.</div>
    <div class="hero-meta">
      <div>Endpoint: <span class="mono">{{ .Summary.Endpoint }}</span></div>
      <div>Block range: {{ .Summary.StartBlock }} to {{ .Summary.EndBlock }}</div>
      <div>Generated at: {{ .Summary.GeneratedAt }}</div>
      <div>Success rate: {{ percentInt .Summary.SuccessCount .Summary.TxCount }}%</div>
    </div>
  </div>

  <div class="grid">
    <div class="card"><div class="label">Transactions</div><div class="value">{{ .Summary.TxCount }}</div></div>
    <div class="card"><div class="label">Successful Profiles</div><div class="value">{{ .Summary.SuccessCount }}</div></div>
    <div class="card"><div class="label">Average Total</div><div class="value">{{ nanos .Summary.AverageTotalNanos }}</div></div>
    <div class="card"><div class="label">Average Historical DB</div><div class="value">{{ nanos .Summary.AverageHistoricalNanos }}</div></div>
    <div class="card"><div class="label">Average Execution</div><div class="value">{{ nanos .Summary.AverageExecutionNanos }}</div></div>
    <div class="card"><div class="label">P50 Total</div><div class="value">{{ nanos .Summary.P50TotalNanos }}</div></div>
    <div class="card"><div class="label">P95 Total</div><div class="value">{{ nanos .Summary.P95TotalNanos }}</div></div>
    <div class="card"><div class="label">P95 Historical DB</div><div class="value">{{ nanos .Summary.P95HistoricalNanos }}</div></div>
  </div>

  <div class="section">
    <h2>Top Takeaways</h2>
    <div class="insights">
      <div class="insight">
        <div class="title">Dominant Phase</div>
        <div class="main">{{ primaryName .Summary.PhaseTotals }}</div>
        <div class="detail">{{ nanos (primaryTotal .Summary.PhaseTotals) }} total</div>
      </div>
      <div class="insight">
        <div class="title">Dominant Low-Level Op</div>
        <div class="main mono">{{ primaryName .Summary.LowLevelTotals }}</div>
        <div class="detail">{{ nanos (primaryTotal .Summary.LowLevelTotals) }} total</div>
      </div>
      <div class="insight">
        <div class="title">Slowest Transaction</div>
        <div class="main mono">{{ primaryTxHash .Summary.TopTransactions }}</div>
        <div class="detail">block {{ primaryTxBlock .Summary.TopTransactions }} · {{ nanos (primaryTxTotal .Summary.TopTransactions) }}</div>
      </div>
      <div class="insight">
        <div class="title">Slowest Block</div>
        <div class="main">{{ primaryBlockNum .Summary.TopBlocks }}</div>
        <div class="detail">{{ nanos (primaryBlockTotal .Summary.TopBlocks) }} across {{ primaryBlockTxs .Summary.TopBlocks }} txs</div>
      </div>
    </div>
  </div>

  <div class="section">
    <h2>Phase Totals</h2>
    <div class="subtle">Which top-level trace phases dominate wall time across the sampled transactions.</div>
    {{ template "ops" .Summary.PhaseTotals }}
  </div>

  <div class="section">
    <h2>Top Low-Level Operations</h2>
    <div class="subtle">Low-level SS / MVCC / Pebble operations like <span class="mono">bank.pebble.first</span> or <span class="mono">slashing.pebble.last</span>.</div>
    {{ template "ops" .Summary.LowLevelTotals }}
  </div>

  <div class="section">
    <h2>Top Store Operations</h2>
    <div class="subtle">Higher-level module store activity, including repeated <span class="mono">*.get</span> and iterator-heavy paths.</div>
    {{ template "ops" .Summary.StoreTotals }}
  </div>

  <div class="section">
    <h2>Slowest Transactions</h2>
    <table>
      <tr><th>Block</th><th>Tx Hash</th><th>Total</th><th>Historical</th><th>Execution</th></tr>
      {{ range .Summary.TopTransactions }}
      <tr>
        <td>{{ .BlockNumber }}</td>
        <td class="mono">{{ .TxHash }}</td>
        <td>{{ nanos .TotalNanos }}</td>
        <td>{{ nanos .HistoricalNanos }}</td>
        <td>{{ nanos .ExecutionNanos }}</td>
      </tr>
      {{ end }}
    </table>
  </div>

  <div class="section">
    <h2>Slowest Blocks</h2>
    <table>
      <tr><th>Block</th><th>Tx Count</th><th>Total Profile Time</th></tr>
      {{ range .Summary.TopBlocks }}
      <tr>
        <td>{{ .BlockNumber }}</td>
        <td>{{ .TxCount }}</td>
        <td>{{ nanos .TotalNanos }}</td>
      </tr>
      {{ end }}
    </table>
    <div class="footnote">Totals shown here come from the trace profile output and are best used as attribution signals, not exact additive wall-clock accounting.</div>
  </div>
</body>
</html>

{{ define "ops" }}
<table>
  <tr><th>Name</th><th>Count</th><th>Total</th><th class="bar-cell">Relative</th></tr>
  {{ $max := maxNanos . }}
  {{ range . }}
  <tr>
    <td class="mono">{{ .Name }}</td>
    <td>{{ .Count }}</td>
    <td>{{ nanos .TotalNanos }}</td>
    <td class="bar-cell">
      <div class="bar-wrap"><div class="bar" style="width: {{ percent .TotalNanos $max }}%;"></div></div>
    </td>
  </tr>
  {{ end }}
</table>
{{ end }}`

	funcs := template.FuncMap{
		"nanos": func(v int64) string {
			return (time.Duration(v) * time.Nanosecond).String()
		},
		"percentInt": func(part, total int) int {
			if total == 0 {
				return 0
			}
			return int(math.Round((float64(part) / float64(total)) * 100))
		},
		"maxNanos": func(ops []aggregateOp) int64 {
			var max int64
			for _, op := range ops {
				if op.TotalNanos > max {
					max = op.TotalNanos
				}
			}
			return max
		},
		"percent": func(v, max int64) string {
			if max <= 0 {
				return "0"
			}
			return strconv.FormatFloat((float64(v)/float64(max))*100, 'f', 1, 64)
		},
		"primaryName": func(ops []aggregateOp) string {
			if len(ops) == 0 {
				return "n/a"
			}
			return ops[0].Name
		},
		"primaryTotal": func(ops []aggregateOp) int64 {
			if len(ops) == 0 {
				return 0
			}
			return ops[0].TotalNanos
		},
		"primaryTxHash": func(txs []txSummary) string {
			if len(txs) == 0 {
				return "n/a"
			}
			return txs[0].TxHash
		},
		"primaryTxBlock": func(txs []txSummary) int64 {
			if len(txs) == 0 {
				return 0
			}
			return txs[0].BlockNumber
		},
		"primaryTxTotal": func(txs []txSummary) int64 {
			if len(txs) == 0 {
				return 0
			}
			return txs[0].TotalNanos
		},
		"primaryBlockNum": func(blocks []blockSummary) int64 {
			if len(blocks) == 0 {
				return 0
			}
			return blocks[0].BlockNumber
		},
		"primaryBlockTotal": func(blocks []blockSummary) int64 {
			if len(blocks) == 0 {
				return 0
			}
			return blocks[0].TotalNanos
		},
		"primaryBlockTxs": func(blocks []blockSummary) int {
			if len(blocks) == 0 {
				return 0
			}
			return blocks[0].TxCount
		},
	}

	t, err := template.New("trace-report").Funcs(funcs).Parse(tpl)
	if err != nil {
		return err
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, data); err != nil {
		return err
	}
	return os.WriteFile(path, buf.Bytes(), 0o600)
}
