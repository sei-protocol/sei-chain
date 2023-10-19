package evmrpc

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/sei-protocol/sei-chain/x/evm/keeper"
	rpcclient "github.com/tendermint/tendermint/rpc/client"
)

type StateAPI struct {
	tmClient    rpcclient.Client
	keeper      *keeper.Keeper
	ctxProvider func(int64) sdk.Context
}

func NewStateAPI(tmClient rpcclient.Client, k *keeper.Keeper, ctxProvider func(int64) sdk.Context) *StateAPI {
	return &StateAPI{tmClient: tmClient, keeper: k, ctxProvider: ctxProvider}
}

func (a *StateAPI) GetBalance(ctx context.Context, address common.Address, blockNr rpc.BlockNumber) (*uint64, error) {
	block, err := getBlockNumber(ctx, a.tmClient, blockNr)
	if err != nil {
		return nil, err
	}
	if block != nil {
		return nil, errors.New("block number not safe, finalized, latest, or pending")
	}
	seiAddr, found := a.keeper.GetSeiAddress(a.ctxProvider(LatestCtxHeight), address)
	if found {
		coin := a.keeper.BankKeeper().GetBalance(a.ctxProvider(LatestCtxHeight), seiAddr, a.keeper.GetBaseDenom(a.ctxProvider(LatestCtxHeight)))
		balance := coin.Amount.BigInt().Uint64()
		return &balance, nil
	}
	balance := a.keeper.GetBalance(a.ctxProvider(LatestCtxHeight), address)
	return &balance, nil
}

func (a *StateAPI) GetCode(ctx context.Context, address common.Address, blockNr rpc.BlockNumber) ([]byte, error) {
	block, err := getBlockNumber(ctx, a.tmClient, blockNr)
	if err != nil {
		return nil, err
	}
	if block != nil {
		return nil, errors.New("block number not safe, finalized, latest, or pending")
	}
	code := a.keeper.GetCode(a.ctxProvider(LatestCtxHeight), address)
	return code, nil
}

func (a *StateAPI) GetStorageAt(ctx context.Context, address common.Address, hexKey string, blockNr rpc.BlockNumber) ([]byte, error) {
	block, err := getBlockNumber(ctx, a.tmClient, blockNr)
	if err != nil {
		return nil, err
	}
	if block != nil {
		return nil, errors.New("block number not safe, finalized, latest, or pending")
	}
	key, _, err := decodeHash(hexKey)
	if err != nil {
		return nil, fmt.Errorf("unable to decode storage key: %s", err)
	}
	state := a.keeper.GetState(a.ctxProvider(LatestCtxHeight), address, key)
	return state[:], nil
}

// decodeHash parses a hex-encoded 32-byte hash. The input may optionally
// be prefixed by 0x and can have a byte length up to 32.
func decodeHash(s string) (h common.Hash, inputLength int, err error) {
	if strings.HasPrefix(s, "0x") || strings.HasPrefix(s, "0X") {
		s = s[2:]
	}
	if (len(s) & 1) > 0 {
		s = "0" + s
	}
	b, err := hex.DecodeString(s)
	if err != nil {
		return common.Hash{}, 0, errors.New("hex string invalid")
	}
	if len(b) > 32 {
		return common.Hash{}, len(b), errors.New("hex string too long, want at most 32 bytes")
	}
	return common.BytesToHash(b), len(b), nil
}
