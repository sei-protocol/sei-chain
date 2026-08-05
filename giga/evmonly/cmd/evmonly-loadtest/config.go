package main

import (
	"flag"
	"fmt"
	"math"
	"math/big"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/sei-protocol/sei-chain/giga/evmonly"
	"github.com/sei-protocol/sei-chain/giga/evmonly/cmd/evmonly-loadtest/scenarios"
)

const (
	defaultChainID                  = "1337"
	defaultGasPriceWei              = "1000000000"
	defaultMinGasPriceWei           = "1000000000"
	defaultSenderBalance            = "1000000000000000000"
	defaultTransferValue            = "1"
	defaultERC20Contract            = "0x000000000000000000000000000000000000e20c"
	defaultSnapshotRevertContract   = "0x0000000000000000000000000000000000005a90"
	defaultSnapshotRevertHelper     = "0x0000000000000000000000000000000000005a91"
	defaultMetricsAddr              = "127.0.0.1:9698"
	defaultReportInterval           = 5 * time.Second
	defaultQueueSize                = 64
	defaultTxGasLimit               = 21_000
	defaultERC20TxGasLimit          = 100_000
	defaultSnapshotRevertTxGasLimit = 100_000
	defaultTxsPerBlock              = 1_000
	defaultPersistBuffer            = 4 << 20
	defaultGenesisTimestamp         = scenarios.DefaultGenesisTimestamp
	defaultWorkerCount              = 1
	defaultCoinbaseAddress          = "0x00000000000000000000000000000000000000cb"
	workloadTransfer                = scenarios.WorkloadTransfer
	workloadERC20Transfer           = scenarios.WorkloadERC20Transfer
	workloadSnapshotRevert          = scenarios.WorkloadSnapshotRevert
	resultSinkDiscard               = "discard"
	resultSinkFile                  = "file"
	resultSinkChangeSet             = "changeset"
	resultSinkReceipts              = "receipts"
)

type config struct {
	blocks                 uint64
	txsPerBlock            int
	queueSize              int
	builders               int
	prepareWorkers         int
	parseWorkers           int
	workers                int
	executorWorkers        int
	reportInterval         time.Duration
	metricsAddr            string
	resultSink             string
	resultPoolSize         int
	persistDir             string
	persistSync            bool
	persistBufferSize      int
	persistQueueSize       int
	cpuProfile             string
	heapProfile            string
	traceProfile           string
	workload               string
	chainID                *big.Int
	gasPrice               *big.Int
	minGasPrice            *big.Int
	senderBalance          *big.Int
	transferValue          *big.Int
	txGasLimit             uint64
	blockGasLimit          uint64
	coinbase               common.Address
	erc20Contract          common.Address
	snapshotRevertContract common.Address
	snapshotRevertHelper   common.Address
	fixedRecipient         *common.Address
	recipientConflictRate  float64
	sameSender             bool
	disableGasPriceRule    bool
}

func scenarioConfig(cfg config) scenarios.Config {
	return scenarios.Config{
		TxsPerBlock:            cfg.txsPerBlock,
		ChainID:                cfg.chainID,
		GasPrice:               cfg.gasPrice,
		SenderBalance:          cfg.senderBalance,
		TransferValue:          cfg.transferValue,
		TxGasLimit:             cfg.txGasLimit,
		BlockGasLimit:          cfg.blockGasLimit,
		Coinbase:               cfg.coinbase,
		ERC20Contract:          cfg.erc20Contract,
		SnapshotRevertContract: cfg.snapshotRevertContract,
		SnapshotRevertHelper:   cfg.snapshotRevertHelper,
		FixedRecipient:         cfg.fixedRecipient,
		RecipientConflictRate:  cfg.recipientConflictRate,
		SameSender:             cfg.sameSender,
	}
}

type blockEnvelope struct {
	number  uint64
	request evmonly.BlockRequest
}

type preparedBlockEnvelope struct {
	number uint64
	block  evmonly.PreparedBlock
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
	snapshotRevertContract := fs.String("snapshot-revert-contract", defaultSnapshotRevertContract, "EVM address for the generated snapshot-revert outer contract")
	snapshotRevertHelper := fs.String("snapshot-revert-helper", defaultSnapshotRevertHelper, "EVM address for the generated snapshot-revert helper contract")
	recipient := fs.String("recipient", "", "optional fixed transfer recipient; empty creates one recipient per tx")
	fs.Float64Var(&cfg.recipientConflictRate, "recipient-conflict-rate", 0, "fraction [0,1] of transactions per block paired onto shared recipients; 0 keeps recipients unique")
	fs.BoolVar(&cfg.sameSender, "same-sender", false, "use one sender with sequential nonces for every transaction in a transfer block")

	fs.Uint64Var(&cfg.blocks, "blocks", 0, "number of blocks to prebuild and execute; must be positive")
	fs.IntVar(&cfg.txsPerBlock, "txs-per-block", defaultTxsPerBlock, "transactions generated per block")
	fs.IntVar(&cfg.queueSize, "queue-size", defaultQueueSize, "buffered blocks waiting for executor workers")
	fs.IntVar(&cfg.builders, "builders", runtime.GOMAXPROCS(0), "parallel block builder goroutines")
	fs.IntVar(&cfg.prepareWorkers, "prepare-workers", defaultPrepareWorkers(), "parallel block preparation workers for transaction decode and sender recovery")
	fs.IntVar(&cfg.parseWorkers, "parse-workers", 0, "parallel transaction decode/sender recovery workers inside each prepared block; 0 defaults to 1 when prepare-workers > 1, otherwise GOMAXPROCS")
	fs.IntVar(&cfg.workers, "workers", defaultWorkerCount, "parallel executor workers")
	fs.IntVar(&cfg.executorWorkers, "executor-workers", defaultExecutorWorkers(), "parallel OCC workers inside each executor")
	fs.DurationVar(&cfg.reportInterval, "report-interval", defaultReportInterval, "stdout and rate-gauge reporting interval; 0 disables periodic reports")
	fs.StringVar(&cfg.metricsAddr, "metrics-addr", defaultMetricsAddr, "Prometheus listen address; empty disables HTTP metrics")
	fs.StringVar(&cfg.resultSink, "result-sink", resultSinkDiscard, "result sink mode: discard or file")
	fs.IntVar(&cfg.resultPoolSize, "result-pool-size", 0, "pooled executor BlockResult slots; 0 sizes for in-flight sink results, negative disables pooling")
	fs.StringVar(&cfg.persistDir, "persist-dir", "", "directory for --result-sink=file append-only changeset and receipt files, removed at shutdown")
	fs.BoolVar(&cfg.persistSync, "persist-sync", false, "fsync persistent result files from the async sink writer")
	fs.IntVar(&cfg.persistBufferSize, "persist-buffer-size", defaultPersistBuffer, "buffer size in bytes for --result-sink=file")
	fs.IntVar(&cfg.persistQueueSize, "persist-queue-size", 0, "record queue size for async file persistence; 0 defaults to 2*queue-size")
	fs.StringVar(&cfg.cpuProfile, "cpu-profile", "", "write Go CPU profile to this file; starts after prebuild")
	fs.StringVar(&cfg.heapProfile, "heap-profile", "", "write Go heap profile to this file after execution")
	fs.StringVar(&cfg.traceProfile, "trace-profile", "", "write Go runtime trace to this file; starts after prebuild")
	fs.StringVar(&cfg.workload, "workload", workloadTransfer, "workload type: transfer, erc20-transfer, or snapshot-revert")
	fs.Uint64Var(&cfg.txGasLimit, "tx-gas-limit", defaultTxGasLimit, "gas limit for each generated transaction")
	fs.Uint64Var(&cfg.blockGasLimit, "block-gas-limit", 0, "block gas limit; 0 lets the executor use its maximum")
	fs.BoolVar(&cfg.disableGasPriceRule, "disable-gas-price-rule", false, "disable the executor min-gas-price validity rule")

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
	if !common.IsHexAddress(*snapshotRevertContract) {
		return config{}, fmt.Errorf("snapshot-revert-contract must be a hex EVM address")
	}
	cfg.snapshotRevertContract = common.HexToAddress(*snapshotRevertContract)
	if !common.IsHexAddress(*snapshotRevertHelper) {
		return config{}, fmt.Errorf("snapshot-revert-helper must be a hex EVM address")
	}
	cfg.snapshotRevertHelper = common.HexToAddress(*snapshotRevertHelper)
	cfg.workload = strings.ToLower(strings.TrimSpace(cfg.workload))
	if cfg.workload != workloadTransfer && cfg.workload != workloadERC20Transfer && cfg.workload != workloadSnapshotRevert {
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
	if cfg.workload == workloadSnapshotRevert && !txGasLimitSet {
		cfg.txGasLimit = defaultSnapshotRevertTxGasLimit
	}
	if cfg.blocks == 0 {
		return config{}, fmt.Errorf("blocks must be positive")
	}
	if cfg.txsPerBlock <= 0 {
		return config{}, fmt.Errorf("txs-per-block must be positive")
	}
	if cfg.queueSize <= 0 {
		return config{}, fmt.Errorf("queue-size must be positive")
	}
	if cfg.queueSize > math.MaxInt/2 {
		return config{}, fmt.Errorf("queue-size must be at most %d", math.MaxInt/2)
	}
	if cfg.builders <= 0 {
		return config{}, fmt.Errorf("builders must be positive")
	}
	if cfg.prepareWorkers <= 0 {
		return config{}, fmt.Errorf("prepare-workers must be positive")
	}
	if cfg.parseWorkers < 0 {
		return config{}, fmt.Errorf("parse-workers must be non-negative")
	}
	if cfg.parseWorkers == 0 {
		cfg.parseWorkers = defaultParseWorkers(cfg.prepareWorkers)
	}
	if cfg.workers <= 0 {
		return config{}, fmt.Errorf("workers must be positive")
	}
	if cfg.executorWorkers <= 0 {
		return config{}, fmt.Errorf("executor-workers must be positive")
	}
	if cfg.recipientConflictRate < 0 || cfg.recipientConflictRate > 1 {
		return config{}, fmt.Errorf("recipient-conflict-rate must be between 0 and 1")
	}
	if cfg.fixedRecipient != nil && cfg.recipientConflictRate != 0 {
		return config{}, fmt.Errorf("recipient cannot be combined with recipient-conflict-rate")
	}
	if cfg.workload == workloadSnapshotRevert && cfg.fixedRecipient != nil {
		return config{}, fmt.Errorf("recipient is not supported with snapshot-revert workload")
	}
	if cfg.workload == workloadSnapshotRevert && cfg.recipientConflictRate != 0 {
		return config{}, fmt.Errorf("recipient-conflict-rate is not supported with snapshot-revert workload")
	}
	if cfg.sameSender && cfg.workload != workloadTransfer {
		return config{}, fmt.Errorf("same-sender is only supported with transfer workload")
	}
	if cfg.workload == workloadSnapshotRevert && cfg.snapshotRevertContract == cfg.snapshotRevertHelper {
		return config{}, fmt.Errorf("snapshot-revert-contract and snapshot-revert-helper must differ")
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
	if cfg.resultPoolSize < 0 {
		cfg.resultPoolSize = 0
	} else if cfg.resultPoolSize == 0 {
		cfg.resultPoolSize = defaultResultPoolSize(cfg)
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
	if !cfg.disableGasPriceRule && cfg.gasPrice.Cmp(cfg.minGasPrice) < 0 {
		return config{}, fmt.Errorf("gas-price-wei must be greater than or equal to min-gas-price-wei unless disable-gas-price-rule is set")
	}
	requiredBalance := new(big.Int).Mul(new(big.Int).SetUint64(cfg.txGasLimit), cfg.gasPrice)
	requiredBalanceReason := "max gas cost"
	if cfg.workload == workloadTransfer {
		requiredBalance.Add(requiredBalance, cfg.transferValue)
		requiredBalanceReason = "transfer value plus max gas cost"
	}
	if cfg.sameSender {
		txCount := new(big.Int).SetUint64(uint64(cfg.txsPerBlock)) //nolint:gosec // txsPerBlock is validated as positive above.
		requiredBalance.Mul(requiredBalance, txCount)
		requiredBalanceReason = "all same-sender transactions' transfer value plus max gas cost"
	}
	if cfg.senderBalance.Cmp(requiredBalance) < 0 {
		return config{}, fmt.Errorf("sender-balance-wei must cover %s: need at least %s", requiredBalanceReason, requiredBalance.String())
	}
	return cfg, nil
}

func defaultPrepareWorkers() int {
	return runtime.GOMAXPROCS(0)
}

func defaultParseWorkers(prepareWorkers int) int {
	if prepareWorkers > 1 {
		return 1
	}
	return runtime.GOMAXPROCS(0)
}

func defaultExecutorWorkers() int {
	workers := runtime.GOMAXPROCS(0)
	if workers > 12 {
		return 12
	}
	return workers
}

func defaultResultPoolSize(cfg config) int {
	size := cfg.workers + 1
	if cfg.resultSink == resultSinkFile {
		size += cfg.persistQueueSize
	}
	return size
}

func executorConfig(cfg config) evmonly.Config {
	return evmonly.Config{
		MinGasPrice:          new(big.Int).Set(cfg.minGasPrice),
		DisableGasPriceCheck: cfg.disableGasPriceRule,
		OCCWorkers:           cfg.executorWorkers,
		ParseWorkers:         cfg.parseWorkers,
		BlockResultPoolSize:  cfg.resultPoolSize,
	}
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
