package types

// GenesisState defines the seinet module's genesis state.
type GenesisState struct{}

// DefaultGenesis returns the default genesis state.
func DefaultGenesis() *GenesisState {
	return &GenesisState{}
}

// Validate performs basic validation on the genesis state.
func (gs GenesisState) Validate() error {
	return nil
}
