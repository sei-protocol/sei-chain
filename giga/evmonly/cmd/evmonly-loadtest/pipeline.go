package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/sei-protocol/sei-chain/giga/evmonly"
	"github.com/sei-protocol/sei-chain/giga/evmonly/cmd/evmonly-loadtest/scenarios"
	"golang.org/x/sync/errgroup"
)

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

	return runPrebuilt(ctx, cfg, state, workload, sinks, metrics)
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

type blockWorkload = scenarios.Workload

func newWorkload(cfg config, state *generatedState) (blockWorkload, error) {
	return scenarios.NewWorkload(cfg.workload, scenarioConfig(cfg), state)
}

func runPrebuilt(ctx context.Context, cfg config, state *generatedState, workload blockWorkload, sinks *resultSinks, metrics *loadMetrics) (err error) {
	prebuildStartedAt := time.Now()
	prebuilt, err := prebuildBlockRequests(ctx, cfg, workload)
	if err != nil {
		return err
	}
	state.Freeze()
	prebuildElapsed := time.Since(prebuildStartedAt)
	printPrebuildReport(prebuildElapsed, prebuilt, cfg.txsPerBlock)

	profiles, err := startProfiles(cfg)
	if err != nil {
		return err
	}

	blocks := make(chan blockEnvelope, cfg.queueSize)
	preparedBlocks := make(chan preparedBlockEnvelope, cfg.queueSize)
	reportCtx, stopReporter := context.WithCancel(ctx)
	reportDone := make(chan struct{})
	go func() {
		defer close(reportDone)
		reportLoop(reportCtx, cfg.reportInterval, metrics, preparedBlocks)
	}()

	startedAt := time.Now()
	group, groupCtx := errgroup.WithContext(ctx)
	executor := evmonly.NewExecutor(executorConfig(cfg), evmonly.WithState(state), evmonly.WithResultSink(sinks))
	defer executor.Close()
	metrics.recordResultPoolStats(executor.ResultPoolStats())
	group.Go(func() error {
		defer close(blocks)
		return feedPrebuiltBlocks(groupCtx, prebuilt, blocks, metrics)
	})
	group.Go(func() error {
		defer close(preparedBlocks)
		return prepareBlocks(groupCtx, cfg, executor, blocks, preparedBlocks, metrics)
	})
	for workerID := 0; workerID < cfg.workers; workerID++ {
		workerID := workerID
		group.Go(func() error {
			return executeBlocks(groupCtx, workerID, executor, preparedBlocks, metrics)
		})
	}

	err = group.Wait()
	metrics.recordResultPoolStats(executor.ResultPoolStats())
	stopReporter()
	<-reportDone

	if errors.Is(err, context.Canceled) {
		err = nil
	}
	printFinalReport(startedAt, metrics.snapshot())
	// Drop raw block retention before heap profiling forces a GC.
	prebuilt = nil
	finishProfiles(profiles, &err)
	return err
}

func prebuildBlockRequests(ctx context.Context, cfg config, workload blockWorkload) ([]blockEnvelope, error) {
	blockCount, err := checkedIntFromUint64("blocks", cfg.blocks)
	if err != nil {
		return nil, err
	}
	prebuilt := make([]blockEnvelope, blockCount)
	var nextBlock atomic.Uint64
	group, groupCtx := errgroup.WithContext(ctx)
	for builderID := 0; builderID < cfg.builders; builderID++ {
		group.Go(func() error {
			for {
				number := nextBlock.Add(1)
				if number > cfg.blocks {
					return nil
				}
				request, err := workload.BuildBlock(groupCtx, number)
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

func prepareBlocks(
	ctx context.Context,
	cfg config,
	executor evmonly.PreparedBlockExecutor,
	blocks <-chan blockEnvelope,
	out chan<- preparedBlockEnvelope,
	metrics *loadMetrics,
) error {
	unordered := make(chan preparedBlockEnvelope, cfg.queueSize)
	workerCtx, cancelWorkers := context.WithCancel(ctx)
	defer cancelWorkers()
	group, groupCtx := errgroup.WithContext(workerCtx)
	for workerID := 0; workerID < cfg.prepareWorkers; workerID++ {
		workerID := workerID
		group.Go(func() error {
			for {
				select {
				case <-groupCtx.Done():
					return nil
				case block, ok := <-blocks:
					if !ok {
						return nil
					}
					prepared, err := executor.PrepareBlock(groupCtx, block.request)
					if err != nil {
						if groupCtx.Err() != nil {
							return nil
						}
						metrics.recordPrepareError()
						return fmt.Errorf("prepare worker %d prepare block %d: %w", workerID, block.number, err)
					}
					select {
					case unordered <- preparedBlockEnvelope{number: block.number, block: prepared}:
						metrics.recordPrepared(len(prepared.Txs))
					case <-groupCtx.Done():
						return nil
					}
				}
			}
		})
	}
	done := make(chan error, 1)
	go func() {
		err := group.Wait()
		close(unordered)
		done <- err
	}()
	orderErr := forwardPreparedBlocksInOrder(ctx, unordered, out)
	if orderErr != nil {
		cancelWorkers()
	}
	workerErr := <-done
	if workerErr != nil {
		return errors.Join(workerErr, orderErr)
	}
	if orderErr != nil {
		return orderErr
	}
	return nil
}

func forwardPreparedBlocksInOrder(ctx context.Context, in <-chan preparedBlockEnvelope, out chan<- preparedBlockEnvelope) error {
	next := uint64(1)
	pending := make(map[uint64]preparedBlockEnvelope)
	for {
		for {
			block, ok := pending[next]
			if !ok {
				break
			}
			if err := sendPreparedBlock(ctx, out, block); err != nil {
				return err
			}
			delete(pending, next)
			next++
		}

		select {
		case <-ctx.Done():
			return nil
		case block, ok := <-in:
			if !ok {
				for len(pending) > 0 {
					block, ok := pending[next]
					if !ok {
						if ctx.Err() != nil {
							return nil
						}
						return fmt.Errorf("prepared block stream closed before block %d", next)
					}
					if err := sendPreparedBlock(ctx, out, block); err != nil {
						return err
					}
					delete(pending, next)
					next++
				}
				return nil
			}
			if block.number < next {
				return fmt.Errorf("prepared block %d arrived after block %d", block.number, next-1)
			}
			if block.number == next {
				if err := sendPreparedBlock(ctx, out, block); err != nil {
					return err
				}
				next++
				continue
			}
			if _, exists := pending[block.number]; exists {
				return fmt.Errorf("duplicate prepared block %d", block.number)
			}
			pending[block.number] = block
		}
	}
}

func sendPreparedBlock(ctx context.Context, out chan<- preparedBlockEnvelope, block preparedBlockEnvelope) error {
	select {
	case out <- block:
		return nil
	case <-ctx.Done():
		return nil
	}
}

func checkedIntFromUint64(name string, value uint64) (int, error) {
	if value > maxIntAsUint64() {
		return 0, fmt.Errorf("%s cannot allocate %d blocks on this platform", name, value)
	}
	return int(value), nil //nolint:gosec // checked against max int above.
}

func maxIntAsUint64() uint64 {
	return uint64(^uint(0) >> 1)
}

func uint64FromNonNegativeInt(value int) uint64 {
	if value < 0 {
		panic("negative integer cannot be converted to uint64")
	}
	return uint64(value) //nolint:gosec // negative values are rejected above.
}

func uint64FromNonNegativeInt64(value int64) uint64 {
	if value < 0 {
		return 0
	}
	return uint64(value) //nolint:gosec // negative values are rejected above.
}

func durationFromUint64Nanos(nanos uint64) time.Duration {
	const maxDurationNanos = uint64(1<<63 - 1)
	if nanos > maxDurationNanos {
		return time.Duration(maxDurationNanos) //nolint:gosec // capped to max int64 duration by construction.
	}
	return time.Duration(nanos) //nolint:gosec // capped to max int64 duration above.
}

func executeBlocks(
	ctx context.Context,
	workerID int,
	executor *evmonly.Executor,
	blocks <-chan preparedBlockEnvelope,
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
			result, err := executor.ExecutePreparedBlock(ctx, block.block)
			if err != nil {
				if ctx.Err() != nil {
					return nil
				}
				metrics.recordExecutionError()
				return fmt.Errorf("worker %d execute block %d: %w", workerID, block.number, err)
			}
			metrics.recordFinished(len(result.Txs), result.GasUsed, result.OCCStats)
			result.Release()
			metrics.recordResultPoolStats(executor.ResultPoolStats())
		}
	}
}
