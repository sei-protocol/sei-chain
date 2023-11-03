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

func (s *SendAPI) SendRawTransaction(ctx context.Context, input hexutil.Bytes) (common.Hash, error) {
	tx := new(ethtypes.Transaction)
	if err := tx.UnmarshalBinary(input); err != nil {
		return common.Hash{}, err
	}
	txdata, err := ethtx.NewTxDataFromTx(tx)
	if err != nil {
		return common.Hash{}, err
	}
	msg, err := types.NewMsgEVMTransaction(txdata)
	if err != nil {
		return common.Hash{}, err
	}
	txBuilder := s.txConfig.NewTxBuilder()
	if err := txBuilder.SetMsgs(msg); err != nil {
		return common.Hash{}, err
	}
	txbz, err := s.txConfig.TxEncoder()(txBuilder.GetTx())
	if err != nil {
		return common.Hash{}, err
	}
	res, err := s.tmClient.BroadcastTx(ctx, txbz)
	if err != nil || res == nil || res.Code != 0 {
		code := -1
		if res != nil {
			code = int(res.Code)
		}
		return common.Hash{}, fmt.Errorf("res: %d, error: %s", code, err)
	}
	return tx.Hash(), nil
}
