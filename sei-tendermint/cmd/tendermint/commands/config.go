package commands

import (
	"fmt"

	"github.com/spf13/viper"

	"github.com/sei-protocol/sei-chain/sei-tendermint/config"
)

// ParseConfig retrieves the default environment configuration,
// sets up the Tendermint root and ensures that the root exists.
func ParseConfig(conf *config.Config) (*config.Config, error) {
	if err := viper.Unmarshal(conf); err != nil {
		return nil, err
	}

	conf.SetRoot(conf.RootDir)

	if err := conf.ValidateBasic(); err != nil {
		return nil, fmt.Errorf("error in config file: %w", err)
	}

	return conf, nil
}
