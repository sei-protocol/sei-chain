package types

// DefaultParams returns default mev module parameters
func DefaultParams() Params {
	return Params{}
}

// Validate validates params
func (p Params) Validate() error {
	// Add any validation logic here if needed
	return nil
}
