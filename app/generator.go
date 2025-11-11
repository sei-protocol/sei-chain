package app

import (
	"context"
	"fmt"

	"github.com/cosmos/cosmos-sdk/client"
	evmtypes "github.com/sei-protocol/sei-chain/x/evm/types"
	"github.com/sei-protocol/sei-chain/x/evm/types/ethtx"
	"github.com/sei-protocol/sei-load/config"
	"github.com/sei-protocol/sei-load/generator"
	"github.com/sei-protocol/sei-load/generator/scenarios"
	abci "github.com/tendermint/tendermint/abci/types"
	"github.com/tendermint/tendermint/libs/log"
)

func NewGeneratorCh(ctx context.Context, txConfig client.TxConfig, chainID int64, maxTxBytes int64, logger log.Logger) <-chan *abci.ResponsePrepareProposal {
	gen, err := generator.NewConfigBasedGenerator(&config.LoadConfig{
		ChainID:    chainID,
		SeiChainID: fmt.Sprintf("%d", chainID), // Use chainID as string for SeiChainID
		Accounts:   &config.AccountConfig{Accounts: 5000},
		Scenarios: []config.Scenario{{
			Name:   scenarios.EVMTransfer,
			Weight: 1,
		}},
	})
	if err != nil {
		panic("failed to initialize generator: " + err.Error())
	}
	ch := make(chan *abci.ResponsePrepareProposal, 1000)
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

			// Convert LoadTx to Cosmos SDK transaction bytes and filter by size
			var totalBytes int64
			txRecords := make([]*abci.TxRecord, 0, len(loadTxs))
			for _, loadTx := range loadTxs {
				if loadTx.EthTx == nil {
					continue
				}

				// Convert Ethereum transaction to Cosmos SDK format
				txData, err := ethtx.NewTxDataFromTx(loadTx.EthTx)
				if err != nil {
					logger.Error("failed to convert eth tx to tx data", "error", err)
					continue
				}

				msg, err := evmtypes.NewMsgEVMTransaction(txData)
				if err != nil {
					logger.Error("failed to create msg evm transaction", "error", err)
					continue
				}

				gasUsedEstimate := loadTx.EthTx.Gas() // Use gas limit from transaction

				txBuilder := txConfig.NewTxBuilder()
				if err = txBuilder.SetMsgs(msg); err != nil {
					logger.Error("failed to set msgs", "error", err)
					continue
				}
				txBuilder.SetGasEstimate(gasUsedEstimate)

				txbz, encodeErr := txConfig.TxEncoder()(txBuilder.GetTx())
				if encodeErr != nil {
					logger.Error("failed to encode tx", "error", encodeErr)
					continue
				}

				// Filter by MaxTxBytes - stop adding transactions if we exceed the limit
				txSize := int64(len(txbz))
				if totalBytes+txSize > maxTxBytes {
					break
				}
				totalBytes += txSize

				txRecords = append(txRecords, &abci.TxRecord{
					Action: abci.TxRecord_UNMODIFIED,
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
