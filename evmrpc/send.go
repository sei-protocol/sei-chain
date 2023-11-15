package evmrpc

import (
	"context"
	"fmt"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/sei-protocol/sei-chain/x/evm/types"
	"github.com/sei-protocol/sei-chain/x/evm/types/ethtx"
	rpcclient "github.com/tendermint/tendermint/rpc/client"
)

type SendAPI struct {
	tmClient rpcclient.Client
	txConfig client.TxConfig
}

func NewSendAPI(tmClient rpcclient.Client, txConfig client.TxConfig) *SendAPI {
	return &SendAPI{tmClient: tmClient, txConfig: txConfig}
}

func (s *SendAPI) SendRawTransaction(ctx context.Context, input hexutil.Bytes) (hash common.Hash, err error) {
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
	res, broadcastError := s.tmClient.BroadcastTx(ctx, txbz)
	if broadcastError != nil || res == nil || res.Code != 0 {
		code := -1
		if res != nil {
			code = int(res.Code)
		}
		return hash, fmt.Errorf("res: %d, error: %s", code, broadcastError)
	}
	return
}
