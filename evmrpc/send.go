package evmrpc

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/export"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/ethereum/go-ethereum/signer/core/apitypes"
	"github.com/sei-protocol/sei-chain/precompiles/wasmd"
	"github.com/sei-protocol/sei-chain/x/evm/keeper"
	"github.com/sei-protocol/sei-chain/x/evm/types"
	"github.com/sei-protocol/sei-chain/x/evm/types/ethtx"
	rpcclient "github.com/tendermint/tendermint/rpc/client"
)

type SendAPI struct {
	tmClient         rpcclient.Client
	txConfigProvider func(int64) client.TxConfig
	sendConfig       *SendConfig
	keeper           *keeper.Keeper
	ctxProvider      func(int64) sdk.Context
	homeDir          string
	backend          *Backend
	connectionType   ConnectionType
}

type SendConfig struct {
	slow bool
}

func NewSendAPI(tmClient rpcclient.Client, txConfigProvider func(int64) client.TxConfig, sendConfig *SendConfig, k *keeper.Keeper, ctxProvider func(int64) sdk.Context, homeDir string, simulateConfig *SimulateConfig, app *baseapp.BaseApp,
	antehandler sdk.AnteHandler, connectionType ConnectionType) *SendAPI {
	return &SendAPI{
		tmClient:         tmClient,
		txConfigProvider: txConfigProvider,
		sendConfig:       sendConfig,
		keeper:           k,
		ctxProvider:      ctxProvider,
		homeDir:          homeDir,
		backend:          NewBackend(ctxProvider, k, txConfigProvider, tmClient, simulateConfig, app, antehandler),
		connectionType:   connectionType,
	}
}

func (s *SendAPI) SendRawTransaction(ctx context.Context, input hexutil.Bytes) (hash common.Hash, err error) {
	startTime := time.Now()
	defer recordMetrics("eth_sendRawTransaction", s.connectionType, startTime, err == nil)
	tx := new(ethtypes.Transaction)
	if err = tx.UnmarshalBinary(input); err != nil {
		return
	}
	hash = tx.Hash()
	txData, err := ethtx.NewTxDataFromTx(tx)
	if err != nil {
		return
	}
	msg, err := types.NewMsgEVMTransaction(txData)
	if err != nil {
		return
	}
	gasUsedEstimate, err := s.simulateTx(ctx, tx)
	if err != nil {
		tx, _ = msg.AsTransaction()
		gasUsedEstimate = tx.Gas() // if issue simulating, fallback to gas limit
	}
	txBuilder := s.txConfigProvider(LatestCtxHeight).NewTxBuilder()
	if err = txBuilder.SetMsgs(msg); err != nil {
		return
	}
	txBuilder.SetGasEstimate(gasUsedEstimate)
	txbz, encodeErr := s.txConfigProvider(LatestCtxHeight).TxEncoder()(txBuilder.GetTx())
	if encodeErr != nil {
		return hash, encodeErr
	}

	if s.sendConfig.slow {
		res, broadcastError := s.tmClient.BroadcastTxCommit(ctx, txbz)
		if broadcastError != nil {
			err = broadcastError
		} else if res == nil {
			err = errors.New("missing broadcast response")
		} else if res.CheckTx.Code != 0 {
			err = sdkerrors.ABCIError(sdkerrors.RootCodespace, res.CheckTx.Code, "")
		}
	} else {
		res, broadcastError := s.tmClient.BroadcastTx(ctx, txbz)
		if broadcastError != nil {
			err = broadcastError
		} else if res == nil {
			err = errors.New("missing broadcast response")
		} else if res.Code != 0 {
			err = sdkerrors.ABCIError(sdkerrors.RootCodespace, res.Code, "")
		}
	}
	return
}

func (s *SendAPI) simulateTx(ctx context.Context, tx *ethtypes.Transaction) (estimate uint64, err error) {
	var from common.Address
	if tx.Type() == ethtypes.DynamicFeeTxType {
		signer := ethtypes.NewLondonSigner(s.keeper.ChainID(s.ctxProvider(LatestCtxHeight)))
		from, err = signer.Sender(tx)
		if err != nil {
			err = fmt.Errorf("failed to get sender for dynamic fee tx: %w", err)
			return
		}
	} else if tx.Protected() {
		signer := ethtypes.NewEIP155Signer(s.keeper.ChainID(s.ctxProvider(LatestCtxHeight)))
		from, err = signer.Sender(tx)
		if err != nil {
			err = fmt.Errorf("failed to get sender for protected tx: %w", err)
			return
		}
	} else {
		signer := ethtypes.HomesteadSigner{}
		from, err = signer.Sender(tx)
		if err != nil {
			err = fmt.Errorf("failed to get sender for homestead tx: %w", err)
			return
		}
	}
	input_ := (hexutil.Bytes)(tx.Data())
	gas_ := hexutil.Uint64(tx.Gas())
	nonce_ := hexutil.Uint64(tx.Nonce())
	al := tx.AccessList()
	bNrOrHash := rpc.BlockNumberOrHashWithNumber(rpc.LatestBlockNumber)
	ctx = context.WithValue(ctx, CtxIsWasmdPrecompileCallKey, wasmd.IsWasmdCall(tx.To()))
	gp := tx.GasPrice()
	maxFeePerGas := tx.GasFeeCap()
	maxPriorityFeePerGas := tx.GasTipCap()
	if gp != nil {
		maxFeePerGas = nil
		maxPriorityFeePerGas = nil
	} else {
		gp = nil
	}
	txArgs := export.TransactionArgs{
		From:                 &from,
		To:                   tx.To(),
		Gas:                  &gas_,
		GasPrice:             (*hexutil.Big)(gp),
		MaxFeePerGas:         (*hexutil.Big)(maxFeePerGas),
		MaxPriorityFeePerGas: (*hexutil.Big)(maxPriorityFeePerGas),
		Value:                (*hexutil.Big)(tx.Value()),
		Nonce:                &nonce_,
		Input:                &input_,
		AccessList:           &al,
		ChainID:              (*hexutil.Big)(tx.ChainId()),
	}
	estimate_, err := export.DoEstimateGas(ctx, s.backend, txArgs, bNrOrHash, nil, nil, s.backend.RPCGasCap())
	if err != nil {
		err = fmt.Errorf("failed to estimate gas: %w", err)
		return
	}
	return uint64(estimate_), nil
}

func (s *SendAPI) SignTransaction(_ context.Context, args apitypes.SendTxArgs, _ *string) (result *export.SignTransactionResult, returnErr error) {
	startTime := time.Now()
	defer recordMetrics("eth_signTransaction", s.connectionType, startTime, returnErr == nil)
	unsignedTx, err := args.ToTransaction()
	if err != nil {
		return nil, err
	}
	signedTx, err := s.signTransaction(unsignedTx, args.From.Address().Hex())
	if err != nil {
		return nil, err
	}
	data, err := signedTx.MarshalBinary()
	if err != nil {
		return nil, err
	}
	return &export.SignTransactionResult{Raw: data, Tx: signedTx}, nil
}

func (s *SendAPI) SendTransaction(ctx context.Context, args export.TransactionArgs) (result common.Hash, returnErr error) {
	startTime := time.Now()
	defer recordMetrics("eth_sendTransaction", s.connectionType, startTime, returnErr == nil)
	if err := args.SetDefaults(ctx, s.backend, false); err != nil {
		return common.Hash{}, err
	}
	var unsignedTx = args.ToTransaction(ethtypes.LegacyTxType)
	signedTx, err := s.signTransaction(unsignedTx, args.From.Hex())
	if err != nil {
		return common.Hash{}, err
	}
	data, err := signedTx.MarshalBinary()
	if err != nil {
		return common.Hash{}, err
	}
	return s.SendRawTransaction(ctx, data)
}

func (s *SendAPI) signTransaction(unsignedTx *ethtypes.Transaction, from string) (*ethtypes.Transaction, error) {
	kb, err := getTestKeyring(s.homeDir)
	if err != nil {
		return nil, err
	}
	privKey, ok := getAddressPrivKeyMap(kb)[from]
	if !ok {
		return nil, errors.New("from address does not have hosted key")
	}
	chainId := s.keeper.ChainID(s.ctxProvider(LatestCtxHeight))
	signer := ethtypes.LatestSignerForChainID(chainId)
	return ethtypes.SignTx(unsignedTx, signer, privKey)
}
