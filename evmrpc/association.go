package evmrpc

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/sei-protocol/sei-chain/x/evm/keeper"
	"github.com/sei-protocol/sei-chain/x/evm/types"
	"github.com/sei-protocol/sei-chain/x/evm/types/ethtx"
	rpcclient "github.com/tendermint/tendermint/rpc/client"
)

type AssociationAPI struct {
	tmClient       rpcclient.Client
	keeper         *keeper.Keeper
	ctxProvider    func(int64) sdk.Context
	txDecoder      sdk.TxDecoder
	sendAPI        *SendAPI
	connectionType ConnectionType
}

func NewAssociationAPI(tmClient rpcclient.Client, k *keeper.Keeper, ctxProvider func(int64) sdk.Context, txDecoder sdk.TxDecoder, sendAPI *SendAPI, connectionType ConnectionType) *AssociationAPI {
	return &AssociationAPI{tmClient: tmClient, keeper: k, ctxProvider: ctxProvider, txDecoder: txDecoder, sendAPI: sendAPI, connectionType: connectionType}
}

type AssociateRequest struct {
	R             string `json:"r"`
	S             string `json:"s"`
	V             string `json:"v"`
	CustomMessage string `json:"custom_message"`
}

func (t *AssociationAPI) Associate(ctx context.Context, req *AssociateRequest) (returnErr error) {
	startTime := time.Now()
	defer recordMetrics("sei_associate", t.connectionType, startTime, returnErr == nil)
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
	txBuilder := t.sendAPI.txConfig.NewTxBuilder()
	if err = txBuilder.SetMsgs(msg); err != nil {
		return err
	}
	txbz, encodeErr := t.sendAPI.txConfig.TxEncoder()(txBuilder.GetTx())
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
	defer recordMetrics("sei_getSeiAddress", t.connectionType, startTime, returnErr == nil)
	seiAddress, found := t.keeper.GetSeiAddress(t.ctxProvider(LatestCtxHeight), ethAddress)
	if !found {
		return "", fmt.Errorf("failed to find Sei address for %s", ethAddress.Hex())
	}

	return seiAddress.String(), nil
}

func (t *AssociationAPI) GetEVMAddress(_ context.Context, seiAddress string) (result string, returnErr error) {
	startTime := time.Now()
	defer recordMetrics("sei_getEVMAddress", t.connectionType, startTime, returnErr == nil)
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
	defer recordMetrics("sei_getCosmosTx", t.connectionType, startTime, returnErr == nil)
	receipt, err := t.keeper.GetReceipt(t.ctxProvider(LatestCtxHeight), ethHash)
	if err != nil {
		return "", err
	}
	height := int64(receipt.BlockNumber)
	number := rpc.BlockNumber(height)
	numberPtr, err := getBlockNumber(ctx, t.tmClient, number)
	if err != nil {
		return "", err
	}
	block, err := blockByNumberWithRetry(ctx, t.tmClient, numberPtr, 1)
	if err != nil {
		return "", err
	}
	blockRes, err := blockResultsWithRetry(ctx, t.tmClient, &height)
	if err != nil {
		return "", err
	}
	for i := range blockRes.TxsResults {
		tmTx := block.Block.Txs[i]
		decoded, err := t.txDecoder(block.Block.Txs[i])
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
	defer recordMetrics("sei_getEvmTx", t.connectionType, startTime, returnErr == nil)
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
