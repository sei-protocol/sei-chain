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
	ethtests "github.com/ethereum/go-ethereum/tests"
	"github.com/sei-protocol/sei-chain/utils"
	evmtypes "github.com/sei-protocol/sei-chain/x/evm/types"
	"github.com/sei-protocol/sei-chain/x/evm/types/ethtx"
	abci "github.com/tendermint/tendermint/abci/types"
	tmtypes "github.com/tendermint/tendermint/types"
)

// TODO: make another function called ReplayJson()
func Replay(a *App) {
	gendoc, err := tmtypes.GenesisDocFromFile(filepath.Join(DefaultNodeHome, "config/genesis.json"))
	if err != nil {
		panic(err)
	}
	fmt.Println("Calling InitChain")
	_, err = a.InitChain(context.Background(), &abci.RequestInitChain{
		Time:          time.Now(),
		ChainId:       gendoc.ChainID,
		AppStateBytes: gendoc.AppState,
	})
	if err != nil {
		panic(err)
	}
	for h := int64(1); h <= int64(a.EvmKeeper.EthReplayConfig.NumBlocksToReplay); h++ {
		a.Logger().Info(fmt.Sprintf("Replaying block height %d", h+int64(a.EvmKeeper.EthReplayConfig.EthDataEarliestBlock)))
		b, err := a.EvmKeeper.EthClient.BlockByNumber(context.Background(), big.NewInt(h+int64(a.EvmKeeper.EthReplayConfig.EthDataEarliestBlock))) // TODO: refactor this out
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
		ctx := a.GetContextForDeliverTx([]byte{})
		for _, tx := range b.Txs {
			a.Logger().Info(fmt.Sprintf("Verifying tx %s", tx.Hash().Hex()))
			if tx.To() != nil {
				a.EvmKeeper.VerifyBalance(ctx, *tx.To())
			}
			a.EvmKeeper.VerifyTxResult(ctx, tx.Hash())
		}
		_, err = a.Commit(context.Background())
		if err != nil {
			panic(err)
		}
	}
}

func ReplayBlockTest(a *App, bt *ethtests.BlockTest) {
	fmt.Println("In ReplayBlockTest")
	a.EvmKeeper.BlockTest = bt
	gendoc, err := tmtypes.GenesisDocFromFile(filepath.Join(DefaultNodeHome, "config/genesis.json"))
	if err != nil {
		panic(err)
	}
	fmt.Println("Calling InitChain")
	_, err = a.InitChain(context.Background(), &abci.RequestInitChain{
		Time:          time.Now(),
		ChainId:       gendoc.ChainID,
		AppStateBytes: gendoc.AppState,
	})
	if err != nil {
		panic(err)
	}

	// TODO: iterate over blocks
	// TODO:
	fmt.Println("In ReplayBlockTest, iterating over blocks, len(bt.Json.Blocks) = ", len(bt.Json.Blocks))
	for i, btBlock := range bt.Json.Blocks {
		h := int64(i + 1)
		b, err := btBlock.Decode()
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
		ctx := a.GetContextForDeliverTx([]byte{})
		for _, tx := range b.Txs {
			a.Logger().Info(fmt.Sprintf("Verifying tx %s", tx.Hash().Hex()))
			if tx.To() != nil {
				a.EvmKeeper.VerifyBalance(ctx, *tx.To())
			}
			a.EvmKeeper.VerifyTxResult(ctx, tx.Hash())
		}
		_, err = a.Commit(context.Background())
		if err != nil {
			panic(err)
		}
		h++
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
