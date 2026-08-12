package mempool

import "math"

// TxConstraints contains the precomputed consensus-derived mempool limits for
// the current state snapshot.
type TxConstraints struct {
	MaxDataBytes int64
	MaxGas       int64
	// MaxGasWanted uses -1 for unlimited. Zero remains accepted for backward
	// compatibility and yields empty proposals.
	MaxGasWanted int64
}

// TxConstraintsFetcher returns the precomputed consensus-derived mempool limits for the current
// state snapshot.
type TxConstraintsFetcher func() (TxConstraints, error)

func NopTxConstraints() TxConstraints {
	return TxConstraints{
		MaxDataBytes: math.MaxInt64,
		MaxGas:       -1,
		MaxGasWanted: -1,
	}
}

func NopTxConstraintsFetcher() (TxConstraints, error) {
	return NopTxConstraints(), nil
}
