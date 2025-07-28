package scenarios

import (
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"

	"github.com/sei-protocol/sei-chain/loadtest_v2/generator/bindings"
	"github.com/sei-protocol/sei-chain/loadtest_v2/generator/types"
)

const ERC20 = "ERC20"

// ERC20Scenario implements the TxGenerator interface for ERC20 contract operations
type ERC20Scenario struct {
	*ContractScenarioBase[bindings.ERC20]
	contract *bindings.ERC20
}

// Name returns the name of the scenario.
func (s *ERC20Scenario) Name() string {
	return ERC20
}

// NewERC20Scenario creates a new ERC20 scenario
func NewERC20Scenario() TxGenerator {
	scenario := &ERC20Scenario{}
	scenario.ContractScenarioBase = NewContractScenarioBase[bindings.ERC20](scenario)
	return scenario
}

// DeployContract implements ContractDeployer interface - deploys ERC20 with specific constructor args
func (s *ERC20Scenario) DeployContract(opts *bind.TransactOpts, client *ethclient.Client) (common.Address, *ethtypes.Transaction, error) {
	// TODO: Update with actual constructor arguments
	address, tx, _, err := bindings.DeployERC20(opts, client, "LoadToken", "LT")
	return address, tx, err
}

// GetBindFunc implements ContractDeployer interface - returns the binding function
func (s *ERC20Scenario) GetBindFunc() ContractBindFunc[bindings.ERC20] {
	return bindings.NewERC20
}

// SetContract implements ContractDeployer interface - stores the contract instance
func (s *ERC20Scenario) SetContract(contract *bindings.ERC20) {
	s.contract = contract
}

// CreateContractTransaction implements ContractDeployer interface - creates ERC20 transaction
func (s *ERC20Scenario) CreateContractTransaction(auth *bind.TransactOpts, scenario *types.TxScenario) (*ethtypes.Transaction, error) {
	return s.contract.Transfer(auth, scenario.Receiver, bigOne)
}
