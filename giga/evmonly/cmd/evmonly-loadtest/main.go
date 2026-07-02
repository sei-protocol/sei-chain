package main

import (
	"bufio"
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
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/rlp"
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
	defaultERC20Contract   = "0x000000000000000000000000000000000000e20c"
	defaultMetricsAddr     = "127.0.0.1:9698"
	defaultReportInterval  = 5 * time.Second
	defaultQueueSize       = 64
	defaultTxGasLimit      = 21_000
	defaultERC20TxGasLimit = 100_000
	defaultTxsPerBlock     = 1_000
	defaultPersistBuffer   = 4 << 20
	defaultWorkerCount     = 1
	defaultCoinbaseAddress = "0x00000000000000000000000000000000000000cb"
	workloadTransfer       = "transfer"
	workloadERC20Transfer  = "erc20-transfer"
	resultSinkDiscard      = "discard"
	resultSinkFile         = "file"
	resultSinkChangeSet    = "changeset"
	resultSinkReceipts     = "receipts"
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
	resultSink          string
	persistDir          string
	persistSync         bool
	persistBufferSize   int
	persistQueueSize    int
	workload            string
	chainID             *big.Int
	gasPrice            *big.Int
	minGasPrice         *big.Int
	senderBalance       *big.Int
	transferValue       *big.Int
	txGasLimit          uint64
	blockGasLimit       uint64
	coinbase            common.Address
	erc20Contract       common.Address
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
	transferValue := fs.String("transfer-value-wei", defaultTransferValue, "wei or token units transferred by each generated transaction")
	coinbase := fs.String("coinbase", defaultCoinbaseAddress, "block coinbase address")
	erc20Contract := fs.String("erc20-contract", defaultERC20Contract, "EVM address for the generated ERC20 transfer contract")
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
	fs.StringVar(&cfg.resultSink, "result-sink", resultSinkDiscard, "result sink mode: discard or file")
	fs.StringVar(&cfg.persistDir, "persist-dir", "", "directory for --result-sink=file append-only changeset and receipt files")
	fs.BoolVar(&cfg.persistSync, "persist-sync", false, "fsync persistent result files from the async sink writer")
	fs.IntVar(&cfg.persistBufferSize, "persist-buffer-size", defaultPersistBuffer, "buffer size in bytes for --result-sink=file")
	fs.IntVar(&cfg.persistQueueSize, "persist-queue-size", 0, "record queue size for async file persistence; 0 defaults to 2*queue-size")
	fs.StringVar(&cfg.workload, "workload", workloadTransfer, "workload type: transfer or erc20-transfer")
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
	if !common.IsHexAddress(*erc20Contract) {
		return config{}, fmt.Errorf("erc20-contract must be a hex EVM address")
	}
	cfg.erc20Contract = common.HexToAddress(*erc20Contract)
	cfg.workload = strings.ToLower(strings.TrimSpace(cfg.workload))
	if cfg.workload != workloadTransfer && cfg.workload != workloadERC20Transfer {
		return config{}, fmt.Errorf("unsupported workload %q", cfg.workload)
	}
	txGasLimitSet := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == "tx-gas-limit" {
			txGasLimitSet = true
		}
	})
	if cfg.workload == workloadERC20Transfer && !txGasLimitSet {
		cfg.txGasLimit = defaultERC20TxGasLimit
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
	cfg.resultSink = strings.ToLower(strings.TrimSpace(cfg.resultSink))
	if cfg.resultSink != resultSinkDiscard && cfg.resultSink != resultSinkFile {
		return config{}, fmt.Errorf("unsupported result-sink %q", cfg.resultSink)
	}
	if cfg.persistBufferSize <= 0 {
		return config{}, fmt.Errorf("persist-buffer-size must be positive")
	}
	if cfg.persistQueueSize < 0 {
		return config{}, fmt.Errorf("persist-queue-size must be non-negative")
	}
	if cfg.persistQueueSize == 0 {
		cfg.persistQueueSize = 2 * cfg.queueSize
	}
	if cfg.resultSink == resultSinkFile && strings.TrimSpace(cfg.persistDir) == "" {
		return config{}, fmt.Errorf("persist-dir is required when result-sink=file")
	}
	if cfg.txGasLimit == 0 {
		return config{}, fmt.Errorf("tx-gas-limit must be positive")
	}
	if cfg.transferValue.BitLen() > 256 {
		return config{}, fmt.Errorf("transfer-value-wei must fit in uint256")
	}
	if cfg.prebuildBlocks && cfg.blocks == 0 {
		return config{}, fmt.Errorf("prebuild-blocks requires --blocks > 0")
	}
	if !cfg.disableGasPriceRule && cfg.gasPrice.Cmp(cfg.minGasPrice) < 0 {
		return config{}, fmt.Errorf("gas-price-wei must be greater than or equal to min-gas-price-wei unless disable-gas-price-rule is set")
	}
	requiredBalance := new(big.Int).Mul(new(big.Int).SetUint64(cfg.txGasLimit), cfg.gasPrice)
	requiredBalanceReason := "max gas cost"
	if cfg.workload == workloadTransfer {
		requiredBalance.Add(requiredBalance, cfg.transferValue)
		requiredBalanceReason = "transfer value plus max gas cost"
	}
	if cfg.senderBalance.Cmp(requiredBalance) < 0 {
		return config{}, fmt.Errorf("sender-balance-wei must cover %s: need at least %s", requiredBalanceReason, requiredBalance.String())
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

func run(cfg config) (err error) {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	state := newGeneratedState()
	workload, err := newWorkload(cfg, state)
	if err != nil {
		return err
	}
	registry := prometheus.NewRegistry()
	metrics := newLoadMetrics(registry)
	sinks, err := newResultSinks(cfg, metrics)
	if err != nil {
		return err
	}
	defer func() {
		closeStartedAt := time.Now()
		if closeErr := sinks.Close(); closeErr != nil {
			if err != nil {
				fmt.Fprintf(os.Stderr, "evmonly-loadtest: result sink close: %v\n", closeErr)
				return
			}
			err = closeErr
		}
		if cfg.resultSink == resultSinkFile {
			printResultSinkReport(time.Since(closeStartedAt), metrics.snapshot())
		}
	}()
	stopSinkSignalCleanup := cleanupSinksOnContextCancel(ctx, sinks)
	defer stopSinkSignalCleanup()

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
		return runPrebuilt(ctx, cfg, state, workload, sinks, metrics)
	}
	return runStreaming(ctx, cfg, state, workload, sinks, metrics)
}

func cleanupSinksOnContextCancel(ctx context.Context, sinks *resultSinks) func() {
	cleanupCtx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		select {
		case <-ctx.Done():
			if err := sinks.Cleanup(); err != nil {
				fmt.Fprintf(os.Stderr, "evmonly-loadtest: result sink cleanup: %v\n", err)
			}
		case <-cleanupCtx.Done():
		}
	}()
	return func() {
		select {
		case <-ctx.Done():
		default:
			cancel()
		}
		<-done
	}
}

type blockWorkload interface {
	buildBlock(context.Context, uint64) (evmonly.BlockRequest, error)
}

func newWorkload(cfg config, state *generatedState) (blockWorkload, error) {
	switch cfg.workload {
	case workloadTransfer:
		return newTransferWorkload(cfg, state), nil
	case workloadERC20Transfer:
		return newERC20TransferWorkload(cfg, state), nil
	default:
		return nil, fmt.Errorf("unsupported workload %q", cfg.workload)
	}
}

func runStreaming(ctx context.Context, cfg config, state *generatedState, workload blockWorkload, sinks *resultSinks, metrics *loadMetrics) error {
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
			}, evmonly.WithState(state), evmonly.WithResultSink(sinks))
			return executeBlocks(groupCtx, workerID, executor, blocks, metrics)
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

func runPrebuilt(ctx context.Context, cfg config, state *generatedState, workload blockWorkload, sinks *resultSinks, metrics *loadMetrics) error {
	prebuildStartedAt := time.Now()
	prebuilt, err := prebuildBlockRequests(ctx, cfg, workload)
	if err != nil {
		return err
	}
	state.Freeze()
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
			}, evmonly.WithState(state), evmonly.WithResultSink(sinks))
			return executeBlocks(groupCtx, workerID, executor, blocks, metrics)
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

func produceBlocks(ctx context.Context, cfg config, workload blockWorkload, out chan<- blockEnvelope, metrics *loadMetrics) error {
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

func prebuildBlockRequests(ctx context.Context, cfg config, workload blockWorkload) ([]blockEnvelope, error) {
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
			metrics.recordFinished(len(result.Txs), result.GasUsed, result.OCCStats)
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
	for i := 0; i < w.cfg.txsPerBlock; i++ {
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
		Context: blockContext(w.cfg, number),
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

type erc20TransferWorkload struct {
	cfg           config
	state         *generatedState
	signer        ethtypes.Signer
	accountCursor atomic.Uint64
}

var (
	erc20TransferSelector = [4]byte{0xa9, 0x05, 0x9c, 0xbb}
	// Minimal ERC20-like runtime for transfer(address,uint256), with balances at
	// storage slot 0 and a standard Transfer(address,address,uint256) log.
	erc20TransferRuntimeCode = common.FromHex("0x60003560e01c63a9059cbb1460145760006000fd5b60243560043533600052600060205260406000208054831060805780548303905580600052600060205260406000208054830190558160005280337fddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef60206000a3600160005260206000f35b60006000fd")
)

func newERC20TransferWorkload(cfg config, state *generatedState) *erc20TransferWorkload {
	state.SetCode(cfg.erc20Contract, erc20TransferRuntimeCode)
	return &erc20TransferWorkload{
		cfg:    cfg,
		state:  state,
		signer: ethtypes.LatestSignerForChainID(cfg.chainID),
	}
}

func (w *erc20TransferWorkload) buildBlock(ctx context.Context, number uint64) (evmonly.BlockRequest, error) {
	txs := make([][]byte, w.cfg.txsPerBlock)
	for i := 0; i < w.cfg.txsPerBlock; i++ {
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
		w.state.SetState(w.cfg.erc20Contract, erc20BalanceSlot(sender), common.BigToHash(w.cfg.transferValue))
		txs[i] = raw
	}
	return evmonly.BlockRequest{
		Context: blockContext(w.cfg, number),
		Txs:     txs,
	}, nil
}

func (w *erc20TransferWorkload) buildTransferTx(accountIndex uint64) ([]byte, common.Address, error) {
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
		To:       &w.cfg.erc20Contract,
		Value:    new(big.Int),
		Data:     erc20TransferCalldata(recipient, w.cfg.transferValue),
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

func (w *erc20TransferWorkload) recipient(accountIndex uint64) common.Address {
	if w.cfg.fixedRecipient != nil {
		return *w.cfg.fixedRecipient
	}
	return addressFromSeed("sei-evmonly-loadtest-erc20-recipient", accountIndex)
}

func erc20TransferCalldata(recipient common.Address, amount *big.Int) []byte {
	data := make([]byte, 4+32+32)
	copy(data[:4], erc20TransferSelector[:])
	copy(data[4+12:36], recipient.Bytes())
	amount.FillBytes(data[36:68])
	return data
}

func erc20BalanceSlot(owner common.Address) common.Hash {
	var encoded [64]byte
	copy(encoded[12:32], owner.Bytes())
	return crypto.Keccak256Hash(encoded[:])
}

func blockContext(cfg config, number uint64) evmonly.BlockContext {
	return evmonly.BlockContext{
		Number:     number,
		Time:       uint64(time.Now().Unix()),
		GasLimit:   cfg.blockGasLimit,
		ChainID:    new(big.Int).Set(cfg.chainID),
		BaseFee:    big.NewInt(0),
		Coinbase:   cfg.coinbase,
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
	frozen   atomic.Bool
	balances map[common.Address]*big.Int
	nonces   map[common.Address]uint64
	code     map[common.Address][]byte
	storage  map[common.Address]map[common.Hash]common.Hash
}

var _ evmonly.StateReader = (*generatedState)(nil)

var frozenZeroBalance = new(big.Int)

func newGeneratedState() *generatedState {
	return &generatedState{
		balances: map[common.Address]*big.Int{},
		nonces:   map[common.Address]uint64{},
		code:     map[common.Address][]byte{},
		storage:  map[common.Address]map[common.Hash]common.Hash{},
	}
}

func (s *generatedState) Freeze() {
	s.frozen.Store(true)
}

func (s *generatedState) GetBalance(addr common.Address) *big.Int {
	if s.frozen.Load() {
		if balance, ok := s.balances[addr]; ok && balance != nil {
			return balance
		}
		return frozenZeroBalance
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if balance, ok := s.balances[addr]; ok && balance != nil {
		return new(big.Int).Set(balance)
	}
	return new(big.Int)
}

func (s *generatedState) SetBalance(addr common.Address, balance *big.Int) {
	s.requireMutable()
	s.mu.Lock()
	defer s.mu.Unlock()
	if balance == nil {
		s.balances[addr] = new(big.Int)
		return
	}
	s.balances[addr] = new(big.Int).Set(balance)
}

func (s *generatedState) GetNonce(addr common.Address) uint64 {
	if s.frozen.Load() {
		return s.nonces[addr]
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.nonces[addr]
}

func (s *generatedState) SetNonce(addr common.Address, nonce uint64) {
	s.requireMutable()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nonces[addr] = nonce
}

func (s *generatedState) GetCode(addr common.Address) []byte {
	if s.frozen.Load() {
		return cloneBytes(s.code[addr])
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return cloneBytes(s.code[addr])
}

func (s *generatedState) SetCode(addr common.Address, code []byte) {
	s.requireMutable()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.code[addr] = cloneBytes(code)
}

func (s *generatedState) GetState(addr common.Address, key common.Hash) common.Hash {
	if s.frozen.Load() {
		if accountStorage, ok := s.storage[addr]; ok {
			return accountStorage[key]
		}
		return common.Hash{}
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if accountStorage, ok := s.storage[addr]; ok {
		return accountStorage[key]
	}
	return common.Hash{}
}

func (s *generatedState) SetState(addr common.Address, key common.Hash, value common.Hash) {
	s.requireMutable()
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

func (s *generatedState) requireMutable() {
	if s.frozen.Load() {
		panic("generated state is frozen")
	}
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

type changeSetSink interface {
	StoreChangeSet(ctx context.Context, height uint64, changeSet evmonly.StateChangeSet) error
}

type receiptSink interface {
	StoreReceipts(ctx context.Context, height uint64, receipts ethtypes.Receipts) error
}

type resultSinks struct {
	changeSets changeSetSink
	receipts   receiptSink
	close      func() error
	cleanup    func() error
}

var _ evmonly.ResultSink = (*resultSinks)(nil)

func newResultSinks(cfg config, metrics *loadMetrics) (*resultSinks, error) {
	switch cfg.resultSink {
	case resultSinkDiscard:
		return &resultSinks{
			changeSets: discardChangeSetSink{writer: &discardStateWriter{}},
			receipts:   discardReceiptSink{},
		}, nil
	case resultSinkFile:
		return newFileResultSinks(cfg, metrics)
	default:
		return nil, fmt.Errorf("unsupported result-sink %q", cfg.resultSink)
	}
}

func (s *resultSinks) StoreChangeSet(ctx context.Context, height uint64, changeSet evmonly.StateChangeSet) error {
	return s.changeSets.StoreChangeSet(ctx, height, changeSet)
}

func (s *resultSinks) StoreReceipts(ctx context.Context, height uint64, receipts ethtypes.Receipts) error {
	return s.receipts.StoreReceipts(ctx, height, receipts)
}

func (s *resultSinks) Close() error {
	var closeErr error
	if s.close == nil {
		closeErr = nil
	} else {
		closeErr = s.close()
	}
	return errors.Join(closeErr, s.Cleanup())
}

func (s *resultSinks) Cleanup() error {
	if s.cleanup == nil {
		return nil
	}
	return s.cleanup()
}

type discardChangeSetSink struct {
	writer evmonly.StateWriter
}

func (s discardChangeSetSink) StoreChangeSet(_ context.Context, _ uint64, changeSet evmonly.StateChangeSet) error {
	s.writer.ApplyChangeSet(changeSet)
	return nil
}

type discardReceiptSink struct{}

func (discardReceiptSink) StoreReceipts(context.Context, uint64, ethtypes.Receipts) error {
	return nil
}

type fileResultSinks struct {
	changeSetFile *appendRLPFile
	receiptFile   *appendRLPFile
	metrics       *loadMetrics
	cleanupMu     sync.Mutex
	paths         []string
	cleaned       map[string]struct{}
}

func newFileResultSinks(cfg config, metrics *loadMetrics) (*resultSinks, error) {
	if err := os.MkdirAll(cfg.persistDir, 0o755); err != nil {
		return nil, fmt.Errorf("create persist dir %s: %w", cfg.persistDir, err)
	}
	changeSetPath := filepath.Join(cfg.persistDir, "changesets.rlp")
	receiptPath := filepath.Join(cfg.persistDir, "receipts.rlp")
	files := &fileResultSinks{
		metrics: metrics,
		paths:   []string{changeSetPath, receiptPath},
		cleaned: map[string]struct{}{},
	}
	var err error
	files.changeSetFile, err = newAppendRLPFile(changeSetPath, cfg.persistBufferSize, cfg.persistSync)
	if err != nil {
		return nil, err
	}
	files.receiptFile, err = newAppendRLPFile(receiptPath, cfg.persistBufferSize, cfg.persistSync)
	if err != nil {
		return nil, errors.Join(err, files.Close())
	}
	async := newAsyncFileResultSinks(files, cfg.persistQueueSize, metrics)
	return &resultSinks{
		changeSets: async,
		receipts:   async,
		close:      async.Close,
		cleanup:    files.Cleanup,
	}, nil
}

func (s *fileResultSinks) Close() error {
	var errs []error
	if s.changeSetFile != nil {
		errs = append(errs, s.changeSetFile.Close())
	}
	if s.receiptFile != nil {
		errs = append(errs, s.receiptFile.Close())
	}
	errs = append(errs, s.Cleanup())
	return errors.Join(errs...)
}

func (s *fileResultSinks) Cleanup() error {
	s.cleanupMu.Lock()
	defer s.cleanupMu.Unlock()

	var errs []error
	for _, path := range s.paths {
		if _, ok := s.cleaned[path]; ok {
			continue
		}
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			errs = append(errs, fmt.Errorf("remove persist file %s: %w", path, err))
			continue
		}
		s.cleaned[path] = struct{}{}
	}
	return errors.Join(errs...)
}

func (s *fileResultSinks) WriteRecord(kind string, height uint64, value any) error {
	var file *appendRLPFile
	switch kind {
	case resultSinkChangeSet:
		file = s.changeSetFile
	case resultSinkReceipts:
		file = s.receiptFile
	default:
		return fmt.Errorf("unsupported result sink record kind %q", kind)
	}
	startedAt := time.Now()
	bytes, err := file.WriteRecord(height, value)
	elapsed := time.Since(startedAt)
	if s.metrics != nil {
		s.metrics.recordSinkWrite(kind, bytes, elapsed, err == nil)
	}
	return err
}

type resultSinkRecord struct {
	kind   string
	height uint64
	value  any
}

type asyncFileResultSinks struct {
	files    *fileResultSinks
	metrics  *loadMetrics
	records  chan resultSinkRecord
	done     chan struct{}
	closeErr error
	close    sync.Once
	errMu    sync.Mutex
	err      error
}

func newAsyncFileResultSinks(files *fileResultSinks, queueSize int, metrics *loadMetrics) *asyncFileResultSinks {
	s := &asyncFileResultSinks{
		files:   files,
		metrics: metrics,
		records: make(chan resultSinkRecord, queueSize),
		done:    make(chan struct{}),
	}
	if metrics != nil {
		metrics.setSinkQueueCapacity(queueSize)
	}
	go s.run()
	return s
}

func (s *asyncFileResultSinks) StoreChangeSet(ctx context.Context, height uint64, changeSet evmonly.StateChangeSet) error {
	return s.enqueue(ctx, resultSinkRecord{
		kind:   resultSinkChangeSet,
		height: height,
		value:  changeSet,
	})
}

func (s *asyncFileResultSinks) StoreReceipts(ctx context.Context, height uint64, receipts ethtypes.Receipts) error {
	return s.enqueue(ctx, resultSinkRecord{
		kind:   resultSinkReceipts,
		height: height,
		value:  receipts,
	})
}

func (s *asyncFileResultSinks) enqueue(ctx context.Context, record resultSinkRecord) error {
	if err := s.getErr(); err != nil {
		return err
	}
	select {
	case s.records <- record:
		s.recordEnqueued(record.kind)
		return nil
	default:
	}

	startedAt := time.Now()
	select {
	case s.records <- record:
		if s.metrics != nil {
			s.metrics.recordSinkEnqueueWait(time.Since(startedAt))
		}
		s.recordEnqueued(record.kind)
		return nil
	case <-s.done:
		if err := s.getErr(); err != nil {
			return err
		}
		return fmt.Errorf("result sink is closed")
	case <-ctx.Done():
		if s.metrics != nil {
			s.metrics.recordSinkEnqueueWait(time.Since(startedAt))
		}
		return ctx.Err()
	}
}

func (s *asyncFileResultSinks) recordEnqueued(kind string) {
	if s.metrics == nil {
		return
	}
	s.metrics.recordSinkEnqueued(kind)
	s.metrics.setSinkQueued(len(s.records))
}

func (s *asyncFileResultSinks) run() {
	defer close(s.done)
	for record := range s.records {
		if s.metrics != nil {
			s.metrics.setSinkQueued(len(s.records))
		}
		if err := s.files.WriteRecord(record.kind, record.height, record.value); err != nil {
			s.setErr(err)
			return
		}
		if s.metrics != nil {
			s.metrics.setSinkQueued(len(s.records))
		}
	}
	if s.metrics != nil {
		s.metrics.setSinkQueued(0)
	}
}

func (s *asyncFileResultSinks) Close() error {
	s.close.Do(func() {
		close(s.records)
		<-s.done
		if s.metrics != nil {
			s.metrics.setSinkQueued(0)
		}
		s.closeErr = errors.Join(s.getErr(), s.files.Close())
	})
	return s.closeErr
}

func (s *asyncFileResultSinks) setErr(err error) {
	s.errMu.Lock()
	defer s.errMu.Unlock()
	if s.err == nil {
		s.err = err
	}
}

func (s *asyncFileResultSinks) getErr() error {
	s.errMu.Lock()
	defer s.errMu.Unlock()
	return s.err
}

type appendRLPFile struct {
	mu          sync.Mutex
	file        *os.File
	writer      *bufio.Writer
	syncOnWrite bool
	closed      bool
}

func newAppendRLPFile(path string, bufferSize int, syncOnWrite bool) (*appendRLPFile, error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, fmt.Errorf("open persist file %s: %w", path, err)
	}
	return &appendRLPFile{
		file:        file,
		writer:      bufio.NewWriterSize(file, bufferSize),
		syncOnWrite: syncOnWrite,
	}, nil
}

func (f *appendRLPFile) WriteRecord(height uint64, value any) (int, error) {
	payload, err := rlp.EncodeToBytes(value)
	if err != nil {
		return 0, fmt.Errorf("encode rlp record for height %d: %w", height, err)
	}
	var header [16]byte
	binary.BigEndian.PutUint64(header[:8], height)
	binary.BigEndian.PutUint64(header[8:], uint64(len(payload)))

	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed {
		return 0, fmt.Errorf("write record for height %d: persist file is closed", height)
	}
	if _, err := f.writer.Write(header[:]); err != nil {
		return 0, fmt.Errorf("write record header for height %d: %w", height, err)
	}
	if _, err := f.writer.Write(payload); err != nil {
		return 0, fmt.Errorf("write record payload for height %d: %w", height, err)
	}
	if err := f.writer.Flush(); err != nil {
		return 0, fmt.Errorf("flush record for height %d: %w", height, err)
	}
	if f.syncOnWrite {
		if err := f.file.Sync(); err != nil {
			return 0, fmt.Errorf("sync record for height %d: %w", height, err)
		}
	}
	return len(header) + len(payload), nil
}

func (f *appendRLPFile) sync() error {
	if err := f.writer.Flush(); err != nil {
		return err
	}
	if err := f.file.Sync(); err != nil {
		return fmt.Errorf("sync persist file: %w", err)
	}
	return nil
}

func (f *appendRLPFile) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed {
		return nil
	}
	f.closed = true
	return errors.Join(f.sync(), f.file.Close())
}

type loadMetrics struct {
	inputBlocks     atomic.Uint64
	finishedBlocks  atomic.Uint64
	finishedTxs     atomic.Uint64
	gasConsumed     atomic.Uint64
	executionErrors atomic.Uint64
	occAttempts     atomic.Uint64
	occFallbacks    atomic.Uint64
	occConflicts    atomic.Uint64
	sinkEnqueued    atomic.Uint64
	sinkWritten     atomic.Uint64
	sinkBytes       atomic.Uint64
	sinkWaitNanos   atomic.Uint64
	sinkWaitEvents  atomic.Uint64
	sinkWriteNanos  atomic.Uint64
	sinkQueued      atomic.Int64

	inputBlocksTotal     prometheus.Counter
	finishedBlocksTotal  prometheus.Counter
	finishedTxsTotal     prometheus.Counter
	gasConsumedTotal     prometheus.Counter
	executionErrorsTotal prometheus.Counter
	occAttemptsTotal     prometheus.Counter
	occFallbacksTotal    prometheus.Counter
	occConflictsTotal    prometheus.Counter

	occFallbackReasonTotal *prometheus.CounterVec
	occConflictTotal       *prometheus.CounterVec
	sinkEnqueuedTotal      *prometheus.CounterVec
	sinkWrittenTotal       *prometheus.CounterVec
	sinkBytesTotal         *prometheus.CounterVec
	sinkEnqueueWaitTotal   prometheus.Counter
	sinkEnqueueWaitEvents  prometheus.Counter
	sinkWriteSecondsTotal  *prometheus.CounterVec

	inputBlockRate    prometheus.Gauge
	finishedBlockRate prometheus.Gauge
	txRate            prometheus.Gauge
	gasRate           prometheus.Gauge
	queuedBlocks      prometheus.Gauge
	sinkQueuedRecords prometheus.Gauge
	sinkQueueCapacity prometheus.Gauge
}

type metricsSnapshot struct {
	at              time.Time
	inputBlocks     uint64
	finishedBlocks  uint64
	finishedTxs     uint64
	gasConsumed     uint64
	executionErrors uint64
	occAttempts     uint64
	occFallbacks    uint64
	occConflicts    uint64
	sinkEnqueued    uint64
	sinkWritten     uint64
	sinkBytes       uint64
	sinkWaitNanos   uint64
	sinkWaitEvents  uint64
	sinkWriteNanos  uint64
	sinkQueued      int64
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
		occAttemptsTotal: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "evmonly_loadtest_occ_attempts_total",
			Help: "Total blocks executed with optimistic concurrency control.",
		}),
		occFallbacksTotal: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "evmonly_loadtest_occ_fallbacks_total",
			Help: "Total OCC blocks that fell back to sequential execution.",
		}),
		occConflictsTotal: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "evmonly_loadtest_occ_conflicts_total",
			Help: "Total OCC conflict accesses observed before sequential fallback.",
		}),
		occFallbackReasonTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "evmonly_loadtest_occ_fallback_reasons_total",
			Help: "OCC fallback count by reason.",
		}, []string{"reason"}),
		occConflictTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "evmonly_loadtest_occ_conflict_keys_total",
			Help: "OCC conflict accesses by access type, state kind, address, and slot.",
		}, []string{"access", "kind", "address", "slot"}),
		sinkEnqueuedTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "evmonly_loadtest_result_sink_records_enqueued_total",
			Help: "Total records accepted by the result sink.",
		}, []string{"kind"}),
		sinkWrittenTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "evmonly_loadtest_result_sink_records_written_total",
			Help: "Total records written by the result sink.",
		}, []string{"kind"}),
		sinkBytesTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "evmonly_loadtest_result_sink_bytes_written_total",
			Help: "Total bytes written by the result sink, including record framing.",
		}, []string{"kind"}),
		sinkEnqueueWaitTotal: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "evmonly_loadtest_result_sink_enqueue_wait_seconds_total",
			Help: "Total time executor workers spent blocked enqueueing result sink records.",
		}),
		sinkEnqueueWaitEvents: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "evmonly_loadtest_result_sink_enqueue_wait_events_total",
			Help: "Total result sink enqueue attempts that had to wait for queue capacity or cancellation.",
		}),
		sinkWriteSecondsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "evmonly_loadtest_result_sink_write_seconds_total",
			Help: "Total time spent by result sink writers serializing, writing, flushing, and optionally syncing records.",
		}, []string{"kind"}),
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
		sinkQueuedRecords: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "evmonly_loadtest_result_sink_queued_records",
			Help: "Persistent result sink records currently waiting for the async writer.",
		}),
		sinkQueueCapacity: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "evmonly_loadtest_result_sink_queue_capacity",
			Help: "Capacity of the persistent result sink async record queue.",
		}),
	}
	registry.MustRegister(
		m.inputBlocksTotal,
		m.finishedBlocksTotal,
		m.finishedTxsTotal,
		m.gasConsumedTotal,
		m.executionErrorsTotal,
		m.occAttemptsTotal,
		m.occFallbacksTotal,
		m.occConflictsTotal,
		m.occFallbackReasonTotal,
		m.occConflictTotal,
		m.sinkEnqueuedTotal,
		m.sinkWrittenTotal,
		m.sinkBytesTotal,
		m.sinkEnqueueWaitTotal,
		m.sinkEnqueueWaitEvents,
		m.sinkWriteSecondsTotal,
		m.inputBlockRate,
		m.finishedBlockRate,
		m.txRate,
		m.gasRate,
		m.queuedBlocks,
		m.sinkQueuedRecords,
		m.sinkQueueCapacity,
	)
	return m
}

func (m *loadMetrics) recordInput() {
	m.inputBlocks.Add(1)
	m.inputBlocksTotal.Inc()
}

func (m *loadMetrics) recordFinished(txCount int, gasUsed uint64, occStats evmonly.OCCStats) {
	m.finishedBlocks.Add(1)
	m.finishedTxs.Add(uint64(txCount))
	m.gasConsumed.Add(gasUsed)
	m.finishedBlocksTotal.Inc()
	m.finishedTxsTotal.Add(float64(txCount))
	m.gasConsumedTotal.Add(float64(gasUsed))
	m.recordOCC(occStats)
}

func (m *loadMetrics) recordOCC(stats evmonly.OCCStats) {
	if !stats.Attempted {
		return
	}
	m.occAttempts.Add(1)
	m.occAttemptsTotal.Inc()
	if stats.Fallback {
		reason := stats.FallbackReason
		if reason == "" {
			reason = "unknown"
		}
		m.occFallbacks.Add(1)
		m.occFallbacksTotal.Inc()
		m.occFallbackReasonTotal.WithLabelValues(reason).Inc()
	}
	if stats.ConflictCount == 0 {
		return
	}
	m.occConflicts.Add(stats.ConflictCount)
	m.occConflictsTotal.Add(float64(stats.ConflictCount))
	for _, conflict := range stats.ConflictSamples {
		m.occConflictTotal.WithLabelValues(
			conflict.Access,
			conflict.Kind,
			conflict.Address.Hex(),
			conflictSlotLabel(conflict),
		).Add(float64(conflict.Count))
	}
}

func conflictSlotLabel(conflict evmonly.OCCConflictCount) string {
	if conflict.Kind != "storage" {
		return ""
	}
	return conflict.Slot.Hex()
}

func (m *loadMetrics) recordExecutionError() {
	m.executionErrors.Add(1)
	m.executionErrorsTotal.Inc()
}

func (m *loadMetrics) recordSinkEnqueued(kind string) {
	m.sinkEnqueued.Add(1)
	m.sinkEnqueuedTotal.WithLabelValues(kind).Inc()
}

func (m *loadMetrics) recordSinkEnqueueWait(elapsed time.Duration) {
	if elapsed <= 0 {
		return
	}
	m.sinkWaitNanos.Add(uint64(elapsed.Nanoseconds()))
	m.sinkWaitEvents.Add(1)
	m.sinkEnqueueWaitTotal.Add(elapsed.Seconds())
	m.sinkEnqueueWaitEvents.Inc()
}

func (m *loadMetrics) recordSinkWrite(kind string, bytes int, elapsed time.Duration, completed bool) {
	if elapsed > 0 {
		m.sinkWriteNanos.Add(uint64(elapsed.Nanoseconds()))
		m.sinkWriteSecondsTotal.WithLabelValues(kind).Add(elapsed.Seconds())
	}
	if !completed {
		return
	}
	m.sinkWritten.Add(1)
	m.sinkBytes.Add(uint64(bytes))
	m.sinkWrittenTotal.WithLabelValues(kind).Inc()
	m.sinkBytesTotal.WithLabelValues(kind).Add(float64(bytes))
}

func (m *loadMetrics) setSinkQueued(records int) {
	m.sinkQueued.Store(int64(records))
	m.sinkQueuedRecords.Set(float64(records))
}

func (m *loadMetrics) setSinkQueueCapacity(records int) {
	m.sinkQueueCapacity.Set(float64(records))
}

func (m *loadMetrics) snapshot() metricsSnapshot {
	return metricsSnapshot{
		at:              time.Now(),
		inputBlocks:     m.inputBlocks.Load(),
		finishedBlocks:  m.finishedBlocks.Load(),
		finishedTxs:     m.finishedTxs.Load(),
		gasConsumed:     m.gasConsumed.Load(),
		executionErrors: m.executionErrors.Load(),
		occAttempts:     m.occAttempts.Load(),
		occFallbacks:    m.occFallbacks.Load(),
		occConflicts:    m.occConflicts.Load(),
		sinkEnqueued:    m.sinkEnqueued.Load(),
		sinkWritten:     m.sinkWritten.Load(),
		sinkBytes:       m.sinkBytes.Load(),
		sinkWaitNanos:   m.sinkWaitNanos.Load(),
		sinkWaitEvents:  m.sinkWaitEvents.Load(),
		sinkWriteNanos:  m.sinkWriteNanos.Load(),
		sinkQueued:      m.sinkQueued.Load(),
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
			sinkWaitSeconds := float64(curr.sinkWaitNanos-prev.sinkWaitNanos) / float64(time.Second)
			sinkWriteSeconds := float64(curr.sinkWriteNanos-prev.sinkWriteNanos) / float64(time.Second)
			fmt.Printf(
				"input_blocks/s=%.2f finished_blocks/s=%.2f tx/s=%.2f gas/s=%.2f queued_blocks=%d sink_queue=%d sink_enqueue_wait/s=%.6f sink_write/s=%.6f totals(input_blocks=%d finished_blocks=%d txs=%d gas=%d errors=%d occ_attempts=%d occ_fallbacks=%d occ_conflicts=%d sink_enqueued=%d sink_written=%d)\n",
				float64(curr.inputBlocks-prev.inputBlocks)/elapsed,
				float64(curr.finishedBlocks-prev.finishedBlocks)/elapsed,
				float64(curr.finishedTxs-prev.finishedTxs)/elapsed,
				float64(curr.gasConsumed-prev.gasConsumed)/elapsed,
				queued,
				curr.sinkQueued,
				sinkWaitSeconds/elapsed,
				sinkWriteSeconds/elapsed,
				curr.inputBlocks,
				curr.finishedBlocks,
				curr.finishedTxs,
				curr.gasConsumed,
				curr.executionErrors,
				curr.occAttempts,
				curr.occFallbacks,
				curr.occConflicts,
				curr.sinkEnqueued,
				curr.sinkWritten,
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
		"complete elapsed=%s input_blocks=%d finished_blocks=%d txs=%d gas=%d errors=%d occ_attempts=%d occ_fallbacks=%d occ_conflicts=%d sink_queue=%d sink_enqueued=%d sink_written=%d sink_bytes=%d sink_enqueue_wait=%s sink_enqueue_wait_events=%d sink_write=%s avg_input_blocks/s=%.2f avg_finished_blocks/s=%.2f avg_tx/s=%.2f avg_gas/s=%.2f\n",
		snapshot.at.Sub(startedAt).Round(time.Millisecond),
		snapshot.inputBlocks,
		snapshot.finishedBlocks,
		snapshot.finishedTxs,
		snapshot.gasConsumed,
		snapshot.executionErrors,
		snapshot.occAttempts,
		snapshot.occFallbacks,
		snapshot.occConflicts,
		snapshot.sinkQueued,
		snapshot.sinkEnqueued,
		snapshot.sinkWritten,
		snapshot.sinkBytes,
		time.Duration(snapshot.sinkWaitNanos).Round(time.Microsecond),
		snapshot.sinkWaitEvents,
		time.Duration(snapshot.sinkWriteNanos).Round(time.Microsecond),
		float64(snapshot.inputBlocks)/elapsed,
		float64(snapshot.finishedBlocks)/elapsed,
		float64(snapshot.finishedTxs)/elapsed,
		float64(snapshot.gasConsumed)/elapsed,
	)
}

func printResultSinkReport(closeElapsed time.Duration, snapshot metricsSnapshot) {
	fmt.Printf(
		"result sink close elapsed=%s sink_queue=%d sink_enqueued=%d sink_written=%d sink_bytes=%d sink_enqueue_wait=%s sink_enqueue_wait_events=%d sink_write=%s\n",
		closeElapsed.Round(time.Millisecond),
		snapshot.sinkQueued,
		snapshot.sinkEnqueued,
		snapshot.sinkWritten,
		snapshot.sinkBytes,
		time.Duration(snapshot.sinkWaitNanos).Round(time.Microsecond),
		snapshot.sinkWaitEvents,
		time.Duration(snapshot.sinkWriteNanos).Round(time.Microsecond),
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
