package tracks

import (
	"sync"
	"time"
)

type Identifiable interface {
	GetID() uint64
}

// Stateless track is parallelizable since the processing is stateless.
// However, the output order must be in the same order as the input, hence
// the involved signaling logic.
func StartStatelessTrack[RawBlock, Block Identifiable](
	inputs <-chan RawBlock,
	outputs chan<- Block,
	processFn func(RawBlock) Block,
	workerCount int,
	prevBlock uint64,
) {
	// completion signals are used to ensure outputs are in order.
	completionSignals := sync.Map{}
	lastBlockSignal := make(chan struct{}, 1)
	lastBlockSignal <- struct{}{}
	completionSignals.Store(prevBlock, lastBlockSignal)
	for range workerCount {
		go func() {
			for input := range inputs {
				completionSignal := make(chan struct{}, 1)
				completionSignals.Store(input.GetID(), completionSignal)
				output := processFn(input)
				prevCompletionSignal, ok := completionSignals.Load(input.GetID() - 1)
				// in practice it's almost impossible for ok == false, but we'll handle it anyway
				for !ok {
					time.Sleep(10 * time.Millisecond)
					prevCompletionSignal, ok = completionSignals.Load(input.GetID() - 1)
				}
				<-prevCompletionSignal.(chan struct{})
				outputs <- output
				completionSignal <- struct{}{}
				completionSignals.Delete(input.GetID() - 1)
			}
		}()
	}
}
