package lakesim

import (
	"context"
	"errors"
	"fmt"

	crand "github.com/sei-protocol/sei-chain/sei-db/common/rand"
)

// Lakesim is a write benchmark for the data lake. Each block it writes one value into every lane, each lane being a
// data lake of its own, and deletes blocks that fall out of the retention window.
type Lakesim struct {
	// The benchmark's configuration.
	config *LakesimConfig

	// The benchmark's instruments.
	metrics *LakesimMetrics

	// The lanes, in config.Lanes order.
	lanes []*lane

	// Stops the producer and the lanes.
	cancel context.CancelFunc

	// Closed once the producer and every lane have stopped.
	done chan struct{}

	// The errors that stopped the benchmark, or nil. Set before done is closed, and read only after.
	failure error
}

// NewLakesim opens every lane and starts producing blocks. The benchmark runs until ctx ends, a lane fails, or
// config.MaxBlocks blocks are produced.
func NewLakesim(ctx context.Context, config *LakesimConfig, metrics *LakesimMetrics) (*Lakesim, error) {
	ctx, cancel := context.WithCancel(ctx)
	rand := crand.NewCannedRandom(config.CannedRandomSize, config.Seed)
	lanes := make([]*lane, 0, len(config.Lanes))
	for i := range config.Lanes {
		l, err := newLane(ctx, i, config, rand.Clone(true), metrics)
		if err != nil {
			cancel()
			return nil, errors.Join(fmt.Errorf("open lane %d: %w", i, err), waitForLanes(lanes))
		}
		fmt.Printf("%s resumes at key %d\n", l.Name(), l.NextKey())
		lanes = append(lanes, l)
	}

	s := &Lakesim{
		config:  config,
		metrics: metrics,
		lanes:   lanes,
		cancel:  cancel,
		done:    make(chan struct{}),
	}
	go s.run(ctx)
	return s, nil
}

// Stop asks the benchmark to stop, and returns without waiting for it.
func (s *Lakesim) Stop() {
	s.cancel()
}

// Wait waits for the benchmark to stop and for every lane to make its writes and deletes durable, and returns the
// errors that stopped it, or nil.
func (s *Lakesim) Wait() error {
	<-s.done
	return s.failure
}

// run produces blocks until ctx ends, a lane stops or the block limit is reached, then stops every lane.
func (s *Lakesim) run(ctx context.Context) {
	defer close(s.done)
	s.produceBlocks(ctx)
	s.cancel()
	s.failure = waitForLanes(s.lanes)
}

// produceBlocks hands each block to every lane in turn, at no more than config.BlocksPerSecond, until ctx ends, a lane
// stops or config.MaxBlocks blocks are produced.
func (s *Lakesim) produceBlocks(ctx context.Context) {
	defer s.metrics.EndProducerPhase()
	throttle := newThrottle(s.config.BlocksPerSecond)
	progress := newProgressReporter(s.config.ConsoleUpdateIntervalSeconds)
	for block := uint64(0); s.config.MaxBlocks == 0 || block < s.config.MaxBlocks; block++ {
		s.metrics.SetProducerPhase(phaseThrottle)
		if !throttle.Wait(ctx) {
			return
		}
		s.metrics.SetProducerPhase(phaseBackpressure)
		for _, l := range s.lanes {
			if !l.WriteBlock(ctx) {
				return
			}
		}
		s.metrics.ReportBlock()
		progress.Report(block + 1)
	}
}

// waitForLanes waits for every lane to stop and returns the errors that stopped them.
func waitForLanes(lanes []*lane) error {
	errs := make([]error, 0, len(lanes))
	for _, l := range lanes {
		errs = append(errs, l.Wait())
	}
	return errors.Join(errs...)
}
