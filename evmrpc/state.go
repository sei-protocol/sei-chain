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
	"github.com/sei-protocol/sei-chain/sei-cosmos/store/cachekv"
	iavlstore "github.com/sei-protocol/sei-chain/sei-cosmos/store/iavl"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/crypto"
	rpcclient "github.com/sei-protocol/sei-chain/sei-tendermint/rpc/client"
	"github.com/sei-protocol/sei-chain/sei-tendermint/rpc/coretypes"
	"github.com/sei-protocol/sei-chain/x/evm/keeper"
	"github.com/sei-protocol/sei-chain/x/evm/state"
	"github.com/sei-protocol/sei-chain/x/evm/types"
)

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
	var iavl *iavlstore.Store
	s := sdkCtx.MultiStore().GetKVStore((a.keeper.GetStoreKey()))
OUTER:
	for {
		switch cast := s.(type) {
		case *iavlstore.Store:
			iavl = cast
			break OUTER
		case *cachekv.Store:
			if cast.GetParent() == nil {
				return nil, errors.New("cannot find EVM IAVL store")
			}
			s = cast.GetParent()
		default:
			return nil, errors.New("cannot find EVM IAVL store")
		}
	}
	proofResult := ProofResult{Address: address}
	for _, key := range storageKeys {
		paddedKey := common.BytesToHash([]byte(key))
		formattedKey := append(types.StateKey(address), paddedKey[:]...)
		qres := iavl.Query(abci.RequestQuery{
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
