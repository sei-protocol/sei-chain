package types

func DefaultGenesis() *GenesisState {
	return &GenesisState{
		Params: DefaultParams(),
	}
}

func (gs GenesisState) Validate() error {
	return gs.Params.Validate()
}

func ValidateStream(gensisStateCh <-chan GenesisState) error {
	passedParamCheck := false
	var paramCheckErr error
	for genesisState := range gensisStateCh {
		if err := genesisState.Validate(); err != nil {
			paramCheckErr = err
		} else {
			passedParamCheck = true
		}
	}
	if !passedParamCheck {
		return paramCheckErr
	}
	return nil
}
