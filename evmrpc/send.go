package evmrpc

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/lib/ethapi"
	"github.com/ethereum/go-ethereum/signer/core/apitypes"
	"github.com/sei-protocol/sei-chain/x/evm/keeper"
	"github.com/sei-protocol/sei-chain/x/evm/types"
	"github.com/sei-protocol/sei-chain/x/evm/types/ethtx"
	rpcclient "github.com/tendermint/tendermint/rpc/client"
)

type SendAPI struct {
	tmClient    rpcclient.Client
	txConfig    client.TxConfig
	sendConfig  *SendConfig
	slowMu      *sync.Mutex
	keeper      *keeper.Keeper
	ctxProvider func(int64) sdk.Context
	homeDir     string
	backend     *Backend
}

type SendConfig struct {
	slow bool
}

func NewSendAPI(tmClient rpcclient.Client, txConfig client.TxConfig, sendConfig *SendConfig, k *keeper.Keeper, ctxProvider func(int64) sdk.Context, homeDir string, simulateConfig *SimulateConfig) *SendAPI {
	return &SendAPI{
		tmClient:    tmClient,
		txConfig:    txConfig,
		sendConfig:  sendConfig,
		slowMu:      &sync.Mutex{},
		keeper:      k,
		ctxProvider: ctxProvider,
		homeDir:     homeDir,
		backend:     NewBackend(ctxProvider, k, tmClient, simulateConfig),
	}
}

func (s *SendAPI) SendRawTransaction(ctx context.Context, input hexutil.Bytes) (hash common.Hash, err error) {
	if s.sendConfig.slow {
		s.slowMu.Lock()
		defer s.slowMu.Unlock()
	}
	var txData ethtx.TxData
	associateTx := ethtx.AssociateTx{}
	if associateTx.Unmarshal(input) == nil {
		txData = &associateTx
	} else {
		tx := new(ethtypes.Transaction)
		if err = tx.UnmarshalBinary(input); err != nil {
			return
		}
		hash = tx.Hash()
		txData, err = ethtx.NewTxDataFromTx(tx)
		if err != nil {
			return
		}
	}
	msg, err := types.NewMsgEVMTransaction(txData)
	if err != nil {
		return
	}
	txBuilder := s.txConfig.NewTxBuilder()
	if err = txBuilder.SetMsgs(msg); err != nil {
		return
	}
	txbz, encodeErr := s.txConfig.TxEncoder()(txBuilder.GetTx())
	if encodeErr != nil {
		return hash, encodeErr
	}
	if s.sendConfig.slow {
		res, broadcastError := s.tmClient.BroadcastTxCommit(ctx, txbz)
		if broadcastError != nil || res == nil || res.CheckTx.Code != 0 {
			code := -1
			if res != nil {
				code = int(res.CheckTx.Code)
			}
			return hash, fmt.Errorf("res: %d, error: %s", code, broadcastError)
		}
	} else {
		res, broadcastError := s.tmClient.BroadcastTx(ctx, txbz)
		if broadcastError != nil || res == nil || res.Code != 0 {
			code := -1
			if res != nil {
				code = int(res.Code)
			}
			return hash, fmt.Errorf("res: %d, error: %s", code, broadcastError)
		}
	}
	return
}

func (s *SendAPI) SignTransaction(_ context.Context, args apitypes.SendTxArgs, _ *string) (*ethapi.SignTransactionResult, error) {
	var unsignedTx = args.ToTransaction()
	signedTx, err := s.signTransaction(unsignedTx, args.From.Address().Hex())
	if err != nil {
		return nil, err
	}
	data, err := signedTx.MarshalBinary()
	if err != nil {
		return nil, err
	}
	return &ethapi.SignTransactionResult{Raw: data, Tx: signedTx}, nil
}

func (s *SendAPI) SendTransaction(ctx context.Context, args ethapi.TransactionArgs) (common.Hash, error) {
	if err := args.SetDefaults(ctx, s.backend); err != nil {
		return common.Hash{}, err
	}
	var unsignedTx = args.ToTransaction()
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
	signer := ethtypes.LatestSignerForChainID(s.keeper.ChainID(s.ctxProvider(LatestCtxHeight)))
	return ethtypes.SignTx(unsignedTx, signer, privKey)
}
