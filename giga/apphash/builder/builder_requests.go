package builder

import "github.com/sei-protocol/sei-chain/giga/apphash"

// Asks the builder goroutine to take in one report (i.e. one call to a Report*() method).
type reportRequest struct {
	input       hashInput
	blockHeight uint64
	value       [32]byte

	// The debugging hashes passed to ReportStateHash(), written to the hash log only. Nil for every other input.
	componentHashes map[string][]byte
}

// Asks the builder goroutine to complete setup at the storage layer height blockHeight.
type setupCompleteRequest struct {
	blockHeight uint64
	reply       chan callResult[struct{}]
}

// Asks the builder goroutine for the published app hash of blockHeight.
type getAppHashRequest struct {
	blockHeight uint64
	reply       chan callResult[*apphash.AppHashData]
}

// Asks the builder goroutine for the range of available app hashes.
type boundsRequest struct {
	reply chan callResult[appHashBounds]
}

// The range of available app hashes, as Bounds() reports it.
type appHashBounds struct {

	// The lowest block whose app hash is available. Valid only if ok.
	lowest uint64

	// The highest block whose app hash is available. Valid only if ok.
	highest uint64

	// False if no app hash is available yet.
	ok bool
}

// Asks the builder goroutine to store and publish every block completed by the requests ahead of it, then stop.
type closeRequest struct {
	reply chan callResult[struct{}]
}

// The reply to a request that returns a result to its caller.
type callResult[T any] struct {
	value T
	err   error
}
