package types

// NewEpoch creates a new Epoch instance
func NewEpoch() Epoch {
	return Epoch{}
}

// DefaultParams returns a default set of parameters
func DefaultEpoch() Epoch {
	return NewEpoch()
}
