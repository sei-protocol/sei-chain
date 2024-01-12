package evmrpc

import (
	"context"
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/sei-protocol/sei-chain/x/evm/keeper"
	rpcclient "github.com/tendermint/tendermint/rpc/client"
)

type AssociationAPI struct {
	tmClient    rpcclient.Client
	keeper      *keeper.Keeper
	ctxProvider func(int64) sdk.Context
	txDecoder   sdk.TxDecoder
}

func NewAssociationAPI(tmClient rpcclient.Client, k *keeper.Keeper, ctxProvider func(int64) sdk.Context, txDecoder sdk.TxDecoder) *AssociationAPI {
	return &AssociationAPI{tmClient: tmClient, keeper: k, ctxProvider: ctxProvider, txDecoder: txDecoder}
}

type AssociateRequest struct {
	R *hexutil.Big `json:"r"`
	S *hexutil.Big `json:"s"`
	V *hexutil.Big `json:"v"`
}

func (t *AssociationAPI) Associate(ctx context.Context, req *AssociateRequest) (map[string]string, error) {
	fmt.Println("In Associate")
	// Create a signature from r, s, v
	sig := make([]byte, 65)
	copy(sig[0:32], req.R.ToInt().Bytes())
	copy(sig[32:64], req.S.ToInt().Bytes())
	sig[64] = byte(req.V.ToInt().Uint64())

	// Recover the public key from the signature
	publicKey, err := crypto.SigToPub(crypto.Keccak256([]byte("")), sig)
	if err != nil {
		return nil, fmt.Errorf("failed to recover public key: %v", err)
	}

	// Get the Ethereum address from the public key
	ethAddress := crypto.PubkeyToAddress(*publicKey)

	// Convert the Ethereum address to a SEI address
	// This is a placeholder - replace with your actual conversion logic
	seiAddress := ethAddress.Hex() // replace with your conversion logic

	// Return the addresses
	return map[string]string{
		"ethAddress": ethAddress.Hex(),
		"seiAddress": seiAddress,
	}, nil
}
