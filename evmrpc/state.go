package evmrpc

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	iavlstore "github.com/cosmos/cosmos-sdk/store/iavl"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/sei-protocol/sei-chain/x/evm/keeper"
	"github.com/sei-protocol/sei-chain/x/evm/types"
	abci "github.com/tendermint/tendermint/abci/types"
	"github.com/tendermint/tendermint/proto/tendermint/crypto"
	rpcclient "github.com/tendermint/tendermint/rpc/client"
	"github.com/tendermint/tendermint/rpc/coretypes"
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

// Result structs for GetProof
// This differs from go-ethereum AccountResult in two ways:
// 1. Proof object is an iavl proof, not a trie proof
// 2. Per-account fields are excluded because there is no per-account root
type ProofResult struct {
	Address      common.Address     `json:"address"`
	HexValues    []string           `json:"hexValues"`
	StorageProof []*crypto.ProofOps `json:"storageProof"`
}

func (a *StateAPI) GetProof(ctx context.Context, address common.Address, storageKeys []string, blockNrOrHash rpc.BlockNumberOrHash) (*ProofResult, error) {
	var block *coretypes.ResultBlock
	var err error
	if blockNr, ok := blockNrOrHash.Number(); ok {
		blockNumber, blockNumErr := getBlockNumber(ctx, a.tmClient, blockNr)
		if blockNumErr != nil {
			return nil, blockNumErr
		}
		block, err = a.tmClient.Block(ctx, blockNumber)
	} else {
		block, err = a.tmClient.BlockByHash(ctx, blockNrOrHash.BlockHash[:])
	}
	if err != nil {
		return nil, err
	}
	sdkCtx := a.ctxProvider(block.Block.Height)
	iavl, ok := sdkCtx.MultiStore().GetKVStore((a.keeper.GetStoreKey())).(*iavlstore.Store)
	if !ok {
		return nil, errors.New("cannot find EVM IAVL store")
	}
	result := ProofResult{Address: address}
	for _, key := range storageKeys {
		paddedKey := common.BytesToHash([]byte(key))
		formattedKey := append(types.StateKey(address), paddedKey[:]...)
		qres := iavl.Query(abci.RequestQuery{
			Path:   "/key",
			Data:   formattedKey,
			Height: block.Block.Height,
			Prove:  true,
		})
		result.HexValues = append(result.HexValues, hex.EncodeToString(qres.Value))
		result.StorageProof = append(result.StorageProof, qres.ProofOps)
	}

	return &result, nil
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
