package walrussim

import (
	"context"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/time/rate"

	commonmetrics "github.com/sei-protocol/sei-chain/sei-db/common/metrics"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/walrus"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/walrus/statestub"
)

// The subdirectory the engine's pods and retained snapshots live in.
const engineDirName = "walrus"

// The subdirectory the state stub's database and checkpoints live in.
const stubDirName = "statestub"

// How many blocks the producer may run ahead of the writer. A block is a few hundred kilobytes, so a short
// queue is enough to keep the writer fed without letting generation outrun it into memory.
const blockQueueDepth = 8

// WalrusSim drives a walrus engine with a generated workload and checks what it answers.
//
// A producer goroutine generates blocks, the main loop writes them, and reader goroutines issue historical
// reads at random heights. What makes the reads checkable without remembering anything is the workload
// itself: a key's write schedule is invertible, so the expected answer at any block is a handful of integer
// operations.
type WalrusSim struct {
	config   *Config
	context  context.Context
	workload *workload
	engine   walrus.Walrus
	stub     statestub.StateStub

	blocks chan walrus.Block

	// Throttles block production when the config asks for a fixed rate.
	limiter *rate.Limiter

	blocksWritten atomic.Uint64
	keysWritten   atomic.Uint64
	reads         atomic.Uint64
	mismatches    atomic.Uint64

	// Where the writer loop is spending its time. Only the main loop touches it, which is what a
	// PhaseTimer requires.
	phases *commonmetrics.PhaseTimer

	startTime      time.Time
	lastReport     time.Time
	lastSnapshot   time.Time
	readers        sync.WaitGroup
	stopReaders    chan struct{}
	closeOnce      sync.Once
	closeError     error
	stubCommitting bool
}

// NewWalrusSim opens the engine and the state stub a run needs.
func NewWalrusSim(runContext context.Context, config *Config) (*WalrusSim, error) {
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	engine, err := walrus.New(config.WalrusConfig())
	if err != nil {
		return nil, fmt.Errorf("failed to open the engine: %w", err)
	}

	created := &WalrusSim{
		config:         config,
		context:        runContext,
		workload:       newWorkload(config),
		engine:         engine,
		phases:         newMainThreadPhaseTimer(),
		blocks:         make(chan walrus.Block, blockQueueDepth),
		stopReaders:    make(chan struct{}),
		startTime:      time.Now(),
		stubCommitting: config.EnableSnapshots,
	}
	if config.MaxBlocksPerSecond > 0 {
		created.limiter = rate.NewLimiter(rate.Limit(config.MaxBlocksPerSecond), 1)
	}

	if config.EnableSnapshots {
		stub, err := statestub.New(config.StateStubConfig())
		if err != nil {
			_ = engine.Close()
			return nil, fmt.Errorf("failed to open the state stub: %w", err)
		}
		created.stub = stub
	}

	fmt.Printf("Workload: %s\n", created.workload.describe())
	return created, nil
}

// Run drives the benchmark until the configured block count is reached or the context is cancelled.
func (s *WalrusSim) Run() error {
	// The first checkpoint is due one interval from now rather than immediately, so a run does not
	// checkpoint an empty store before it has written anything.
	s.lastSnapshot = time.Now()

	go s.produce()
	s.startReaders()

	// Time spent blocked in the range expression below is time the generator had nothing ready, so the
	// phase is set before the loop and again at the end of each pass.
	s.phases.SetPhase(phaseWaitingForBlocks)
	for block := range s.blocks {
		if s.limiter != nil {
			s.phases.SetPhase(phaseThrottling)
			if err := s.limiter.Wait(s.context); err != nil {
				break
			}
		}
		if err := s.writeBlock(block); err != nil {
			return err
		}
		if s.context.Err() != nil {
			break
		}
		s.phases.SetPhase(phaseWaitingForBlocks)
	}
	s.phases.Reset()

	close(s.stopReaders)
	s.readers.Wait()
	s.report(true)
	return nil
}

// Close flushes the engine and releases everything the run opened.
func (s *WalrusSim) Close() error {
	s.closeOnce.Do(func() {
		if err := s.engine.Close(); err != nil {
			s.closeError = fmt.Errorf("failed to close the engine: %w", err)
		}
		if s.stub != nil {
			if err := s.stub.Close(); err != nil && s.closeError == nil {
				s.closeError = fmt.Errorf("failed to close the state stub: %w", err)
			}
		}
	})
	return s.closeError
}

// produce generates blocks until the run is done, handing them to the writer through a short queue.
func (s *WalrusSim) produce() {
	defer close(s.blocks)

	for number := s.config.FirstBlock; ; number++ {
		if s.config.BlockCount > 0 && number >= s.config.FirstBlock+s.config.BlockCount {
			return
		}
		block := s.workload.block(number)
		select {
		case <-s.context.Done():
			return
		case s.blocks <- block:
		}
	}
}

// writeBlock hands one block to the engine, mirrors it into the state stub, and checkpoints when due.
func (s *WalrusSim) writeBlock(block walrus.Block) error {
	s.phases.SetPhase(phaseAppending)
	if err := s.engine.AppendBlock(block); err != nil {
		return fmt.Errorf("failed to append block %d: %w", block.Number, err)
	}
	if s.stubCommitting {
		s.phases.SetPhase(phaseCommitting)
		if err := s.stub.CommitBlock(block.Number, block.ChangeSets); err != nil {
			return fmt.Errorf("failed to commit block %d to the state stub: %w", block.Number, err)
		}
	}

	keys := len(block.ChangeSets[0].Changeset.Pairs)
	s.blocksWritten.Add(1)
	s.keysWritten.Add(uint64(keys))
	recordBlockWritten(s.config.Name, keys)

	if s.stubCommitting && time.Since(s.lastSnapshot) >= s.snapshotInterval() {
		s.phases.SetPhase(phaseSnapshotting)
		if err := s.takeSnapshot(); err != nil {
			return err
		}
	}

	s.phases.SetPhase(phaseReporting)
	s.report(false)
	return nil
}

// takeSnapshot checkpoints the state stub, hands the checkpoint to the engine, and deletes the copy the stub
// owned.
//
// Deleting it immediately is the point of hard-linking: the engine's reference keeps the data alive, so the
// two sides never have to agree about when a snapshot may go.
func (s *WalrusSim) takeSnapshot() error {
	start := time.Now()

	// A snapshot can only terminate a walk over pods that are already written, so the accumulated blocks are
	// cut into a pod first.
	if err := s.engine.Flush(); err != nil {
		return fmt.Errorf("failed to flush before snapshotting: %w", err)
	}
	directory, blockNumber, err := s.stub.Checkpoint()
	if err != nil {
		return fmt.Errorf("failed to checkpoint the state stub: %w", err)
	}
	if err := s.engine.RetainSnapshot(blockNumber, directory); err != nil {
		return fmt.Errorf("failed to retain the snapshot at block %d: %w", blockNumber, err)
	}
	if err := os.RemoveAll(directory); err != nil {
		return fmt.Errorf("failed to delete the state stub's copy of %s: %w", directory, err)
	}

	s.lastSnapshot = time.Now()
	recordSnapshot(s.config.Name, start)
	return nil
}

// snapshotInterval returns how long to wait between checkpoints.
func (s *WalrusSim) snapshotInterval() time.Duration {
	return time.Duration(s.config.SnapshotIntervalSeconds * float64(time.Second))
}

// report prints a progress line, at most as often as the configured interval.
func (s *WalrusSim) report(force bool) {
	// The queryable range is published every block. Gating a metric on how often a human wants a line of
	// console output would make the dashboard a function of the console settings.
	ok, first, last, err := s.engine.QueryableBounds()
	if err == nil && ok {
		recordQueryable(s.config.Name, first, last)
	}
	if s.stub != nil {
		recordStubPipeline(s.config.Name, s.stub.PendingBlocks())
	}

	interval := time.Duration(s.config.ConsoleUpdateIntervalSeconds * float64(time.Second))
	if !force && (interval <= 0 || time.Since(s.lastReport) < interval) {
		return
	}
	s.lastReport = time.Now()

	elapsed := time.Since(s.startTime)
	blocks := s.blocksWritten.Load()
	keys := s.keysWritten.Load()
	reads := s.reads.Load()
	mismatches := s.mismatches.Load()

	fmt.Printf("\r%s | blocks %d (%.0f/s) | keys %.0f/s | queryable [%d, %d] | reads %d | mismatches %d      ",
		elapsed.Round(time.Second), blocks, float64(blocks)/elapsed.Seconds(),
		float64(keys)/elapsed.Seconds(), first, last, reads, mismatches)
	if force {
		fmt.Println()
	}
}
