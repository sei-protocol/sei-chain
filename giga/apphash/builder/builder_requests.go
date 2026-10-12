package builder

import "github.com/sei-protocol/sei-chain/giga/apphash"

// Asks the builder goroutine to take in one report (i.e. one call to a Report*() method).
type reportRequest struct {
	input       hashInput
	blockHeight uint64
	value       [32]byte
}

// Asks the builder goroutine to complete setup at the storage layer height blockHeight.
type setupCompleteRequest struct {
	blockHeight uint64
	reply       chan callResult[struct{}]
}

// Asks the builder goroutine to register a listener.
type registerListenerRequest struct {
	listener func(appHash *apphash.AppHashData)
	reply    chan callResult[*apphash.AppHashData]
}

// Asks the builder goroutine for an iterator over the published app hashes from startingBlockHeight.
type iteratorRequest struct {
	startingBlockHeight uint64
	reply               chan callResult[apphash.AppHashIterator]
}

// Asks the builder goroutine to permit deleting the app hashes below blockHeight.
type pruneRequest struct {
	blockHeight uint64
}

// The reply to a request that returns a result to its caller.
type callResult[T any] struct {
	value T
	err   error
}
