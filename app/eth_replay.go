package app

import (
	"context"
	"encoding/binary"
	"fmt"
	"math/big"
	"path/filepath"
	"time"

	"github.com/cosmos/cosmos-sdk/client"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/sei-protocol/sei-chain/utils"
	evmtypes "github.com/sei-protocol/sei-chain/x/evm/types"
	"github.com/sei-protocol/sei-chain/x/evm/types/ethtx"
	abci "github.com/tendermint/tendermint/abci/types"
	tmtypes "github.com/tendermint/tendermint/types"
)

func Replay(a *App) {
	gendoc, err := tmtypes.GenesisDocFromFile(filepath.Join(DefaultNodeHome, "config/genesis.json"))
	if err != nil {
		panic(err)
	}
	_, err = a.InitChain(context.Background(), &abci.RequestInitChain{
		Time:          time.Now(),
		ChainId:       "sei-chain",
		AppStateBytes: gendoc.AppState,
	})
	if err != nil {
		panic(err)
	}
	for h := int64(1); h <= int64(a.EvmKeeper.EthReplayConfig.NumBlocksToReplay); h++ {
		fmt.Printf("Replaying block height %d\n", h+int64(a.EvmKeeper.EthReplayConfig.EthDataEarliestBlock))
		b, err := a.EvmKeeper.EthClient.BlockByNumber(context.Background(), big.NewInt(h+int64(a.EvmKeeper.EthReplayConfig.EthDataEarliestBlock)))
		if err != nil {
			panic(err)
		}
		hash := make([]byte, 8)
		binary.BigEndian.PutUint64(hash, uint64(h))
		_, err = a.FinalizeBlock(context.Background(), &abci.RequestFinalizeBlock{
			Txs:               utils.Map(b.Txs, func(tx *ethtypes.Transaction) []byte { return encodeTx(tx, a.GetTxConfig()) }),
			DecidedLastCommit: abci.CommitInfo{Votes: []abci.VoteInfo{}},
			Height:            h,
			Hash:              hash,
			Time:              time.Now(),
		})
		if err != nil {
			panic(err)
		}
		for i, tx := range b.Txs {
			if tx.To() != nil {
				fmt.Printf("Verifying balance of tx %d\n", i)
				a.EvmKeeper.VerifyBalance(a.GetContextForDeliverTx([]byte{}), *tx.To())
			}
		}
		_, err = a.Commit(context.Background())
		if err != nil {
			panic(err)
		}
	}
}

func encodeTx(tx *ethtypes.Transaction, txConfig client.TxConfig) []byte {
	var txData ethtx.TxData
	var err error
	switch tx.Type() {
	case ethtypes.LegacyTxType:
		txData, err = ethtx.NewLegacyTx(tx)
	case ethtypes.DynamicFeeTxType:
		txData, err = ethtx.NewDynamicFeeTx(tx)
	case ethtypes.AccessListTxType:
		txData, err = ethtx.NewAccessListTx(tx)
	case ethtypes.BlobTxType:
		txData, err = ethtx.NewBlobTx(tx)
	}
	if err != nil {
		panic(err)
	}
	msg, err := evmtypes.NewMsgEVMTransaction(txData)
	if err != nil {
		panic(err)
	}
	txBuilder := txConfig.NewTxBuilder()
	if err = txBuilder.SetMsgs(msg); err != nil {
		panic(err)
	}
	txbz, encodeErr := txConfig.TxEncoder()(txBuilder.GetTx())
	if encodeErr != nil {
		panic(encodeErr)
	}
	return txbz
}
