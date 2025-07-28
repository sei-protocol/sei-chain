#!/bin/bash

# Script to generate scenario template files
# Usage: ./generate_scenario_template.sh <ContractName> <OutputFile>

set -e

CONTRACT_NAME="$1"
OUTPUT_FILE="$2"

if [ -z "$CONTRACT_NAME" ] || [ -z "$OUTPUT_FILE" ]; then
    echo "Usage: $0 <ContractName> <OutputFile>"
    exit 1
fi

# Check if file already exists
if [ -f "$OUTPUT_FILE" ]; then
    echo "⏭️  Scenario template for $CONTRACT_NAME already exists, skipping..."
    exit 0
fi

echo "📝 Generating scenario template for $CONTRACT_NAME ..."

# Convert contract name to uppercase for constant
SCENARIO_CONST=$(echo "$CONTRACT_NAME" | tr '[:lower:]' '[:upper:]')

# Generate the scenario template file
cat > "$OUTPUT_FILE" << EOF
package scenarios

import (
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"

	"github.com/sei-protocol/sei-chain/loadtest_v2/generator/bindings"
	"github.com/sei-protocol/sei-chain/loadtest_v2/generator/types"
)

const ${SCENARIO_CONST} = "${CONTRACT_NAME}"

// ${CONTRACT_NAME}Scenario implements the TxGenerator interface for ${CONTRACT_NAME} contract operations
type ${CONTRACT_NAME}Scenario struct {
	*ContractScenarioBase[bindings.${CONTRACT_NAME}]
	contract *bindings.${CONTRACT_NAME}
}

// New${CONTRACT_NAME}Scenario creates a new ${CONTRACT_NAME} scenario
func New${CONTRACT_NAME}Scenario() TxGenerator {
	scenario := &${CONTRACT_NAME}Scenario{}
	scenario.ContractScenarioBase = NewContractScenarioBase[bindings.${CONTRACT_NAME}](scenario)
	return scenario
}

// DeployContract implements ContractDeployer interface - deploys ${CONTRACT_NAME} with specific constructor args
func (s *${CONTRACT_NAME}Scenario) DeployContract(opts *bind.TransactOpts, client *ethclient.Client) (common.Address, *ethtypes.Transaction, error) {
	// TODO: Update with actual constructor arguments
	address, tx, _, err := bindings.Deploy${CONTRACT_NAME}(opts, client /* add constructor args here */)
	return address, tx, err
}

// GetBindFunc implements ContractDeployer interface - returns the binding function
func (s *${CONTRACT_NAME}Scenario) GetBindFunc() ContractBindFunc[bindings.${CONTRACT_NAME}] {
	return bindings.New${CONTRACT_NAME}
}

// SetContract implements ContractDeployer interface - stores the contract instance
func (s *${CONTRACT_NAME}Scenario) SetContract(contract *bindings.${CONTRACT_NAME}) {
	s.contract = contract
}

// CreateContractTransaction implements ContractDeployer interface - creates ${CONTRACT_NAME} transaction
func (s *${CONTRACT_NAME}Scenario) CreateContractTransaction(auth *bind.TransactOpts, scenario *types.TxScenario) (*ethtypes.Transaction, error) {
	// TODO: Implement contract interaction logic
	panic("unimplemented - add contract interaction logic here")
}
EOF

echo "✅ Generated scenario template: $OUTPUT_FILE"
