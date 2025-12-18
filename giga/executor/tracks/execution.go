package tracks

// Execution track is not parallelizable since it requires the previous block to be executed.
func StartExecutionTrack[Block, Receipt, ChangeSet any](
	blocks <-chan Block,
	receipts chan<- Receipt,
	changeSets chan<- ChangeSet,
	schedulerFn func(Block, chan<- Receipt) ChangeSet,
	commitFn func(ChangeSet),
) {
	go func() {
		for block := range blocks {
			rcs := schedulerFn(block, receipts)
			commitFn(rcs)
			changeSets <- rcs
		}
	}()
}
