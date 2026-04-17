package evmrpc

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/sei-protocol/sei-chain/sei-cosmos/client"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	sdkerrors "github.com/sei-protocol/sei-chain/sei-cosmos/types/errors"
	rpcclient "github.com/sei-protocol/sei-chain/sei-tendermint/rpc/client"
	"github.com/sei-protocol/sei-chain/x/evm/keeper"
	"github.com/sei-protocol/sei-chain/x/evm/types"
	"github.com/sei-protocol/sei-chain/x/evm/types/ethtx"
)

type AssociationAPI struct {
	tmClient         rpcclient.Client
	keeper           *keeper.Keeper
	ctxProvider      func(int64) sdk.Context
	txConfigProvider func(int64) client.TxConfig
	sendAPI          *SendAPI
	connectionType   ConnectionType
	watermarks       *WatermarkManager
}

func NewAssociationAPI(
	tmClient rpcclient.Client,
	k *keeper.Keeper,
	ctxProvider func(int64) sdk.Context,
	txConfigProvider func(int64) client.TxConfig,
	sendAPI *SendAPI,
	connectionType ConnectionType,
	watermarks *WatermarkManager,
) *AssociationAPI {
	return &AssociationAPI{
		tmClient:         tmClient,
		keeper:           k,
		ctxProvider:      ctxProvider,
		txConfigProvider: txConfigProvider,
		sendAPI:          sendAPI,
		connectionType:   connectionType,
		watermarks:       watermarks,
	}
}

type AssociateRequest struct {
	R             string `json:"r"`
	S             string `json:"s"`
	V             string `json:"v"`
	CustomMessage string `json:"custom_message"`
}

func (t *AssociationAPI) Associate(ctx context.Context, req *AssociateRequest) (returnErr error) {
	startTime := time.Now()
	defer recordMetricsWithError("sei_associate", t.connectionType, startTime, returnErr)
	rBytes, err := decodeHexString(req.R)
	if err != nil {
		return err
	}
	sBytes, err := decodeHexString(req.S)
	if err != nil {
		return err
	}
	vBytes, err := decodeHexString(req.V)
	if err != nil {
		return err
	}

	associateTx := ethtx.AssociateTx{
		V:             vBytes,
		R:             rBytes,
		S:             sBytes,
		CustomMessage: req.CustomMessage,
	}

	msg, err := types.NewMsgEVMTransaction(&associateTx)
	if err != nil {
		return err
	}
	txBuilder := t.sendAPI.txConfigProvider(LatestCtxHeight).NewTxBuilder()
	if err = txBuilder.SetMsgs(msg); err != nil {
		return err
	}
	txbz, encodeErr := t.sendAPI.txConfigProvider(LatestCtxHeight).TxEncoder()(txBuilder.GetTx())
	if encodeErr != nil {
		return encodeErr
	}

	res, broadcastError := t.tmClient.BroadcastTx(ctx, txbz)
	if broadcastError != nil {
		err = broadcastError
	} else if res == nil {
		err = errors.New("missing broadcast response")
	} else if res.Code != 0 {
		err = sdkerrors.ABCIError(sdkerrors.RootCodespace, res.Code, "")
	}

	return err
}

func (t *AssociationAPI) GetSeiAddress(_ context.Context, ethAddress common.Address) (result string, returnErr error) {
	startTime := time.Now()
	defer recordMetricsWithError("sei_getSeiAddress", t.connectionType, startTime, returnErr)
	seiAddress, found := t.keeper.GetSeiAddress(t.ctxProvider(LatestCtxHeight), ethAddress)
	if !found {
		return "", fmt.Errorf("failed to find Sei address for %s", ethAddress.Hex())
	}

	return seiAddress.String(), nil
}

func (t *AssociationAPI) GetEVMAddress(_ context.Context, seiAddress string) (result string, returnErr error) {
	startTime := time.Now()
	defer recordMetricsWithError("sei_getEVMAddress", t.connectionType, startTime, returnErr)
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

func decodeHexString(hexString string) ([]byte, error) {
	trimmed := strings.TrimPrefix(hexString, "0x")
	if len(trimmed)%2 != 0 {
		trimmed = "0" + trimmed
	}
	return hex.DecodeString(trimmed)
}

func (t *AssociationAPI) GetCosmosTx(ctx context.Context, ethHash common.Hash) (result string, returnErr error) {
	startTime := time.Now()
	defer recordMetricsWithError("sei_getCosmosTx", t.connectionType, startTime, returnErr)
	receipt, err := t.keeper.GetReceipt(t.ctxProvider(LatestCtxHeight), ethHash)
	if err != nil {
		return "", err
	}
	if receipt.BlockNumber > math.MaxInt64 {
		return "", fmt.Errorf("invalid block number: %d", receipt.BlockNumber)
	}
	height := int64(receipt.BlockNumber) //nolint:gosec
	number := rpc.BlockNumber(height)
	numberPtr, err := getBlockNumber(ctx, t.tmClient, number)
	if err != nil {
		return "", err
	}
	block, err := blockByNumberRespectingWatermarks(ctx, t.tmClient, t.watermarks, numberPtr, 1)
	if err != nil {
		return "", err
	}
	blockRes, err := blockResultsWithRetry(ctx, t.tmClient, &height)
	if err != nil {
		return "", err
	}
	for i := range blockRes.TxsResults {
		tmTx := block.Block.Txs[i]
		decoded, err := t.txConfigProvider(block.Block.Height).TxDecoder()(block.Block.Txs[i])
		if err != nil {
			return "", err
		}
		for _, msg := range decoded.GetMsgs() {
			switch m := msg.(type) {
			case *types.MsgEVMTransaction:
				ethtx, _ := m.AsTransaction()
				hash := ethtx.Hash()
				if hash == ethHash {
					return fmt.Sprintf("%X", tmTx.Hash()), nil
				}
			}
		}
	}
	return "", fmt.Errorf("transaction not found")
}

func (t *AssociationAPI) GetEvmTx(ctx context.Context, cosmosHash string) (result string, returnErr error) {
	startTime := time.Now()
	defer recordMetricsWithError("sei_getEvmTx", t.connectionType, startTime, returnErr)
	hashBytes, err := hex.DecodeString(cosmosHash)
	if err != nil {
		return "", fmt.Errorf("failed to decode cosmosHash: %w", err)
	}

	txResponse, err := t.tmClient.Tx(ctx, hashBytes, false)
	if err != nil {
		return "", err
	}
	if txResponse.TxResult.EvmTxInfo == nil {
		return "", fmt.Errorf("transaction not found")
	}

	return txResponse.TxResult.EvmTxInfo.TxHash, nil
}
