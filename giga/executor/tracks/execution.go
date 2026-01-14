package tracks

type ExecutionTrack[Block, Receipt, ChangeSet any] struct {
	blocks      <-chan Block
	receipts    chan<- Receipt
	changeSets  chan<- ChangeSet
	schedulerFn func(Block, chan<- Receipt) ChangeSet
	commitFn    func(ChangeSet)
}

func NewExecutionTrack[Block, Receipt, ChangeSet any](
	blocks <-chan Block,
	receipts chan<- Receipt,
	changeSets chan<- ChangeSet,
	schedulerFn func(Block, chan<- Receipt) ChangeSet,
	commitFn func(ChangeSet),
) *ExecutionTrack[Block, Receipt, ChangeSet] {
	return &ExecutionTrack[Block, Receipt, ChangeSet]{blocks, receipts, changeSets, schedulerFn, commitFn}
}

func (t *ExecutionTrack[Block, Receipt, ChangeSet]) Start() {
	go func() {
		for block := range t.blocks {
			rcs := t.schedulerFn(block, t.receipts)
			t.commitFn(rcs)
			t.changeSets <- rcs
		}
	}()
}

func (t *ExecutionTrack[Block, Receipt, ChangeSet]) Stop() {
	close(t.receipts)
	close(t.changeSets)
}
