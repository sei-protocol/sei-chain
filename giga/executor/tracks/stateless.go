package tracks

import (
	"sync"
	"time"
)

type Identifiable interface {
	GetID() uint64
}

type StatelessTrack[RawBlock, Block Identifiable] struct {
	inputs      <-chan RawBlock
	outputs     chan<- Block
	processFn   func(RawBlock) Block
	workerCount int
	prevBlock   uint64
}

func NewStatelessTrack[RawBlock, Block Identifiable](
	inputs <-chan RawBlock,
	outputs chan<- Block,
	processFn func(RawBlock) Block,
	workerCount int,
	prevBlock uint64,
) *StatelessTrack[RawBlock, Block] {
	return &StatelessTrack[RawBlock, Block]{inputs, outputs, processFn, workerCount, prevBlock}
}

func (t *StatelessTrack[RawBlock, Block]) Start() {
	// completion signals are used to ensure outputs are in order.
	completionSignals := sync.Map{}
	lastBlockSignal := make(chan struct{}, 1)
	lastBlockSignal <- struct{}{}
	completionSignals.Store(t.prevBlock, lastBlockSignal)
	for range t.workerCount {
		go func() {
			for input := range t.inputs {
				completionSignal := make(chan struct{}, 1)
				completionSignals.Store(input.GetID(), completionSignal)
				output := t.processFn(input)
				prevCompletionSignal, ok := completionSignals.Load(input.GetID() - 1)
				// in practice it's almost impossible for ok == false, but we'll handle it anyway
				for !ok {
					time.Sleep(10 * time.Millisecond)
					prevCompletionSignal, ok = completionSignals.Load(input.GetID() - 1)
				}
				<-prevCompletionSignal.(chan struct{})
				t.outputs <- output
				completionSignal <- struct{}{}
				completionSignals.Delete(input.GetID() - 1)
			}
		}()
	}
}

func (t *StatelessTrack[RawBlock, Block]) Stop() {
	close(t.outputs)
}
