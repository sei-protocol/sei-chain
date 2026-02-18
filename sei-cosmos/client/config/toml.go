package config

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"text/template"

	"github.com/sei-protocol/sei-chain/sei-cosmos/client/flags"
	tmcli "github.com/sei-protocol/sei-chain/sei-tendermint/libs/cli"
	"github.com/spf13/viper"
)

const defaultConfigTemplate = `# This is a TOML config file.
# For more information, see https://github.com/toml-lang/toml

###############################################################################
###                           Client Configuration                            ###
###############################################################################

# The network chain ID
chain-id = "{{ .ChainID }}"
# The keyring's backend, where the keys are stored (os|file|kwallet|pass|test|memory)
keyring-backend = "{{ .KeyringBackend }}"
# CLI output format (text|json)
output = "{{ .Output }}"
# <host>:<port> to Tendermint RPC interface for this chain
node = "{{ .Node }}"
# Transaction broadcasting mode (sync|async|block)
broadcast-mode = "{{ .BroadcastMode }}"
`

func SetClientConfig(key string, value string, configPath string, config *ClientConfig) error {
	switch key {
	case flags.FlagChainID:
		config.SetChainID(value)
	case flags.FlagKeyringBackend:
		config.SetKeyringBackend(value)
	case tmcli.OutputFlag:
		config.SetOutput(value)
	case flags.FlagNode:
		config.SetNode(value)
	case flags.FlagBroadcastMode:
		config.SetBroadcastMode(value)
	default:
		return errUnknownConfigKey(key)
	}

	confFile := filepath.Join(configPath, "client.toml")
	if err := writeConfigToFile(confFile, config); err != nil {
		return fmt.Errorf("could not write client config to the file: %v", err)
	}
	return nil
}

// writeConfigToFile parses defaultConfigTemplate, renders config using the template and writes it to
// configFilePath.
func writeConfigToFile(configFilePath string, config *ClientConfig) error {
	var buffer bytes.Buffer

	tmpl := template.New("clientConfigFileTemplate")
	configTemplate, err := tmpl.Parse(defaultConfigTemplate)
	if err != nil {
		return err
	}

	if err := configTemplate.Execute(&buffer, config); err != nil {
		return err
	}

	return os.WriteFile(configFilePath, buffer.Bytes(), 0600)
}

// ensureConfigPath creates a directory configPath if it does not exist
func ensureConfigPath(configPath string) error {
	return os.MkdirAll(configPath, 0750)
}

// getClientConfig reads values from client.toml file and unmarshalls them into ClientConfig
func GetClientConfig(configPath string, v *viper.Viper) (*ClientConfig, error) {
	v.AddConfigPath(configPath)
	v.SetConfigName("client")
	v.SetConfigType("toml")

	if err := v.ReadInConfig(); err != nil {
		return nil, err
	}

	conf := new(ClientConfig)
	if err := v.Unmarshal(conf); err != nil {
		return nil, err
	}

	return conf, nil
}
