package scenarios

import (
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"

	"github.com/sei-protocol/sei-chain/loadtest_v2/config"
	"github.com/sei-protocol/sei-chain/loadtest_v2/generator/types"
)

const EVMTransfer = "EVMTransfer"

// EVMTransferScenario implements the TxGenerator interface for simple ETH transfers
type EVMTransferScenario struct {
	*ScenarioBase
}

// NewEVMTransferScenario creates a new ETH transfer scenario
func NewEVMTransferScenario() TxGenerator {
	scenario := &EVMTransferScenario{}
	scenario.ScenarioBase = NewScenarioBase(scenario)
	return scenario
}

// Name returns the name of the scenario.
func (s *EVMTransferScenario) Name() string {
	return EVMTransfer
}

// DeployScenario implements ScenarioDeployer interface - no deployment needed for ETH transfers
func (s *EVMTransferScenario) DeployScenario(config *config.LoadConfig, deployer *types.Account) common.Address {
	// No deployment needed for simple ETH transfers
	// Return zero address to indicate no contract deployment
	return common.Address{}
}

// AttachScenario implements ScenarioDeployer interface - no attachment needed for ETH transfers.
func (s *EVMTransferScenario) AttachScenario(config *config.LoadConfig, address common.Address) common.Address {
	// No attachment needed for simple ETH transfers
	// Return zero address to indicate no contract deployment
	return common.Address{}
}

// CreateTransaction implements ScenarioDeployer interface - creates ETH transfer transaction
func (s *EVMTransferScenario) CreateTransaction(config *config.LoadConfig, scenario *types.TxScenario) (*ethtypes.Transaction, error) {
	// Create transaction with value transfer
	tx := &ethtypes.LegacyTx{
		Nonce:    scenario.Nonce,
		To:       &scenario.Receiver,
		Value:    bigOne,
		Gas:      21000,                   // Standard gas limit for ETH transfer
		GasPrice: big.NewInt(20000000000), // 20 gwei, same as used in utils
		Data:     nil,                     // No data for simple transfer
	}

	// Sign the transaction
	signer := ethtypes.NewEIP155Signer(config.GetChainID())
	signedTx, err := ethtypes.SignTx(ethtypes.NewTx(tx), signer, scenario.Sender.PrivKey)
	if err != nil {
		return nil, err
	}

	return signedTx, nil
}
