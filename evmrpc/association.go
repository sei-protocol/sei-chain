package evmrpc

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/sei-protocol/sei-chain/sei-cosmos/client"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/sei-protocol/sei-chain/x/evm/keeper"
)

type AssociationAPI struct {
	keeper         *keeper.Keeper
	tmClient       client.LocalClient
	ctxProvider    func(int64) sdk.Context
	connectionType ConnectionType
	watermarks     *WatermarkManager
}

func NewAssociationAPI(
	tmClient client.LocalClient,
	k *keeper.Keeper,
	ctxProvider func(int64) sdk.Context,
	connectionType ConnectionType,
	watermarks *WatermarkManager,
) *AssociationAPI {
	return &AssociationAPI{
		keeper:         k,
		tmClient:       tmClient,
		ctxProvider:    ctxProvider,
		connectionType: connectionType,
		watermarks:     watermarks,
	}
}

func (t *AssociationAPI) GetSeiAddress(ctx context.Context, ethAddress common.Address) (result string, returnErr error) {
	startTime := time.Now()
	defer func() {
		recordMetricsWithError(ctx, "sei_getSeiAddress", t.connectionType, startTime, returnErr, recover())
	}()
	seiAddress, found := t.keeper.GetSeiAddress(t.ctxProvider(LatestCtxHeight), ethAddress)
	if !found {
		return "", fmt.Errorf("failed to find Sei address for %s", ethAddress.Hex())
	}

	return seiAddress.String(), nil
}

func (t *AssociationAPI) GetEVMAddress(ctx context.Context, seiAddress string) (result string, returnErr error) {
	startTime := time.Now()
	defer func() {
		recordMetricsWithError(ctx, "sei_getEVMAddress", t.connectionType, startTime, returnErr, recover())
	}()
	seiAddr, err := sdk.AccAddressFromBech32(seiAddress)
	if err != nil {
		return "", err
	}
	ethAddress, found := t.keeper.GetEVMAddress(t.ctxProvider(LatestCtxHeight), seiAddr)
	if !found {
		return "", fmt.Errorf("failed to find EVM address for %s", seiAddress)
	}

	return ethAddress.Hex(), nil
}

func (t *AssociationAPI) GetCosmosTx(ctx context.Context, ethHash common.Hash) (result string, returnErr error) {
	startTime := time.Now()
	defer func() {
		recordMetricsWithError(ctx, "sei_getCosmosTx", t.connectionType, startTime, returnErr, recover())
	}()
	receipt, err := t.keeper.GetReceipt(t.ctxProvider(LatestCtxHeight), ethHash)
	if err != nil {
		return "", err
	}
	if receipt.BlockNumber > math.MaxInt64 {
		return "", fmt.Errorf("invalid block number: %d", receipt.BlockNumber)
	}
	height := int64(receipt.BlockNumber) //nolint:gosec
	block, err := blockByNumberRespectingWatermarks(ctx, t.tmClient, t.watermarks, &height, 1)
	if err != nil {
		return "", err
	}
	if int(receipt.TransactionIndex) >= len(block.Block.Txs) {
		return "", fmt.Errorf("transaction index %d out of range (block has %d txs)", receipt.TransactionIndex, len(block.Block.Txs))
	}
	return fmt.Sprintf("%X", block.Block.Txs[receipt.TransactionIndex].Hash()), nil
}
