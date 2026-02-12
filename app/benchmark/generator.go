package benchmark

import (
	"context"
	"fmt"
	"math/big"
	"os"
	"strconv"
	"sync"

	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/sei-protocol/sei-chain/sei-cosmos/client"
	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/log"
	evmtypes "github.com/sei-protocol/sei-chain/x/evm/types"
	"github.com/sei-protocol/sei-chain/x/evm/types/ethtx"
	"github.com/sei-protocol/sei-load/config"
	"github.com/sei-protocol/sei-load/generator"
	"github.com/sei-protocol/sei-load/generator/scenarios"
	loadtypes "github.com/sei-protocol/sei-load/types"
)

// Phase represents the current phase of the benchmark generator.
type Phase int

const (
	// PhaseWarmup is the initial phase to let the chain fully initialize.
	PhaseWarmup Phase = iota
	// PhaseSetup is the phase where contracts are deployed.
	PhaseSetup
	// PhaseLoad is the main phase where load transactions are generated.
	PhaseLoad
)

// WarmupBlocks is the number of blocks to wait before starting setup.
// This allows the chain to fully initialize (EVM genesis, fee collector address, etc.)
const WarmupBlocks = 3

// scenarioState tracks the state of a scenario instance.
type scenarioState struct {
	config       config.Scenario
	scenario     scenarios.TxGenerator
	accounts     loadtypes.AccountPool
	deployed     bool
	address      common.Address
	deployTx     *ethtypes.Transaction
	deployTxHash common.Hash
}

// Generator manages the benchmark transaction generation with setup and load phases.
type Generator struct {
	cfg      *config.LoadConfig
	txConfig client.TxConfig
	chainID  *big.Int

	scenarios      []*scenarioState
	deployer       *loadtypes.Account
	sharedAccounts loadtypes.AccountPool
	accountPools   []loadtypes.AccountPool

	phase          Phase
	warmupCounter  int // counts blocks during warmup phase
	pendingDeploys map[common.Hash]*scenarioState

	loadGenerator generator.Generator
	txsPerBatch   int

	mu     sync.RWMutex
	logger log.Logger
}

// NewGenerator creates a new benchmark generator from a config.
func NewGenerator(cfg *config.LoadConfig, txConfig client.TxConfig, logger log.Logger) (*Generator, error) {
	// Read number of transactions per batch from environment variable, default to 1000
	txsPerBatch := 1000
	if envVal := os.Getenv("BENCHMARK_TXS_PER_BATCH"); envVal != "" {
		if parsed, err := strconv.Atoi(envVal); err == nil && parsed > 0 {
			txsPerBatch = parsed
		}
	}
	logger.Info("benchmark generator config", "txsPerBatch", txsPerBatch)

	bg := &Generator{
		cfg:            cfg,
		txConfig:       txConfig,
		chainID:        cfg.GetChainID(),
		deployer:       loadtypes.GenerateAccounts(1)[0],
		pendingDeploys: make(map[common.Hash]*scenarioState),
		phase:          PhaseWarmup, // Start with warmup to let chain initialize
		warmupCounter:  0,
		txsPerBatch:    txsPerBatch,
		logger:         logger,
	}
	logger.Info("benchmark: Generator will wait for warmup blocks before deploying", "warmupBlocks", WarmupBlocks)

	// Create shared account pool
	if cfg.Accounts != nil {
		accounts := loadtypes.GenerateAccounts(cfg.Accounts.Accounts)
		bg.sharedAccounts = loadtypes.NewAccountPool(&loadtypes.AccountConfig{
			Accounts:       accounts,
			NewAccountRate: cfg.Accounts.NewAccountRate,
		})
		bg.accountPools = append(bg.accountPools, bg.sharedAccounts)
	}

	// Create scenario instances
	for i, scenarioCfg := range cfg.Scenarios {
		scenario := scenarios.CreateScenario(scenarioCfg)

		// Determine account pool to use
		var accountPool loadtypes.AccountPool
		if scenarioCfg.Accounts != nil {
			// Scenario defines its own account settings - create separate pool
			accounts := loadtypes.GenerateAccounts(scenarioCfg.Accounts.Accounts)
			accountPool = loadtypes.NewAccountPool(&loadtypes.AccountConfig{
				Accounts:       accounts,
				NewAccountRate: scenarioCfg.Accounts.NewAccountRate,
			})
			bg.accountPools = append(bg.accountPools, accountPool)
		} else if bg.sharedAccounts != nil {
			accountPool = bg.sharedAccounts
		} else {
			return nil, fmt.Errorf("no accounts config defined for scenario %d", i)
		}

		state := &scenarioState{
			config:   scenarioCfg,
			scenario: scenario,
			accounts: accountPool,
		}
		bg.scenarios = append(bg.scenarios, state)
	}

	logger.Info("Benchmark generator created",
		"scenarios", len(bg.scenarios),
		"accounts", cfg.Accounts.Accounts,
	)

	return bg, nil
}

// Phase returns the current phase of the generator.
func (g *Generator) Phase() Phase {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.phase
}

// IsSetupPhase returns true if the generator is in the setup phase.
func (g *Generator) IsSetupPhase() bool {
	return g.Phase() == PhaseSetup
}

// createDeploymentTx creates a deployment transaction for a scenario.
// Returns nil if the scenario doesn't need deployment.
func (g *Generator) createDeploymentTx(state *scenarioState) *ethtypes.Transaction {
	// Create a temporary TxScenario for deployment
	txScenario := &loadtypes.TxScenario{
		Name:     state.scenario.Name(),
		Sender:   g.deployer,
		Receiver: common.Address{}, // Not used for deployment
	}

	// Try to get a deployment transaction by checking if scenario supports it
	// We use the scenario's Deploy method which creates deployment transactions
	// For scenarios that don't need deployment (like EVMTransfer), this returns zero address

	// Check if this is a contract scenario by trying to generate a deploy-style tx
	// We do this by calling the scenario's internal deployment logic

	// For now, we identify contract scenarios by name
	switch state.scenario.Name() {
	case scenarios.EVMTransfer, scenarios.EVMTransferNoop:
		// These don't need deployment
		return nil
	}

	// For contract scenarios, create a deployment transaction
	// We need to craft this manually since we're not using RPC
	return g.craftDeploymentTx(state, txScenario)
}

// craftDeploymentTx creates deployment bytecode transaction for contract scenarios.
func (g *Generator) craftDeploymentTx(state *scenarioState, txScenario *loadtypes.TxScenario) *ethtypes.Transaction {
	var deployData []byte

	// Get deployment bytecode based on scenario type
	switch state.scenario.Name() {
	case scenarios.ERC20:
		deployData = getERC20DeployData()
	case scenarios.ERC721:
		deployData = getERC721DeployData()
	case scenarios.ERC20Conflict:
		deployData = getERC20ConflictDeployData()
	case scenarios.ERC20Noop:
		deployData = getERC20NoopDeployData()
	case scenarios.Disperse:
		deployData = getDisperseDeployData()
	default:
		g.logger.Info("benchmark: Unknown contract scenario, skipping deployment", "scenario", state.scenario.Name())
		return nil
	}

	nonce := g.deployer.GetAndIncrementNonce()
	g.logger.Info("benchmark: Creating deployment transaction",
		"scenario", state.scenario.Name(),
		"deployer", g.deployer.Address.Hex(),
		"nonce", nonce,
		"chainID", g.chainID.String(),
		"dataLen", len(deployData))

	// Create deployment transaction with chain ID set
	tx := &ethtypes.DynamicFeeTx{
		ChainID:   g.chainID,
		Nonce:     nonce,
		To:        nil, // nil To address indicates contract creation
		Value:     big.NewInt(0),
		Gas:       5000000,                   // 5M gas limit for deployment (increased)
		GasTipCap: big.NewInt(1000000000),    // 1 gwei
		GasFeeCap: big.NewInt(1000000000000), // 1000 gwei (high to ensure inclusion)
		Data:      deployData,
	}

	// Sign the transaction (use CancunSigner to match sei-load scenarios)
	signer := ethtypes.NewCancunSigner(g.chainID)
	signedTx, err := ethtypes.SignTx(ethtypes.NewTx(tx), signer, g.deployer.PrivKey)
	if err != nil {
		g.logger.Error("benchmark: Failed to sign deployment tx", "error", err)
		panic(err)
	}

	g.logger.Info("benchmark: Deployment transaction signed",
		"txHash", signedTx.Hash().Hex(),
		"gas", signedTx.Gas())

	return signedTx
}

// generateSetupBlock creates deployment transactions for undeployed scenarios.
// This is called on every PrepareProposal during setup phase, but we only
// create a deployment transaction ONCE per scenario.
func (g *Generator) generateSetupBlock() []*abci.TxRecord {
	g.mu.Lock()
	defer g.mu.Unlock()

	txRecords := make([]*abci.TxRecord, 0, len(g.scenarios))

	for _, state := range g.scenarios {
		// Skip if already deployed
		if state.deployed {
			continue
		}

		// Skip if deployment transaction already created and pending
		// (deployTxHash is set when we create the deploy tx)
		if state.deployTxHash != (common.Hash{}) {
			// Already have a pending deploy tx for this scenario
			continue
		}

		// Create deployment transaction (only happens once per scenario)
		deployTx := g.createDeploymentTx(state)
		if deployTx == nil {
			// Scenario doesn't need deployment (e.g., EVMTransfer)
			g.logger.Info("benchmark: Scenario doesn't need deployment, attaching with zero address",
				"scenario", state.scenario.Name())
			state.deployed = true
			if err := state.scenario.Attach(g.cfg, common.Address{}); err != nil {
				panic(fmt.Sprintf("benchmark: Failed to attach scenario %s: %v", state.scenario.Name(), err))
			}
			continue
		}

		state.deployTx = deployTx
		state.deployTxHash = deployTx.Hash()
		g.pendingDeploys[deployTx.Hash()] = state

		g.logger.Info("benchmark: Created deployment transaction (will only deploy once)",
			"scenario", state.config.Name,
			"txHash", deployTx.Hash().Hex())

		// Convert to Cosmos SDK tx
		txRecord, err := g.ethTxToTxRecord(deployTx)
		if err != nil {
			panic(fmt.Sprintf("benchmark: Failed to convert deployment tx for %s: %v", state.config.Name, err))
		}
		txRecords = append(txRecords, txRecord)
	}

	// Fast-path: if no scenarios need contract deployment (e.g., all EVMTransfer),
	// we can transition to load phase immediately since all are marked deployed.
	// For contract scenarios, transition happens in ProcessReceipts() after
	// deployment transactions are confirmed.
	if len(g.pendingDeploys) == 0 && g.allScenariosDeployed() {
		g.transitionToLoadPhase()
	}

	return txRecords
}

// allScenariosDeployed returns true if all scenarios are marked as deployed.
func (g *Generator) allScenariosDeployed() bool {
	for _, state := range g.scenarios {
		if !state.deployed {
			return false
		}
	}
	return true
}

// generateLoadBlock generates load transactions.
func (g *Generator) generateLoadBlock() []*abci.TxRecord {
	g.mu.RLock()
	loadGen := g.loadGenerator
	g.mu.RUnlock()

	if loadGen == nil {
		return nil
	}

	loadTxs := loadGen.GenerateN(g.txsPerBatch)
	txRecords := make([]*abci.TxRecord, 0, len(loadTxs))
	for _, loadTx := range loadTxs {
		txRecord, err := g.ethTxToTxRecord(loadTx.EthTx)
		if err != nil {
			panic(fmt.Sprintf("benchmark: Failed to convert load tx: %v", err))
		}
		txRecords = append(txRecords, txRecord)
	}

	return txRecords
}

// ethTxToTxRecord converts an Ethereum transaction to a Cosmos SDK TxRecord.
func (g *Generator) ethTxToTxRecord(ethTx *ethtypes.Transaction) (*abci.TxRecord, error) {
	txData, err := ethtx.NewTxDataFromTx(ethTx)
	if err != nil {
		return nil, fmt.Errorf("failed to convert eth tx to tx data: %w", err)
	}

	msg, err := evmtypes.NewMsgEVMTransaction(txData)
	if err != nil {
		return nil, fmt.Errorf("failed to create msg evm transaction: %w", err)
	}

	txBuilder := g.txConfig.NewTxBuilder()
	if err = txBuilder.SetMsgs(msg); err != nil {
		return nil, fmt.Errorf("failed to set msgs: %w", err)
	}
	txBuilder.SetGasEstimate(ethTx.Gas())

	txbz, err := g.txConfig.TxEncoder()(txBuilder.GetTx())
	if err != nil {
		return nil, fmt.Errorf("failed to encode tx: %w", err)
	}

	return &abci.TxRecord{
		Action: abci.TxRecord_UNMODIFIED,
		Tx:     txbz,
	}, nil
}

// ProcessReceipts handles receipts from FinalizeBlock to extract deployed addresses.
func (g *Generator) ProcessReceipts(receipts map[common.Hash]*evmtypes.Receipt) {
	g.mu.Lock()
	defer g.mu.Unlock()

	if g.phase != PhaseSetup {
		return
	}

	g.logger.Info("benchmark: Processing receipts for pending deployments",
		"pendingCount", len(g.pendingDeploys),
		"receiptsProvided", len(receipts))

	for txHash, state := range g.pendingDeploys {
		receipt, ok := receipts[txHash]
		if !ok {
			g.logger.Info("benchmark: Receipt not yet available for deployment",
				"scenario", state.config.Name,
				"txHash", txHash.Hex())
			continue
		}

		g.logger.Info("benchmark: Found receipt for deployment",
			"scenario", state.config.Name,
			"txHash", txHash.Hex(),
			"status", receipt.Status,
			"gasUsed", receipt.GasUsed,
			"contractAddress", receipt.ContractAddress,
			"vmError", receipt.VmError)

		if receipt.Status != uint32(ethtypes.ReceiptStatusSuccessful) {
			panic(fmt.Sprintf("benchmark: Deployment failed for scenario %s: txHash=%s status=%d vmError=%s gasUsed=%d",
				state.config.Name, txHash.Hex(), receipt.Status, receipt.VmError, receipt.GasUsed))
		}

		addr := common.HexToAddress(receipt.ContractAddress)
		g.logger.Info("benchmark: Contract deployed successfully",
			"scenario", state.config.Name,
			"address", addr.Hex(),
			"txHash", txHash.Hex(),
			"gasUsed", receipt.GasUsed)

		// Attach the deployed address to the scenario
		if err := state.scenario.Attach(g.cfg, addr); err != nil {
			panic(fmt.Sprintf("benchmark: Failed to attach scenario %s to address %s: %v",
				state.config.Name, addr.Hex(), err))
		}
		g.logger.Info("benchmark: Scenario attached to deployed contract",
			"scenario", state.config.Name,
			"address", addr.Hex())
		state.address = addr
		state.deployed = true
		delete(g.pendingDeploys, txHash)
	}

	// Transition to load phase once all deployments are confirmed
	if len(g.pendingDeploys) == 0 && g.allScenariosDeployed() {
		g.transitionToLoadPhase()
	}
}

// transitionToLoadPhase switches from setup to load generation.
func (g *Generator) transitionToLoadPhase() {
	g.logger.Info("benchmark: All scenarios deployed, transitioning to load phase")
	g.phase = PhaseLoad

	// Create weighted generator from deployed scenarios
	weightedConfigs := make([]*generator.WeightedCfg, 0, len(g.scenarios))
	for _, state := range g.scenarios {
		if !state.deployed {
			g.logger.Info("benchmark: Scenario not deployed, skipping", "scenario", state.config.Name)
			continue
		}
		if state.config.Weight == 0 {
			g.logger.Info("benchmark: Skipping scenario with weight 0", "scenario", state.config.Name)
			continue
		}
		g.logger.Info("benchmark: Adding scenario to load generator",
			"scenario", state.config.Name,
			"weight", state.config.Weight,
			"address", state.address.Hex())
		gen := generator.NewScenarioGenerator(state.accounts, state.scenario)
		weightedConfigs = append(weightedConfigs, generator.WeightedConfig(state.config.Weight, gen))
	}

	if len(weightedConfigs) == 0 {
		panic("benchmark: No scenarios available for load generation")
	}

	g.loadGenerator = generator.NewWeightedGenerator(weightedConfigs...)
	g.logger.Info("benchmark: Load generator initialized and ready", "scenarios", len(weightedConfigs))
}

// Generate returns the next batch of transaction records.
func (g *Generator) Generate() []*abci.TxRecord {
	g.mu.Lock()
	phase := g.phase

	// Handle warmup phase - just count blocks and transition to setup
	if phase == PhaseWarmup {
		g.warmupCounter++
		if g.warmupCounter >= WarmupBlocks {
			g.logger.Info("benchmark: Warmup complete, transitioning to setup phase",
				"blocksWaited", g.warmupCounter)
			g.phase = PhaseSetup
			phase = PhaseSetup
		} else {
			g.logger.Info("benchmark: Warming up, waiting for chain to initialize",
				"block", g.warmupCounter,
				"needed", WarmupBlocks)
			g.mu.Unlock()
			return nil // Return empty during warmup
		}
	}
	g.mu.Unlock()

	if phase == PhaseSetup {
		return g.generateSetupBlock()
	}

	return g.generateLoadBlock()
}

// GetPendingDeployHashes returns the transaction hashes of pending deployments.
func (g *Generator) GetPendingDeployHashes() []common.Hash {
	g.mu.RLock()
	defer g.mu.RUnlock()

	hashes := make([]common.Hash, 0, len(g.pendingDeploys))
	for hash := range g.pendingDeploys {
		hashes = append(hashes, hash)
	}
	return hashes
}

// StartProposalChannel creates a channel that generates proposals.
func (g *Generator) StartProposalChannel(ctx context.Context, logger *Logger) <-chan *abci.ResponsePrepareProposal {
	ch := make(chan *abci.ResponsePrepareProposal, 100)

	go func() {
		defer close(ch)
		for {
			if ctx.Err() != nil {
				return
			}

			txRecords := g.Generate()
			if len(txRecords) == 0 {
				continue
			}

			proposal := &abci.ResponsePrepareProposal{
				TxRecords: txRecords,
			}

			select {
			case ch <- proposal:
			case <-ctx.Done():
				return
			}
		}
	}()

	return ch
}
