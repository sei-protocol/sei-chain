package tracks

type ReceiptSinkTrack[Receipt any] struct {
	receipts    <-chan Receipt
	sinkFn      func(Receipt)
	workerCount int
}

func NewReceiptSinkTrack[Receipt any](
	receipts <-chan Receipt,
	sinkFn func(Receipt),
	workerCount int,
) *ReceiptSinkTrack[Receipt] {
	return &ReceiptSinkTrack[Receipt]{receipts, sinkFn, workerCount}
}

func (t *ReceiptSinkTrack[Receipt]) Start() {
	go func() {
		for receipt := range t.receipts {
			t.sinkFn(receipt)
		}
	}()
}

func (t *ReceiptSinkTrack[Receipt]) Stop() {}

type HistoricalStateSinkTrack[ChangeSet any] struct {
	changeSets <-chan ChangeSet
	sinkFn     func(ChangeSet)
}

func NewHistoricalStateSinkTrack[ChangeSet any](
	changeSets <-chan ChangeSet,
	sinkFn func(ChangeSet),
) *HistoricalStateSinkTrack[ChangeSet] {
	return &HistoricalStateSinkTrack[ChangeSet]{changeSets, sinkFn}
}

func (t *HistoricalStateSinkTrack[ChangeSet]) Start() {
	go func() {
		for changeSet := range t.changeSets {
			t.sinkFn(changeSet)
		}
	}()
}

func (t *HistoricalStateSinkTrack[ChangeSet]) Stop() {}
