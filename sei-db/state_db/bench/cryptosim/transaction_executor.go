package cryptosim

type TransactionExecutor struct {
	// The CryptoSim benchmark runner.
	cryptoSim *CryptoSim

	// The Incoming transactions to be executed.
	workChan chan *any
}

// A single threaded transaction executor.
func NewTransactionExecutor(
	cryptosim *CryptoSim,
	queueSize int,
) *TransactionExecutor {
	return &TransactionExecutor{
		cryptoSim: cryptosim,
		workChan:  make(chan *any, queueSize),
	}
}

// TODO
