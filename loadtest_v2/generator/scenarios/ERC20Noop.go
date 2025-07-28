package scenarios

import (
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"

	"github.com/sei-protocol/sei-chain/loadtest_v2/generator/bindings"
	"github.com/sei-protocol/sei-chain/loadtest_v2/generator/types"
)

const ERC20Noop = "ERC20Noop"

// ERC20NoopScenario implements the TxGenerator interface for ERC20Noop contract operations
type ERC20NoopScenario struct {
	*ContractScenarioBase[bindings.ERC20Noop]
	contract *bindings.ERC20Noop
}

// NewERC20NoopScenario creates a new ERC20Noop scenario
func NewERC20NoopScenario() TxGenerator {
	scenario := &ERC20NoopScenario{}
	scenario.ContractScenarioBase = NewContractScenarioBase[bindings.ERC20Noop](scenario)
	return scenario
}

// DeployContract implements ContractDeployer interface - deploys ERC20Noop with specific constructor args
func (s *ERC20NoopScenario) DeployContract(opts *bind.TransactOpts, client *ethclient.Client) (common.Address, *ethtypes.Transaction, error) {
	// TODO: Update with actual constructor arguments
	address, tx, _, err := bindings.DeployERC20Noop(opts, client, "NoopToken", "NT")
	return address, tx, err
}

// GetBindFunc implements ContractDeployer interface - returns the binding function
func (s *ERC20NoopScenario) GetBindFunc() ContractBindFunc[bindings.ERC20Noop] {
	return bindings.NewERC20Noop
}

// SetContract implements ContractDeployer interface - stores the contract instance
func (s *ERC20NoopScenario) SetContract(contract *bindings.ERC20Noop) {
	s.contract = contract
}

// CreateContractTransaction implements ContractDeployer interface - creates ERC20Noop transaction
func (s *ERC20NoopScenario) CreateContractTransaction(auth *bind.TransactOpts, scenario *types.TxScenario) (*ethtypes.Transaction, error) {
	return s.contract.Transfer(auth, scenario.Receiver, bigOne)
}
