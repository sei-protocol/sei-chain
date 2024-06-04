package evmrpc

import (
	"context"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/sei-protocol/sei-chain/x/evm/keeper"
	"github.com/sei-protocol/sei-chain/x/evm/types"
	rpcclient "github.com/tendermint/tendermint/rpc/client"
)

type AssociationAPI struct {
	tmClient       rpcclient.Client
	keeper         *keeper.Keeper
	ctxProvider    func(int64) sdk.Context
	txDecoder      sdk.TxDecoder
	connectionType ConnectionType
}

func NewAssociationAPI(tmClient rpcclient.Client, k *keeper.Keeper, ctxProvider func(int64) sdk.Context, txDecoder sdk.TxDecoder, connectionType ConnectionType) *AssociationAPI {
	return &AssociationAPI{tmClient: tmClient, keeper: k, ctxProvider: ctxProvider, txDecoder: txDecoder, connectionType: connectionType}
}

func (t *AssociationAPI) GetSeiAddress(_ context.Context, ethAddress common.Address) (result string, returnErr error) {
	startTime := time.Now()
	defer recordMetrics("sei_getSeiAddress", t.connectionType, startTime, returnErr == nil)
	seiAddress := t.keeper.GetSeiAddress(t.ctxProvider(LatestCtxHeight), ethAddress)

	return seiAddress.String(), nil
}

func (t *AssociationAPI) GetEVMAddress(_ context.Context, seiAddress string) (result string, returnErr error) {
	startTime := time.Now()
	defer recordMetrics("sei_getEVMAddress", t.connectionType, startTime, returnErr == nil)
	seiAddr, err := sdk.AccAddressFromBech32(seiAddress)
	if err != nil {
		return "", err
	}
	ethAddress := t.keeper.GetEVMAddress(t.ctxProvider(LatestCtxHeight), seiAddr)

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
