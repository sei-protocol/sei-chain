package tracks

// Receipt sink can be fully parallelized since each receipt is independent of each other.
func StartReceiptSinkTrack[Receipt any](
	receipts <-chan Receipt,
	sinkFn func(Receipt),
	workerCount int,
) {
	for range workerCount {
		go func() {
			for receipt := range receipts {
				sinkFn(receipt)
			}
		}()
	}
}

// Historical state sink is not parallelizable since it requires the previous state to be committed.
func StartHistoricalStateSinkTrack[ChangeSet any](
	changeSets <-chan ChangeSet,
	sinkFn func(ChangeSet),
) {
	go func() {
		for changeSet := range changeSets {
			sinkFn(changeSet)
		}
	}()
}
