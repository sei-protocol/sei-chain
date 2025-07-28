package scenarios

import (
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/sei-protocol/sei-chain/loadtest_v2/generator/bindings"
	"github.com/sei-protocol/sei-chain/loadtest_v2/generator/types"
	"math/big"
	"sync/atomic"
)

const ERC721 = "ERC721"

// ERC721Scenario implements the TxGenerator interface for ERC721 contract operations
type ERC721Scenario struct {
	*ContractScenarioBase[bindings.ERC721]
	contract *bindings.ERC721
	id       int64
}

// NewERC721Scenario creates a new ERC721 scenario
func NewERC721Scenario() TxGenerator {
	scenario := &ERC721Scenario{}
	scenario.ContractScenarioBase = NewContractScenarioBase[bindings.ERC721](scenario)
	return scenario
}

// DeployContract implements ContractDeployer interface - deploys ERC721 with specific constructor args
func (s *ERC721Scenario) DeployContract(opts *bind.TransactOpts, client *ethclient.Client) (common.Address, *ethtypes.Transaction, error) {
	// TODO: Update with actual constructor arguments
	address, tx, _, err := bindings.DeployERC721(opts, client /* add constructor args here */)
	return address, tx, err
}

// GetBindFunc implements ContractDeployer interface - returns the binding function
func (s *ERC721Scenario) GetBindFunc() ContractBindFunc[bindings.ERC721] {
	return bindings.NewERC721
}

// SetContract implements ContractDeployer interface - stores the contract instance
func (s *ERC721Scenario) SetContract(contract *bindings.ERC721) {
	s.contract = contract
}

// CreateContractTransaction implements ContractDeployer interface - creates ERC721 transaction
func (s *ERC721Scenario) CreateContractTransaction(auth *bind.TransactOpts, scenario *types.TxScenario) (*ethtypes.Transaction, error) {
	return s.contract.Mint(auth, scenario.Receiver, big.NewInt(atomic.AddInt64(&s.id, 1)))
}
