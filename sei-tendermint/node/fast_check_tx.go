package node

import (
	"context"
	"fmt"

	ethtypes "github.com/ethereum/go-ethereum/core/types"
	gogoproto "github.com/gogo/protobuf/proto"

	"github.com/sei-protocol/sei-chain/sei-cosmos/types/tx"
	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/utils/helpers"
	evmtypes "github.com/sei-protocol/sei-chain/x/evm/types"
)

const (
	evmTxTypeURL     = "/seiprotocol.seichain.evm.MsgEVMTransaction"
	maxInt64AsUint64 = uint64(1<<63 - 1)
)

type fastCheckTxApplication struct {
	abci.Application
}

func (app fastCheckTxApplication) CheckTx(_ context.Context, req *abci.RequestCheckTxV2) *abci.ResponseCheckTxV2 {
	res, err := parseFastCheckTx(req.Tx)
	if err != nil {
		return &abci.ResponseCheckTxV2{ResponseCheckTx: &abci.ResponseCheckTx{Code: 1, Log: err.Error()}}
	}
	return res
}

func parseFastCheckTx(txBytes []byte) (*abci.ResponseCheckTxV2, error) {
	var rawTx tx.TxRaw
	if err := gogoproto.Unmarshal(txBytes, &rawTx); err != nil {
		return nil, err
	}
	var body tx.TxBody
	if err := gogoproto.Unmarshal(rawTx.BodyBytes, &body); err != nil {
		return nil, err
	}
	if len(body.Messages) != 1 {
		return nil, fmt.Errorf("fast check tx only accepts single-message EVM transactions")
	}
	anyMsg := body.Messages[0]
	if anyMsg == nil {
		return nil, fmt.Errorf("fast check tx received nil message")
	}
	if anyMsg.GetTypeUrl() != evmTxTypeURL {
		return nil, fmt.Errorf("fast check tx only accepts EVM transactions")
	}
	var msg evmtypes.MsgEVMTransaction
	if err := gogoproto.Unmarshal(anyMsg.Value, &msg); err != nil {
		return nil, err
	}
	ethTx, _ := msg.AsTransaction()
	if ethTx == nil {
		return nil, fmt.Errorf("failed to unpack EVM transaction")
	}
	gas, ok := utils.SafeCast[int64](ethTx.Gas())
	if !ok {
		return nil, fmt.Errorf("EVM gas wanted exceeds int64 max")
	}
	if !ethTx.Protected() {
		return nil, fmt.Errorf("unprotected EVM transaction")
	}
	signer := ethtypes.LatestSignerForChainID(ethTx.ChainId())
	evmAddr, seiAddr, _, err := helpers.RecoverAddressesFromTx(ethTx, signer, ethTx.ChainId())
	if err != nil {
		return nil, err
	}
	return &abci.ResponseCheckTxV2{
		ResponseCheckTx:  &abci.ResponseCheckTx{GasWanted: gas, GasEstimated: gas},
		IsEVM:            true,
		EVMNonce:         ethTx.Nonce(),
		EVMHash:          ethTx.Hash(),
		EVMSenderAddress: evmAddr,
		SeiSenderAddress: seiAddr,
	}, nil
}
