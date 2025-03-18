package types

import "fmt"

// DefaultGenesisState returns the default Capability genesis state
func DefaultGenesisState() *GenesisState {
	return NewGenesisState(DefaultParams(), []GenesisCtAccount{})
}

// Validate performs basic genesis state validation returning an error upon any
// failure.
func (gs GenesisState) Validate() error {
	if err := gs.Params.Validate(); err != nil {
		return err
	}

	accounts := make(map[string]bool)
	for _, genesisCtAccount := range gs.Accounts {
		if genesisCtAccount.Key == nil {
			return fmt.Errorf("genesisCtAccount key cannot be empty")
		}

		if err := genesisCtAccount.Account.ValidateBasic(); err != nil {
			return err
		}

		account := genesisCtAccount.Account
		publicKey := string(account.PublicKey)
		if accounts[publicKey] {
			return fmt.Errorf("duplicate genesisCtAccount for public key %s", publicKey)
		}
		accounts[publicKey] = true

	}
	return nil
}

// NewGenesisState creates a new genesis state.
func NewGenesisState(params Params, accounts []GenesisCtAccount) *GenesisState {
	return &GenesisState{
		Params:   params,
		Accounts: accounts,
	}
}
