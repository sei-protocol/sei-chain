package evmrpc

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/rpc"
	gigacachekv "github.com/sei-protocol/sei-chain/giga/deps/store"
	"github.com/sei-protocol/sei-chain/sei-cosmos/store/cachekv"
	"github.com/sei-protocol/sei-chain/sei-cosmos/store/prefix"
	"github.com/sei-protocol/sei-chain/sei-cosmos/store/tracekv"
	storetypes "github.com/sei-protocol/sei-chain/sei-cosmos/store/types"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/crypto"
	rpcclient "github.com/sei-protocol/sei-chain/sei-tendermint/rpc/client"
	"github.com/sei-protocol/sei-chain/sei-tendermint/rpc/coretypes"
	"github.com/sei-protocol/sei-chain/x/evm/keeper"
	"github.com/sei-protocol/sei-chain/x/evm/state"
	"github.com/sei-protocol/sei-chain/x/evm/types"
)

var errNoProofCapableQueryableKVStore = errors.New("cannot find a proof-capable queryable KV store")

type StateAPI struct {
	tmClient       rpcclient.Client
	keeper         *keeper.Keeper
	ctxProvider    func(int64) sdk.Context
	connectionType ConnectionType
	watermarks     *WatermarkManager
}

func NewStateAPI(tmClient rpcclient.Client, k *keeper.Keeper, ctxProvider func(int64) sdk.Context, connectionType ConnectionType, watermarks *WatermarkManager) *StateAPI {
	return &StateAPI{tmClient: tmClient, keeper: k, ctxProvider: ctxProvider, connectionType: connectionType, watermarks: watermarks}
}

func (a *StateAPI) GetBalance(ctx context.Context, address common.Address, blockNrOrHash rpc.BlockNumberOrHash) (result *hexutil.Big, returnErr error) {
	startTime := time.Now()
	defer recordMetricsWithError("eth_getBalance", a.connectionType, startTime, returnErr)
	height, err := a.watermarks.ResolveHeight(ctx, blockNrOrHash)
	if err != nil {
		return nil, err
	}
	sdkCtx := a.ctxProvider(height)
	if err := CheckVersion(sdkCtx, a.keeper); err != nil {
		return nil, err
	}
	statedb := state.NewDBImpl(sdkCtx, a.keeper, true)
	return (*hexutil.Big)(statedb.GetBalance(address).ToBig()), nil
}

func (a *StateAPI) GetCode(ctx context.Context, address common.Address, blockNrOrHash rpc.BlockNumberOrHash) (result hexutil.Bytes, returnErr error) {
	startTime := time.Now()
	defer recordMetricsWithError("eth_getCode", a.connectionType, startTime, returnErr)
	height, err := a.watermarks.ResolveHeight(ctx, blockNrOrHash)
	if err != nil {
		return nil, err
	}
	sdkCtx := a.ctxProvider(height)
	if err := CheckVersion(sdkCtx, a.keeper); err != nil {
		return nil, err
	}
	code := a.keeper.GetCode(sdkCtx, address)
	return code, nil
}

func (a *StateAPI) GetStorageAt(ctx context.Context, address common.Address, hexKey string, blockNrOrHash rpc.BlockNumberOrHash) (result hexutil.Bytes, returnErr error) {
	startTime := time.Now()
	defer recordMetricsWithError("eth_getStorageAt", a.connectionType, startTime, returnErr)
	height, err := a.watermarks.ResolveHeight(ctx, blockNrOrHash)
	if err != nil {
		return nil, err
	}
	sdkCtx := a.ctxProvider(height)
	if err := CheckVersion(sdkCtx, a.keeper); err != nil {
		return nil, err
	}
	key, _, err := decodeHash(hexKey)
	if err != nil {
		return nil, fmt.Errorf("unable to decode storage key: %s", err)
	}
	state := a.keeper.GetState(sdkCtx, address, key)
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

func (a *StateAPI) GetProof(ctx context.Context, address common.Address, storageKeys []string, blockNrOrHash rpc.BlockNumberOrHash) (result *ProofResult, returnErr error) {
	startTime := time.Now()
	defer recordMetricsWithError("eth_getProof", a.connectionType, startTime, returnErr)
	var block *coretypes.ResultBlock
	var err error
	if blockNr, ok := blockNrOrHash.Number(); ok {
		blockNumber, blockNumErr := getBlockNumber(ctx, a.tmClient, blockNr)
		if blockNumErr != nil {
			return nil, blockNumErr
		}
		block, err = blockByNumberRespectingWatermarks(ctx, a.tmClient, a.watermarks, blockNumber, 1)
	} else {
		block, err = blockByHashRespectingWatermarks(ctx, a.tmClient, a.watermarks, blockNrOrHash.BlockHash[:], 1)
	}
	if err != nil {
		return nil, err
	}
	sdkCtx := a.ctxProvider(block.Block.Height)
	if err := CheckVersion(sdkCtx, a.keeper); err != nil {
		return nil, err
	}
	queryStore, err := findQueryableKVStore(sdkCtx.MultiStore().GetKVStore(a.keeper.GetStoreKey()))
	if err != nil {
		return nil, err
	}
	proofResult := ProofResult{Address: address}
	for _, key := range storageKeys {
		paddedKey := common.BytesToHash([]byte(key))
		formattedKey := append(types.StateKey(address), paddedKey[:]...)
		qres := queryStore.Query(abci.RequestQuery{
			Path:   "/key",
			Data:   formattedKey,
			Height: block.Block.Height,
			Prove:  true,
		})
		proofResult.HexValues = append(proofResult.HexValues, hex.EncodeToString(qres.Value))
		proofResult.StorageProof = append(proofResult.StorageProof, qres.ProofOps)
	}

	return &proofResult, nil
}

// findQueryableKVStore unwraps known KVStore wrappers until it reaches a types.Queryable
// (classic IAVL, store/v2 memiavl commitment, or future proof-capable roots).
// Go only allows `x := s.(type)` inside a type switch, not before it. Nil parents are
// handled by the `s == nil` check on the next iteration; nil *Store receivers are
// guarded in each pointer case so we never call methods on nil.
func findQueryableKVStore(s sdk.KVStore) (storetypes.Queryable, error) {
	const maxDepth = 64
	for range maxDepth {
		if s == nil {
			return nil, errNoProofCapableQueryableKVStore
		}
		switch cast := s.(type) {
		case *cachekv.Store:
			if cast == nil {
				return nil, errNoProofCapableQueryableKVStore
			}
			s = cast.GetParent()
			continue
		case *gigacachekv.Store:
			if cast == nil {
				return nil, errNoProofCapableQueryableKVStore
			}
			s = cast.GetParent()
			continue
		case *tracekv.Store:
			if cast == nil {
				return nil, errNoProofCapableQueryableKVStore
			}
			s = cast.Parent()
			continue
		case prefix.Store:
			s = cast.Parent()
			continue
		case *prefix.Store:
			if cast == nil {
				return nil, errNoProofCapableQueryableKVStore
			}
			s = cast.Parent()
			continue
		}
		if q, ok := s.(storetypes.Queryable); ok {
			return q, nil
		}
		return nil, errNoProofCapableQueryableKVStore
	}
	return nil, fmt.Errorf("%w: exceeded unwrap depth", errNoProofCapableQueryableKVStore)
}

func (a *StateAPI) GetNonce(_ context.Context, address common.Address) uint64 {
	startTime := time.Now()
	defer recordMetrics("eth_getNonce", a.connectionType, startTime)
	return a.keeper.GetNonce(a.ctxProvider(LatestCtxHeight), address)
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
