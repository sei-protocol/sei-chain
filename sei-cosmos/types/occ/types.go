package occ

import (
	"errors"
)

var (
	ErrReadEstimate = errors.New("multiversion store value contains estimate, cannot read, aborting")
)

// Abort contains the information for a transaction's conflict
type Abort struct {
	DependentTxIdx int
	Err            error
}

func NewEstimateAbort(dependentTxIdx int) Abort {
	return Abort{
		DependentTxIdx: dependentTxIdx,
		Err:            ErrReadEstimate,
	}
}
