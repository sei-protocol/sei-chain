package scheduler

import "errors"

var (
	ErrReadEstimate = errors.New("multiversion store value contains estimate, cannot read, aborting")
)

// define the return struct for abort due to conflict
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
