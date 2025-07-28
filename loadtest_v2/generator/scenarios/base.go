package scenarios

import (
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/sei-protocol/sei-chain/loadtest_v2/config"
	"github.com/sei-protocol/sei-chain/loadtest_v2/generator/types"
	"github.com/sei-protocol/sei-chain/loadtest_v2/generator/utils"
	"math/big"
)

var bigOne = big.NewInt(1)

// TxGenerator defines the interface for generating transactions.
type TxGenerator interface {
	Name() string
	Generate(scenario *types.TxScenario) *types.LoadTx
	Attach(config *config.LoadConfig, address common.Address) error
	Deploy(config *config.LoadConfig, deployer *types.Account) common.Address
}

// ScenarioDeployer defines the interface for scenario-specific deployment logic
// This can be implemented by both contract and non-contract scenarios
type ScenarioDeployer interface {
	// DeployScenario handles any setup required for the scenario
	// For contracts: deploys the contract and returns its address
	// For non-contracts: performs any initialization and returns zero address
	DeployScenario(config *config.LoadConfig, deployer *types.Account) common.Address

	// AttachScenario connects to an existing contract.
	AttachScenario(config *config.LoadConfig, address common.Address) common.Address

	// CreateTransaction creates a transaction for this scenario
	CreateTransaction(config *config.LoadConfig, scenario *types.TxScenario) (*ethtypes.Transaction, error)
}

// ContractBindFunc defines a function that creates a contract instance from an address
type ContractBindFunc[T any] func(address common.Address, backend bind.ContractBackend) (*T, error)

// ContractDeployer defines the interface for contract-specific deployment logic
// This extends ScenarioDeployer with contract-specific methods
type ContractDeployer[T any] interface {
	ScenarioDeployer

	// DeployContract deploys the contract with specific constructor arguments
	DeployContract(opts *bind.TransactOpts, client *ethclient.Client) (common.Address, *ethtypes.Transaction, error)

	// GetBindFunc returns the function to bind the contract instance
	GetBindFunc() ContractBindFunc[T]

	// SetContract stores the bound contract instance
	SetContract(contract *T)

	// CreateContractTransaction creates a contract interaction transaction
	CreateContractTransaction(auth *bind.TransactOpts, scenario *types.TxScenario) (*ethtypes.Transaction, error)
}

// ScenarioBase provides common functionality for all scenarios
type ScenarioBase struct {
	config   *config.LoadConfig
	deployed bool
	address  common.Address
	deployer ScenarioDeployer
}

// NewScenarioBase creates a new base scenario with the given deployer
func NewScenarioBase(deployer ScenarioDeployer) *ScenarioBase {
	return &ScenarioBase{
		deployer: deployer,
	}
}

// Deploy handles the common deployment flow
func (s *ScenarioBase) Deploy(config *config.LoadConfig, deployer *types.Account) common.Address {
	s.config = config
	s.address = s.deployer.DeployScenario(config, deployer)
	s.deployed = true
	return s.address
}

// Attach connects to an existing contract.
func (s *ScenarioBase) Attach(config *config.LoadConfig, address common.Address) error {
	s.config = config
	s.address = s.deployer.AttachScenario(config, address)
	s.deployed = true
	return nil
}

// Generate handles the common transaction generation flow
func (s *ScenarioBase) Generate(scenario *types.TxScenario) *types.LoadTx {
	if !s.deployed {
		panic("Scenario not deployed/initialized")
	}

	// Create transaction using scenario-specific logic
	tx, err := s.deployer.CreateTransaction(s.config, scenario)
	if err != nil {
		panic("Failed to create transaction: " + err.Error())
	}

	return types.CreateTxFromEthTx(tx, scenario)
}

// GetConfig returns the configuration
func (s *ScenarioBase) GetConfig() *config.LoadConfig {
	return s.config
}

// GetAddress returns the deployed contract address (zero address for non-contract scenarios)
func (s *ScenarioBase) GetAddress() common.Address {
	return s.address
}

// ContractScenarioBase provides common functionality for contract scenarios
type ContractScenarioBase[T any] struct {
	*ScenarioBase
	deployer ContractDeployer[T]
}

// NewContractScenarioBase creates a new base scenario with the given contract deployer
func NewContractScenarioBase[T any](deployer ContractDeployer[T]) *ContractScenarioBase[T] {
	base := &ContractScenarioBase[T]{
		deployer: deployer,
	}
	base.ScenarioBase = NewScenarioBase(base)
	return base
}

func dial(config *config.LoadConfig) (*ethclient.Client, error) {
	if len(config.Endpoints) == 0 {
		return ethclient.NewClient(nil), nil
	}
	return ethclient.Dial(config.Endpoints[0])
}

// AttachScenario implements AttachScenario interface for contract scenarios
func (c *ContractScenarioBase[T]) AttachScenario(config *config.LoadConfig, address common.Address) common.Address {
	client, err := dial(config)
	if err != nil {
		panic("Failed to connect to Ethereum client: " + err.Error())
	}

	// Bind contract instance using the provided bind function
	bindFunc := c.deployer.GetBindFunc()
	contract, err := bindFunc(address, client)
	if err != nil {
		panic("Failed to bind contract: " + err.Error())
	}

	// Store the contract instance
	c.deployer.SetContract(contract)
	return address
}

// DeployScenario implements ScenarioDeployer interface for contract scenarios
func (c *ContractScenarioBase[T]) DeployScenario(config *config.LoadConfig, deployer *types.Account) common.Address {
	client, err := dial(config)
	if err != nil {
		panic("Failed to connect to Ethereum client: " + err.Error())
	}

	// Create deployment options
	auth, err := utils.CreateDeploymentOpts(config.GetChainID(), client, deployer)
	if err != nil {
		panic("Failed to create deployment options: " + err.Error())
	}

	// Deploy using contract-specific logic
	address, _, err := c.deployer.DeployContract(auth, client)
	if err != nil {
		panic("Failed to deploy contract: " + err.Error())
	}

	// Bind contract instance using the provided bind function
	bindFunc := c.deployer.GetBindFunc()
	contract, err := bindFunc(address, client)
	if err != nil {
		panic("Failed to bind contract: " + err.Error())
	}

	// Store the contract instance
	c.deployer.SetContract(contract)
	return address
}

// CreateTransaction implements ScenarioDeployer interface for contract scenarios
func (c *ContractScenarioBase[T]) CreateTransaction(config *config.LoadConfig, scenario *types.TxScenario) (*ethtypes.Transaction, error) {
	auth := utils.CreateTransactionOpts(config.GetChainID(), scenario)
	return c.deployer.CreateContractTransaction(auth, scenario)
}
