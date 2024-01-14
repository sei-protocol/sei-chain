package evmrpc

import (
	"context"
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/sei-protocol/sei-chain/x/evm/keeper"
	"github.com/sei-protocol/sei-chain/x/evm/types/ethtx"
	rpcclient "github.com/tendermint/tendermint/rpc/client"
)

type AssociationAPI struct {
	tmClient    rpcclient.Client
	keeper      *keeper.Keeper
	ctxProvider func(int64) sdk.Context
	txDecoder   sdk.TxDecoder
	sendAPI     *SendAPI
}

func NewAssociationAPI(tmClient rpcclient.Client, k *keeper.Keeper, ctxProvider func(int64) sdk.Context, txDecoder sdk.TxDecoder, sendAPI *SendAPI) *AssociationAPI {
	return &AssociationAPI{tmClient: tmClient, keeper: k, ctxProvider: ctxProvider, txDecoder: txDecoder, sendAPI: sendAPI}
}

type AssociateRequest struct {
	R *hexutil.Big `json:"r"`
	S *hexutil.Big `json:"s"`
	V *hexutil.Big `json:"v"`
}

func (t *AssociationAPI) Associate(ctx context.Context, req *AssociateRequest) error {
	/// Old code [START]
	associateTx := ethtx.AssociateTx{
		R: req.R.ToInt().Bytes(),
		S: req.S.ToInt().Bytes(),
		V: req.V.ToInt().Bytes(),
	}
	/// Old code [END]

	// ///// New code [START]
	// vBytes, err := req.V.MarshalText()
	// if err != nil {
	// 	return err
	// }
	// rBytes, err := req.R.MarshalText()
	// if err != nil {
	// 	return err
	// }
	// sBytes, err := req.S.MarshalText()
	// if err != nil {
	// 	return err
	// }
	// associateTx := ethtx.AssociateTx{
	// 	V: vBytes,
	// 	R: rBytes,
	// 	S: sBytes,
	// }
	// ///// New code [END]

	data, err := associateTx.Marshal()
	if err != nil {
		return err
	}
	_, err = t.sendAPI.SendRawTransaction(ctx, data)
	if err != nil {
		return err
	}
	return nil
}

func (t *AssociationAPI) GetSeiAddress(ctx context.Context, ethAddress common.Address) (string, error) {
	seiAddress, found := t.keeper.GetSeiAddress(t.ctxProvider(LatestCtxHeight), ethAddress)
	if !found {
		return "", fmt.Errorf("failed to find Sei address for %s", ethAddress.Hex())
	}

	return seiAddress.String(), nil
}

func (t *AssociationAPI) GetEVMAddress(ctx context.Context, seiAddress string) (string, error) {
	ethAddress, found := t.keeper.GetEVMAddress(t.ctxProvider(LatestCtxHeight), sdk.MustAccAddressFromBech32(seiAddress))
	if !found {
		return "", fmt.Errorf("failed to find EVM address for %s", seiAddress)
	}

	return ethAddress.Hex(), nil
}
