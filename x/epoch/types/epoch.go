package types

import fmt "fmt"

// NewEpoch creates a new Epoch instance
func NewEpoch() Epoch {
	return Epoch{}
}

// DefaultParams returns a default set of parameters
func DefaultEpoch() Epoch {
	return NewEpoch()
}

func (e *Epoch) Validate() error {
	if e.GetGenesisTime().IsZero() {
		return fmt.Errorf("epoch genesis time cannot be zero")
	}

	if e.GetEpochDuration().Seconds() == 0 {
		return fmt.Errorf("epoch duration cannot be zero")
	}

	if e.GetGenesisTime().After(e.GetCurrentEpochStartTime()) {
		return fmt.Errorf("epoch genesis time cannot be after epoch start time")
	}

	if e.GetCurrentEpochHeight() < 0 {
		return fmt.Errorf("epoch current epoch height cannot be negative")
	}

	return nil
}
