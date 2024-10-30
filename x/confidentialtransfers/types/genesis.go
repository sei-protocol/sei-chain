package types

// this line is used by starport scaffolding # genesis/types/import

// DefaultIndex is the default capability global index
const DefaultIndex uint64 = 1

// DefaultGenesis returns the default Capability genesis state
// TODO: Define the Genesis State as a .proto message once it's properly fleshed out
func DefaultGenesis() *GenesisState {
	return &GenesisState{
		Params: DefaultParams(),
	}
}

// Validate performs basic genesis state validation returning an error upon any
// failure.
// TODO: Implement this method once DefaultGenesis is defined.
func (gs GenesisState) Validate() error {
	return nil
}
