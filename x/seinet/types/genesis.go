package types

// GenesisState holds module genesis data.
type GenesisState struct {
	Covenants     []SeiNetCovenant          `json:"covenants"`
	ThreatRecords []SeiGuardianThreatRecord `json:"threat_records"`
}

// DefaultGenesis returns default genesis state.
func DefaultGenesis() *GenesisState {
	return &GenesisState{
		Covenants:     []SeiNetCovenant{},
		ThreatRecords: []SeiGuardianThreatRecord{},
	}
}

// Validate performs basic genesis validation.
func (gs GenesisState) Validate() error {
	return nil
}
