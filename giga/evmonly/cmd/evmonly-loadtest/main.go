package main

import (
	"context"
	"crypto/ecdsa"
	"encoding/binary"
	"errors"
	"flag"
	"fmt"
	"math"
	"math/big"
	"net"
	"net/http"
	"os"
	"os/signal"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"golang.org/x/sync/errgroup"
	"golang.org/x/time/rate"

	"github.com/sei-protocol/sei-chain/giga/evmonly"
)

const (
	defaultChainID         = "713715"
	defaultGasPriceWei     = "1000000000"
	defaultMinGasPriceWei  = "1000000000"
	defaultSenderBalance   = "1000000000000000000"
	defaultTransferValue   = "1"
	defaultMetricsAddr     = "127.0.0.1:9698"
	defaultReportInterval  = 5 * time.Second
	defaultQueueSize       = 64
	defaultTxGasLimit      = 21_000
	defaultTxsPerBlock     = 1_000
	defaultWorkerCount     = 1
	defaultCoinbaseAddress = "0x00000000000000000000000000000000000000cb"
)

type config struct {
	blocks              uint64
	txsPerBlock         int
	queueSize           int
	builders            int
	workers             int
	executorWorkers     int
	targetBlocksPerSec  float64
	reportInterval      time.Duration
	metricsAddr         string
	workload            string
	chainID             *big.Int
	gasPrice            *big.Int
	minGasPrice         *big.Int
	senderBalance       *big.Int
	transferValue       *big.Int
	txGasLimit          uint64
	blockGasLimit       uint64
	coinbase            common.Address
	fixedRecipient      *common.Address
	disableGasPriceRule bool
	prebuildBlocks      bool
}

type blockEnvelope struct {
	number  uint64
	request evmonly.BlockRequest
}

func main() {
	cfg, err := parseConfig(os.Args[1:])
	if err != nil {
		fmt.Fprintf(os.Stderr, "evmonly-loadtest: %v\n", err)
		os.Exit(2)
	}
	if err := run(cfg); err != nil {
		fmt.Fprintf(os.Stderr, "evmonly-loadtest: %v\n", err)
		os.Exit(1)
	}
}

func parseConfig(args []string) (config, error) {
	cfg := config{}
	fs := flag.NewFlagSet("evmonly-loadtest", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)

	chainID := fs.String("chain-id", defaultChainID, "EVM chain ID used to sign and execute transactions")
	gasPrice := fs.String("gas-price-wei", defaultGasPriceWei, "legacy transaction gas price in wei")
	minGasPrice := fs.String("min-gas-price-wei", defaultMinGasPriceWei, "executor minimum gas price in wei")
	senderBalance := fs.String("sender-balance-wei", defaultSenderBalance, "generated sender genesis balance in wei")
	transferValue := fs.String("transfer-value-wei", defaultTransferValue, "wei transferred by each generated transaction")
	coinbase := fs.String("coinbase", defaultCoinbaseAddress, "block coinbase address")
	recipient := fs.String("recipient", "", "optional fixed transfer recipient; empty creates one recipient per tx")

	fs.Uint64Var(&cfg.blocks, "blocks", 0, "number of blocks to feed; 0 runs until interrupted")
	fs.IntVar(&cfg.txsPerBlock, "txs-per-block", defaultTxsPerBlock, "transactions generated per block")
	fs.IntVar(&cfg.queueSize, "queue-size", defaultQueueSize, "buffered blocks waiting for executor workers")
	fs.IntVar(&cfg.builders, "builders", runtime.GOMAXPROCS(0), "parallel block builder goroutines")
	fs.IntVar(&cfg.workers, "workers", defaultWorkerCount, "parallel executor workers")
	fs.IntVar(&cfg.executorWorkers, "executor-workers", defaultExecutorWorkers(), "parallel OCC workers inside each executor")
	fs.Float64Var(&cfg.targetBlocksPerSec, "target-blocks-per-sec", 0, "input block rate cap; 0 means unlimited")
	fs.DurationVar(&cfg.reportInterval, "report-interval", defaultReportInterval, "stdout and rate-gauge reporting interval; 0 disables periodic reports")
	fs.StringVar(&cfg.metricsAddr, "metrics-addr", defaultMetricsAddr, "Prometheus listen address; empty disables HTTP metrics")
	fs.StringVar(&cfg.workload, "workload", "transfer", "workload type; currently transfer")
	fs.Uint64Var(&cfg.txGasLimit, "tx-gas-limit", defaultTxGasLimit, "gas limit for each generated transaction")
	fs.Uint64Var(&cfg.blockGasLimit, "block-gas-limit", 0, "block gas limit; 0 lets the executor use its maximum")
	fs.BoolVar(&cfg.disableGasPriceRule, "disable-gas-price-rule", false, "disable the executor min-gas-price validity rule")
	fs.BoolVar(&cfg.prebuildBlocks, "prebuild-blocks", false, "generate all bounded blocks before starting executor workers")

	if err := fs.Parse(args); err != nil {
		return config{}, err
	}
	var err error
	if cfg.chainID, err = parsePositiveBig("chain-id", *chainID); err != nil {
		return config{}, err
	}
	if cfg.gasPrice, err = parseNonNegativeBig("gas-price-wei", *gasPrice); err != nil {
		return config{}, err
	}
	if cfg.minGasPrice, err = parseNonNegativeBig("min-gas-price-wei", *minGasPrice); err != nil {
		return config{}, err
	}
	if cfg.senderBalance, err = parseNonNegativeBig("sender-balance-wei", *senderBalance); err != nil {
		return config{}, err
	}
	if cfg.transferValue, err = parseNonNegativeBig("transfer-value-wei", *transferValue); err != nil {
		return config{}, err
	}
	if !common.IsHexAddress(*coinbase) {
		return config{}, fmt.Errorf("coinbase must be a hex EVM address")
	}
	cfg.coinbase = common.HexToAddress(*coinbase)
	if *recipient != "" {
		if !common.IsHexAddress(*recipient) {
			return config{}, fmt.Errorf("recipient must be a hex EVM address")
		}
		addr := common.HexToAddress(*recipient)
		cfg.fixedRecipient = &addr
	}
	cfg.workload = strings.ToLower(strings.TrimSpace(cfg.workload))
	if cfg.workload != "transfer" {
		return config{}, fmt.Errorf("unsupported workload %q", cfg.workload)
	}
	if cfg.txsPerBlock <= 0 {
		return config{}, fmt.Errorf("txs-per-block must be positive")
	}
	if cfg.queueSize <= 0 {
		return config{}, fmt.Errorf("queue-size must be positive")
	}
	if cfg.builders <= 0 {
		return config{}, fmt.Errorf("builders must be positive")
	}
	if cfg.workers <= 0 {
		return config{}, fmt.Errorf("workers must be positive")
	}
	if cfg.executorWorkers <= 0 {
		return config{}, fmt.Errorf("executor-workers must be positive")
	}
	if cfg.targetBlocksPerSec < 0 {
		return config{}, fmt.Errorf("target-blocks-per-sec must be non-negative")
	}
	if cfg.reportInterval < 0 {
		return config{}, fmt.Errorf("report-interval must be non-negative")
	}
	if cfg.txGasLimit == 0 {
		return config{}, fmt.Errorf("tx-gas-limit must be positive")
	}
	if cfg.prebuildBlocks && cfg.blocks == 0 {
		return config{}, fmt.Errorf("prebuild-blocks requires --blocks > 0")
	}
	if !cfg.disableGasPriceRule && cfg.gasPrice.Cmp(cfg.minGasPrice) < 0 {
		return config{}, fmt.Errorf("gas-price-wei must be greater than or equal to min-gas-price-wei unless disable-gas-price-rule is set")
	}
	requiredBalance := new(big.Int).Mul(new(big.Int).SetUint64(cfg.txGasLimit), cfg.gasPrice)
	requiredBalance.Add(requiredBalance, cfg.transferValue)
	if cfg.senderBalance.Cmp(requiredBalance) < 0 {
		return config{}, fmt.Errorf("sender-balance-wei must cover transfer value plus max gas cost: need at least %s", requiredBalance.String())
	}
	return cfg, nil
}

func defaultExecutorWorkers() int {
	workers := runtime.GOMAXPROCS(0)
	if workers > 12 {
		return 12
	}
	return workers
}

func parsePositiveBig(name, raw string) (*big.Int, error) {
	v, err := parseBig(name, raw)
	if err != nil {
		return nil, err
	}
	if v.Sign() <= 0 {
		return nil, fmt.Errorf("%s must be positive", name)
	}
	return v, nil
}

func parseNonNegativeBig(name, raw string) (*big.Int, error) {
	v, err := parseBig(name, raw)
	if err != nil {
		return nil, err
	}
	if v.Sign() < 0 {
		return nil, fmt.Errorf("%s must be non-negative", name)
	}
	return v, nil
}

func parseBig(name, raw string) (*big.Int, error) {
	v, ok := new(big.Int).SetString(strings.TrimSpace(raw), 10)
	if !ok {
		return nil, fmt.Errorf("%s must be a base-10 integer", name)
	}
	return v, nil
}

func run(cfg config) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	state := newGeneratedState()
	workload := newTransferWorkload(cfg, state)
	registry := prometheus.NewRegistry()
	metrics := newLoadMetrics(registry)

	var server *metricsServer
	if cfg.metricsAddr != "" {
		var err error
		server, err = startMetricsServer(cfg.metricsAddr, registry)
		if err != nil {
			return err
		}
		defer func() {
			if err := server.stop(3 * time.Second); err != nil {
				fmt.Fprintf(os.Stderr, "evmonly-loadtest: metrics server shutdown: %v\n", err)
			}
		}()
		fmt.Printf("metrics listening on http://%s/metrics\n", cfg.metricsAddr)
	}

	if cfg.prebuildBlocks {
		return runPrebuilt(ctx, cfg, state, workload, metrics)
	}
	return runStreaming(ctx, cfg, state, workload, metrics)
}

func runStreaming(ctx context.Context, cfg config, state *generatedState, workload *transferWorkload, metrics *loadMetrics) error {
	blocks := make(chan blockEnvelope, cfg.queueSize)
	reportCtx, stopReporter := context.WithCancel(ctx)
	reportDone := make(chan struct{})
	go func() {
		defer close(reportDone)
		reportLoop(reportCtx, cfg.reportInterval, metrics, blocks)
	}()

	startedAt := time.Now()
	group, groupCtx := errgroup.WithContext(ctx)
	group.Go(func() error {
		defer close(blocks)
		return produceBlocks(groupCtx, cfg, workload, blocks, metrics)
	})
	for workerID := 0; workerID < cfg.workers; workerID++ {
		workerID := workerID
		group.Go(func() error {
			executor := evmonly.NewExecutor(evmonly.Config{
				MinGasPrice:          new(big.Int).Set(cfg.minGasPrice),
				DisableGasPriceCheck: cfg.disableGasPriceRule,
				OCCWorkers:           cfg.executorWorkers,
			}, evmonly.WithState(state))
			return executeBlocks(groupCtx, workerID, executor, blocks, &discardStateWriter{}, discardReceiptSink{}, metrics)
		})
	}

	err := group.Wait()
	stopReporter()
	<-reportDone

	if errors.Is(err, context.Canceled) {
		err = nil
	}
	printFinalReport(startedAt, metrics.snapshot())
	return err
}

func runPrebuilt(ctx context.Context, cfg config, state *generatedState, workload *transferWorkload, metrics *loadMetrics) error {
	prebuildStartedAt := time.Now()
	prebuilt, err := prebuildBlockRequests(ctx, cfg, workload)
	if err != nil {
		return err
	}
	prebuildElapsed := time.Since(prebuildStartedAt)
	printPrebuildReport(prebuildElapsed, prebuilt, cfg.txsPerBlock)

	blocks := make(chan blockEnvelope, cfg.queueSize)
	reportCtx, stopReporter := context.WithCancel(ctx)
	reportDone := make(chan struct{})
	go func() {
		defer close(reportDone)
		reportLoop(reportCtx, cfg.reportInterval, metrics, blocks)
	}()

	startedAt := time.Now()
	group, groupCtx := errgroup.WithContext(ctx)
	group.Go(func() error {
		defer close(blocks)
		return feedPrebuiltBlocks(groupCtx, prebuilt, blocks, metrics)
	})
	for workerID := 0; workerID < cfg.workers; workerID++ {
		workerID := workerID
		group.Go(func() error {
			executor := evmonly.NewExecutor(evmonly.Config{
				MinGasPrice:          new(big.Int).Set(cfg.minGasPrice),
				DisableGasPriceCheck: cfg.disableGasPriceRule,
				OCCWorkers:           cfg.executorWorkers,
			}, evmonly.WithState(state))
			return executeBlocks(groupCtx, workerID, executor, blocks, &discardStateWriter{}, discardReceiptSink{}, metrics)
		})
	}

	err = group.Wait()
	stopReporter()
	<-reportDone

	if errors.Is(err, context.Canceled) {
		err = nil
	}
	printFinalReport(startedAt, metrics.snapshot())
	return err
}

func produceBlocks(ctx context.Context, cfg config, workload *transferWorkload, out chan<- blockEnvelope, metrics *loadMetrics) error {
	var limiter *rate.Limiter
	if cfg.targetBlocksPerSec > 0 {
		burst := int(math.Ceil(cfg.targetBlocksPerSec))
		if burst < 1 {
			burst = 1
		}
		limiter = rate.NewLimiter(rate.Limit(cfg.targetBlocksPerSec), burst)
	}

	var nextBlock atomic.Uint64
	group, groupCtx := errgroup.WithContext(ctx)
	for builderID := 0; builderID < cfg.builders; builderID++ {
		group.Go(func() error {
			for {
				number := nextBlock.Add(1)
				if cfg.blocks != 0 && number > cfg.blocks {
					return nil
				}
				if limiter != nil {
					if err := limiter.Wait(groupCtx); err != nil {
						return nil
					}
				}
				request, err := workload.buildBlock(groupCtx, number)
				if err != nil {
					if groupCtx.Err() != nil {
						return nil
					}
					return err
				}
				block := blockEnvelope{number: number, request: request}
				select {
				case out <- block:
					metrics.recordInput()
				case <-groupCtx.Done():
					return nil
				}
			}
		})
	}
	return group.Wait()
}

func prebuildBlockRequests(ctx context.Context, cfg config, workload *transferWorkload) ([]blockEnvelope, error) {
	if cfg.blocks > uint64(maxInt()) {
		return nil, fmt.Errorf("prebuild-blocks cannot allocate %d blocks on this platform", cfg.blocks)
	}
	prebuilt := make([]blockEnvelope, int(cfg.blocks))
	var nextBlock atomic.Uint64
	group, groupCtx := errgroup.WithContext(ctx)
	for builderID := 0; builderID < cfg.builders; builderID++ {
		group.Go(func() error {
			for {
				number := nextBlock.Add(1)
				if number > cfg.blocks {
					return nil
				}
				request, err := workload.buildBlock(groupCtx, number)
				if err != nil {
					if groupCtx.Err() != nil {
						return nil
					}
					return err
				}
				prebuilt[number-1] = blockEnvelope{
					number:  number,
					request: request,
				}
			}
		})
	}
	if err := group.Wait(); err != nil {
		return nil, err
	}
	return prebuilt, nil
}

func feedPrebuiltBlocks(ctx context.Context, prebuilt []blockEnvelope, out chan<- blockEnvelope, metrics *loadMetrics) error {
	for _, block := range prebuilt {
		select {
		case out <- block:
			metrics.recordInput()
		case <-ctx.Done():
			return nil
		}
	}
	return nil
}

func maxInt() int {
	return int(^uint(0) >> 1)
}

func executeBlocks(
	ctx context.Context,
	workerID int,
	executor evmonly.BlockExecutor,
	blocks <-chan blockEnvelope,
	stateWriter evmonly.StateWriter,
	receiptSink receiptSink,
	metrics *loadMetrics,
) error {
	for {
		select {
		case <-ctx.Done():
			return nil
		case block, ok := <-blocks:
			if !ok {
				return nil
			}
			result, err := executor.ExecuteBlock(ctx, block.request)
			if err != nil {
				metrics.recordExecutionError()
				if ctx.Err() != nil {
					return nil
				}
				return fmt.Errorf("worker %d execute block %d: %w", workerID, block.number, err)
			}
			stateWriter.ApplyChangeSet(result.ChangeSet)
			receiptSink.StoreReceipts(block.number, result.Receipts)
			metrics.recordFinished(len(result.Txs), result.GasUsed)
		}
	}
}

type transferWorkload struct {
	cfg           config
	state         *generatedState
	signer        ethtypes.Signer
	accountCursor atomic.Uint64
}

func newTransferWorkload(cfg config, state *generatedState) *transferWorkload {
	return &transferWorkload{
		cfg:    cfg,
		state:  state,
		signer: ethtypes.LatestSignerForChainID(cfg.chainID),
	}
}

func (w *transferWorkload) buildBlock(ctx context.Context, number uint64) (evmonly.BlockRequest, error) {
	txs := make([][]byte, w.cfg.txsPerBlock)
	for i := range txs {
		select {
		case <-ctx.Done():
			return evmonly.BlockRequest{}, ctx.Err()
		default:
		}
		accountIndex := w.accountCursor.Add(1)
		raw, sender, err := w.buildTransferTx(accountIndex)
		if err != nil {
			return evmonly.BlockRequest{}, err
		}
		w.state.SetBalance(sender, w.cfg.senderBalance)
		txs[i] = raw
	}
	return evmonly.BlockRequest{
		Context: w.blockContext(number),
		Txs:     txs,
	}, nil
}

func (w *transferWorkload) buildTransferTx(accountIndex uint64) ([]byte, common.Address, error) {
	key, err := deterministicPrivateKey(accountIndex)
	if err != nil {
		return nil, common.Address{}, err
	}
	sender := crypto.PubkeyToAddress(key.PublicKey)
	recipient := w.recipient(accountIndex)
	tx := ethtypes.NewTx(&ethtypes.LegacyTx{
		Nonce:    0,
		GasPrice: new(big.Int).Set(w.cfg.gasPrice),
		Gas:      w.cfg.txGasLimit,
		To:       &recipient,
		Value:    new(big.Int).Set(w.cfg.transferValue),
	})
	signed, err := ethtypes.SignTx(tx, w.signer, key)
	if err != nil {
		return nil, common.Address{}, err
	}
	raw, err := signed.MarshalBinary()
	if err != nil {
		return nil, common.Address{}, err
	}
	return raw, sender, nil
}

func (w *transferWorkload) recipient(accountIndex uint64) common.Address {
	if w.cfg.fixedRecipient != nil {
		return *w.cfg.fixedRecipient
	}
	return addressFromSeed("sei-evmonly-loadtest-recipient", accountIndex)
}

func (w *transferWorkload) blockContext(number uint64) evmonly.BlockContext {
	return evmonly.BlockContext{
		Number:     number,
		Time:       uint64(time.Now().Unix()),
		GasLimit:   w.cfg.blockGasLimit,
		ChainID:    new(big.Int).Set(w.cfg.chainID),
		BaseFee:    big.NewInt(0),
		Coinbase:   w.cfg.coinbase,
		ParentHash: hashFromSeed("sei-evmonly-loadtest-parent", number-1),
		BlockHash:  hashFromSeed("sei-evmonly-loadtest-block", number),
		PrevRandao: hashFromSeed("sei-evmonly-loadtest-randao", number),
	}
}

func deterministicPrivateKey(index uint64) (*ecdsa.PrivateKey, error) {
	var buf [16]byte
	binary.BigEndian.PutUint64(buf[:8], index)
	for attempt := uint64(0); ; attempt++ {
		binary.BigEndian.PutUint64(buf[8:], attempt)
		key, err := crypto.ToECDSA(crypto.Keccak256([]byte("sei-evmonly-loadtest-sender"), buf[:]))
		if err == nil {
			return key, nil
		}
		if attempt == ^uint64(0) {
			break
		}
	}
	return nil, fmt.Errorf("could not derive private key for account %d", index)
}

func addressFromSeed(prefix string, index uint64) common.Address {
	hash := hashFromSeed(prefix, index)
	return common.BytesToAddress(hash[12:])
}

func hashFromSeed(prefix string, index uint64) common.Hash {
	var buf [8]byte
	binary.BigEndian.PutUint64(buf[:], index)
	return crypto.Keccak256Hash([]byte(prefix), buf[:])
}

type generatedState struct {
	mu       sync.RWMutex
	balances map[common.Address]*big.Int
	nonces   map[common.Address]uint64
	code     map[common.Address][]byte
	storage  map[common.Address]map[common.Hash]common.Hash
}

var _ evmonly.StateReader = (*generatedState)(nil)

func newGeneratedState() *generatedState {
	return &generatedState{
		balances: map[common.Address]*big.Int{},
		nonces:   map[common.Address]uint64{},
		code:     map[common.Address][]byte{},
		storage:  map[common.Address]map[common.Hash]common.Hash{},
	}
}

func (s *generatedState) GetBalance(addr common.Address) *big.Int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if balance, ok := s.balances[addr]; ok && balance != nil {
		return new(big.Int).Set(balance)
	}
	return new(big.Int)
}

func (s *generatedState) SetBalance(addr common.Address, balance *big.Int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if balance == nil {
		s.balances[addr] = new(big.Int)
		return
	}
	s.balances[addr] = new(big.Int).Set(balance)
}

func (s *generatedState) GetNonce(addr common.Address) uint64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.nonces[addr]
}

func (s *generatedState) SetNonce(addr common.Address, nonce uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nonces[addr] = nonce
}

func (s *generatedState) GetCode(addr common.Address) []byte {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return cloneBytes(s.code[addr])
}

func (s *generatedState) SetCode(addr common.Address, code []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.code[addr] = cloneBytes(code)
}

func (s *generatedState) GetState(addr common.Address, key common.Hash) common.Hash {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if accountStorage, ok := s.storage[addr]; ok {
		return accountStorage[key]
	}
	return common.Hash{}
}

func (s *generatedState) SetState(addr common.Address, key common.Hash, value common.Hash) {
	s.mu.Lock()
	defer s.mu.Unlock()
	accountStorage, ok := s.storage[addr]
	if !ok {
		accountStorage = map[common.Hash]common.Hash{}
		s.storage[addr] = accountStorage
	}
	if value == (common.Hash{}) {
		delete(accountStorage, key)
		return
	}
	accountStorage[key] = value
}

func cloneBytes(v []byte) []byte {
	if len(v) == 0 {
		return nil
	}
	return append([]byte(nil), v...)
}

type discardStateWriter struct{}

var _ evmonly.StateWriter = (*discardStateWriter)(nil)

func (*discardStateWriter) ApplyChangeSet(evmonly.StateChangeSet) {}

type receiptSink interface {
	StoreReceipts(height uint64, receipts ethtypes.Receipts)
}

type discardReceiptSink struct{}

func (discardReceiptSink) StoreReceipts(uint64, ethtypes.Receipts) {}

type loadMetrics struct {
	inputBlocks     atomic.Uint64
	finishedBlocks  atomic.Uint64
	finishedTxs     atomic.Uint64
	gasConsumed     atomic.Uint64
	executionErrors atomic.Uint64

	inputBlocksTotal     prometheus.Counter
	finishedBlocksTotal  prometheus.Counter
	finishedTxsTotal     prometheus.Counter
	gasConsumedTotal     prometheus.Counter
	executionErrorsTotal prometheus.Counter

	inputBlockRate    prometheus.Gauge
	finishedBlockRate prometheus.Gauge
	txRate            prometheus.Gauge
	gasRate           prometheus.Gauge
	queuedBlocks      prometheus.Gauge
}

type metricsSnapshot struct {
	at              time.Time
	inputBlocks     uint64
	finishedBlocks  uint64
	finishedTxs     uint64
	gasConsumed     uint64
	executionErrors uint64
}

func newLoadMetrics(registry *prometheus.Registry) *loadMetrics {
	m := &loadMetrics{
		inputBlocksTotal: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "evmonly_loadtest_block_input_total",
			Help: "Total blocks fed to the EVM-only executor input queue.",
		}),
		finishedBlocksTotal: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "evmonly_loadtest_block_finished_total",
			Help: "Total blocks that finished EVM-only executor execution.",
		}),
		finishedTxsTotal: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "evmonly_loadtest_transactions_finished_total",
			Help: "Total transactions that finished EVM-only executor execution.",
		}),
		gasConsumedTotal: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "evmonly_loadtest_gas_consumed_total",
			Help: "Total EVM gas consumed by finished blocks.",
		}),
		executionErrorsTotal: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "evmonly_loadtest_execution_errors_total",
			Help: "Total block execution errors returned by the EVM-only executor.",
		}),
		inputBlockRate: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "evmonly_loadtest_block_input_throughput",
			Help: "Most recent measured block input throughput in blocks per second.",
		}),
		finishedBlockRate: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "evmonly_loadtest_block_finished_throughput",
			Help: "Most recent measured block completion throughput in blocks per second.",
		}),
		txRate: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "evmonly_loadtest_transactions_per_second",
			Help: "Most recent measured transaction execution throughput in transactions per second.",
		}),
		gasRate: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "evmonly_loadtest_gas_consumed_per_second",
			Help: "Most recent measured gas consumption throughput in gas per second.",
		}),
		queuedBlocks: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "evmonly_loadtest_queued_blocks",
			Help: "Blocks currently waiting in the executor input queue.",
		}),
	}
	registry.MustRegister(
		m.inputBlocksTotal,
		m.finishedBlocksTotal,
		m.finishedTxsTotal,
		m.gasConsumedTotal,
		m.executionErrorsTotal,
		m.inputBlockRate,
		m.finishedBlockRate,
		m.txRate,
		m.gasRate,
		m.queuedBlocks,
	)
	return m
}

func (m *loadMetrics) recordInput() {
	m.inputBlocks.Add(1)
	m.inputBlocksTotal.Inc()
}

func (m *loadMetrics) recordFinished(txCount int, gasUsed uint64) {
	m.finishedBlocks.Add(1)
	m.finishedTxs.Add(uint64(txCount))
	m.gasConsumed.Add(gasUsed)
	m.finishedBlocksTotal.Inc()
	m.finishedTxsTotal.Add(float64(txCount))
	m.gasConsumedTotal.Add(float64(gasUsed))
}

func (m *loadMetrics) recordExecutionError() {
	m.executionErrors.Add(1)
	m.executionErrorsTotal.Inc()
}

func (m *loadMetrics) snapshot() metricsSnapshot {
	return metricsSnapshot{
		at:              time.Now(),
		inputBlocks:     m.inputBlocks.Load(),
		finishedBlocks:  m.finishedBlocks.Load(),
		finishedTxs:     m.finishedTxs.Load(),
		gasConsumed:     m.gasConsumed.Load(),
		executionErrors: m.executionErrors.Load(),
	}
}

func (m *loadMetrics) setRates(prev, curr metricsSnapshot, queued int) {
	elapsed := curr.at.Sub(prev.at).Seconds()
	if elapsed <= 0 {
		return
	}
	inputRate := float64(curr.inputBlocks-prev.inputBlocks) / elapsed
	finishedRate := float64(curr.finishedBlocks-prev.finishedBlocks) / elapsed
	txRate := float64(curr.finishedTxs-prev.finishedTxs) / elapsed
	gasRate := float64(curr.gasConsumed-prev.gasConsumed) / elapsed
	m.inputBlockRate.Set(inputRate)
	m.finishedBlockRate.Set(finishedRate)
	m.txRate.Set(txRate)
	m.gasRate.Set(gasRate)
	m.queuedBlocks.Set(float64(queued))
}

func reportLoop(ctx context.Context, interval time.Duration, metrics *loadMetrics, blocks <-chan blockEnvelope) {
	if interval == 0 {
		<-ctx.Done()
		return
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	prev := metrics.snapshot()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			curr := metrics.snapshot()
			queued := len(blocks)
			metrics.setRates(prev, curr, queued)
			elapsed := curr.at.Sub(prev.at).Seconds()
			if elapsed <= 0 {
				prev = curr
				continue
			}
			fmt.Printf(
				"input_blocks/s=%.2f finished_blocks/s=%.2f tx/s=%.2f gas/s=%.2f queued_blocks=%d totals(input_blocks=%d finished_blocks=%d txs=%d gas=%d errors=%d)\n",
				float64(curr.inputBlocks-prev.inputBlocks)/elapsed,
				float64(curr.finishedBlocks-prev.finishedBlocks)/elapsed,
				float64(curr.finishedTxs-prev.finishedTxs)/elapsed,
				float64(curr.gasConsumed-prev.gasConsumed)/elapsed,
				queued,
				curr.inputBlocks,
				curr.finishedBlocks,
				curr.finishedTxs,
				curr.gasConsumed,
				curr.executionErrors,
			)
			prev = curr
		}
	}
}

func printFinalReport(startedAt time.Time, snapshot metricsSnapshot) {
	elapsed := snapshot.at.Sub(startedAt).Seconds()
	if elapsed <= 0 {
		elapsed = 1
	}
	fmt.Printf(
		"complete elapsed=%s input_blocks=%d finished_blocks=%d txs=%d gas=%d errors=%d avg_input_blocks/s=%.2f avg_finished_blocks/s=%.2f avg_tx/s=%.2f avg_gas/s=%.2f\n",
		snapshot.at.Sub(startedAt).Round(time.Millisecond),
		snapshot.inputBlocks,
		snapshot.finishedBlocks,
		snapshot.finishedTxs,
		snapshot.gasConsumed,
		snapshot.executionErrors,
		float64(snapshot.inputBlocks)/elapsed,
		float64(snapshot.finishedBlocks)/elapsed,
		float64(snapshot.finishedTxs)/elapsed,
		float64(snapshot.gasConsumed)/elapsed,
	)
}

func printPrebuildReport(elapsed time.Duration, blocks []blockEnvelope, txsPerBlock int) {
	seconds := elapsed.Seconds()
	if seconds <= 0 {
		seconds = 1
	}
	txCount := len(blocks) * txsPerBlock
	fmt.Printf(
		"prebuild complete elapsed=%s blocks=%d txs=%d build_blocks/s=%.2f build_tx/s=%.2f\n",
		elapsed.Round(time.Millisecond),
		len(blocks),
		txCount,
		float64(len(blocks))/seconds,
		float64(txCount)/seconds,
	)
}

type metricsServer struct {
	server *http.Server
	done   chan error
}

func startMetricsServer(addr string, registry *prometheus.Registry) (*metricsServer, error) {
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("listen for metrics on %s: %w", addr, err)
	}
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.HandlerFor(registry, promhttp.HandlerOpts{}))
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok\n"))
	})
	server := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 3 * time.Second,
	}
	ms := &metricsServer{server: server, done: make(chan error, 1)}
	go func() {
		err := server.Serve(listener)
		if errors.Is(err, http.ErrServerClosed) {
			err = nil
		}
		ms.done <- err
	}()
	return ms, nil
}

func (s *metricsServer) stop(timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	if err := s.server.Shutdown(ctx); err != nil {
		return err
	}
	return <-s.done
}
