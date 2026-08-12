package mempool

import "math"

// TxConstraints contains the precomputed consensus-derived mempool limits for
// the current state snapshot.
type TxConstraints struct {
	MaxDataBytes int64
	MaxGas       int64
	// MaxGasWanted is enforced at admission only when positive; zero leaves
	// admission unrestricted while retaining the existing empty-proposal behavior.
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
