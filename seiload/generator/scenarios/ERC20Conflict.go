package scenarios

import (
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"

	"github.com/sei-protocol/sei-chain/seiload/generator/bindings"
	"github.com/sei-protocol/sei-chain/seiload/types"
)

const ERC20Conflict = "ERC20Conflict"

// ERC20ConflictScenario implements the TxGenerator interface for ERC20Conflict contract operations
type ERC20ConflictScenario struct {
	*ContractScenarioBase[bindings.ERC20Conflict]
	contract *bindings.ERC20Conflict
}

// NewERC20ConflictScenario creates a new ERC20Conflict scenario
func NewERC20ConflictScenario() TxGenerator {
	scenario := &ERC20ConflictScenario{}
	scenario.ContractScenarioBase = NewContractScenarioBase[bindings.ERC20Conflict](scenario)
	return scenario
}

// Name returns the name of the scenario.
func (s *ERC20ConflictScenario) Name() string {
	return ERC20Conflict
}

// DeployContract implements ContractDeployer interface - deploys ERC20Conflict with specific constructor args
func (s *ERC20ConflictScenario) DeployContract(opts *bind.TransactOpts, client *ethclient.Client) (common.Address, *ethtypes.Transaction, error) {
	// TODO: Update with actual constructor arguments
	address, tx, _, err := bindings.DeployERC20Conflict(opts, client, "ConflictToken", "CT")
	return address, tx, err
}

// GetBindFunc implements ContractDeployer interface - returns the binding function
func (s *ERC20ConflictScenario) GetBindFunc() ContractBindFunc[bindings.ERC20Conflict] {
	return bindings.NewERC20Conflict
}

// SetContract implements ContractDeployer interface - stores the contract instance
func (s *ERC20ConflictScenario) SetContract(contract *bindings.ERC20Conflict) {
	s.contract = contract
}

// CreateContractTransaction implements ContractDeployer interface - creates ERC20Conflict transaction
func (s *ERC20ConflictScenario) CreateContractTransaction(auth *bind.TransactOpts, scenario *types.TxScenario) (*ethtypes.Transaction, error) {
	return s.contract.Transfer(auth, scenario.Receiver, bigOne)
}
