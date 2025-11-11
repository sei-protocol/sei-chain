package app

import (
	"context"

	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	evmtypes "github.com/sei-protocol/sei-chain/x/evm/types"
	"github.com/sei-protocol/sei-chain/x/evm/types/ethtx"
	"github.com/sei-protocol/sei-load/config"
	"github.com/sei-protocol/sei-load/generator"
	"github.com/sei-protocol/sei-load/generator/scenarios"
	abci "github.com/tendermint/tendermint/abci/types"
	"github.com/tendermint/tendermint/libs/log"
)

func NewGeneratorCh(ctx context.Context, txConfig client.TxConfig, chainID string, evmChainID int64, logger log.Logger) <-chan *abci.ResponsePrepareProposal {
	gen, err := generator.NewConfigBasedGenerator(&config.LoadConfig{
		ChainID:    evmChainID,
		SeiChainID: chainID,
		Accounts:   &config.AccountConfig{Accounts: 5000},
		Scenarios: []config.Scenario{{
			Name:   scenarios.EVMTransfer,
			Weight: 1,
		}},
	})
	if err != nil {
		panic("failed to initialize generator: " + err.Error())
	}
	ch := make(chan *abci.ResponsePrepareProposal, 100)
	go func() {
		defer close(ch)
		var height int64
		for {
			// bail on ctx err
			if ctx.Err() != nil {
				return
			}
			// generate txs like: txs := gen.GenerateN(1000)
			loadTxs := gen.GenerateN(1000)
			if len(loadTxs) == 0 {
				continue
			}

			// Convert LoadTx to Cosmos SDK transaction bytes
			txRecords := make([]*abci.TxRecord, 0, len(loadTxs))
			for _, loadTx := range loadTxs {
				if loadTx.EthTx == nil {
					continue
				}

				// Convert Ethereum transaction to Cosmos SDK format
				txData, err := ethtx.NewTxDataFromTx(loadTx.EthTx)
				if err != nil {
					logger.Error("failed to convert eth tx to tx data", "error", err)
					panic(err)
				}

				msg, err := evmtypes.NewMsgEVMTransaction(txData)
				if err != nil {
					logger.Error("failed to create msg evm transaction", "error", err)
					panic(err)
				}

				gasUsedEstimate := loadTx.EthTx.Gas() // Use gas limit from transaction

				txBuilder := txConfig.NewTxBuilder()
				if err = txBuilder.SetMsgs(msg); err != nil {
					logger.Error("failed to set msgs", "error", err)
					panic(err)
				}
				txBuilder.SetGasEstimate(gasUsedEstimate)

				txbz, encodeErr := txConfig.TxEncoder()(txBuilder.GetTx())
				if encodeErr != nil {
					logger.Error("failed to encode tx", "error", encodeErr)
					panic(encodeErr)
				}

				txRecords = append(txRecords, &abci.TxRecord{
					Action: abci.TxRecord_GENERATED,
					Tx:     txbz,
				})
			}

			if len(txRecords) == 0 {
				continue
			}

			proposal := &abci.ResponsePrepareProposal{
				TxRecords: txRecords,
			}

			height++
			select {
			case ch <- proposal:
			case <-ctx.Done():
				return
			}
		}
	}()
	return ch
}

// InitGenerator initializes the benchmark generator with default config
func (app *App) InitGenerator(ctx context.Context, chainID string, evmChainID int64, logger log.Logger) {
	logger.Info("Initializing benchmark mode generator", "mode", "benchmark")
	app.benchmarkProposalCh = NewGeneratorCh(ctx, app.encodingConfig.TxConfig, chainID, evmChainID, logger)
	logger.Info("Benchmark generator initialized and started", "config", "default EVM Transfers")
}

func (app *App) PrepareProposalGeneratorHandler(_ sdk.Context, req *abci.RequestPrepareProposal) (*abci.ResponsePrepareProposal, error) {
	// Pull from generator channel - the generator has already filtered transactions by size
	select {
	case proposal, ok := <-app.benchmarkProposalCh:
		if proposal == nil || !ok {
			// Channel closed or no proposal available, return empty (req.Txs will remain in mempool)
			return &abci.ResponsePrepareProposal{
				TxRecords: []*abci.TxRecord{},
			}, nil
		}
		return proposal, nil
	default:
		// No proposal ready yet, return empty (req.Txs will remain in mempool)
		return &abci.ResponsePrepareProposal{
			TxRecords: []*abci.TxRecord{},
		}, nil
	}
}

// WithBenchmarkMode is an AppOption that enables benchmark mode with default config
func WithBenchmarkMode() AppOption {
	return func(app *App) {
		app.enableBenchmarkMode = true
	}
}
