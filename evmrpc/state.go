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
	"github.com/sei-protocol/sei-chain/sei-cosmos/client"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/crypto"
	"github.com/sei-protocol/sei-chain/x/evm/keeper"
	"github.com/sei-protocol/sei-chain/x/evm/state"
)

type StateAPI struct {
	tmClient       client.LocalClient
	keeper         *keeper.Keeper
	ctxProvider    func(int64) sdk.Context
	connectionType ConnectionType
	watermarks     *WatermarkManager
}

func NewStateAPI(tmClient client.LocalClient, k *keeper.Keeper, ctxProvider func(int64) sdk.Context, connectionType ConnectionType, watermarks *WatermarkManager) *StateAPI {
	return &StateAPI{tmClient: tmClient, keeper: k, ctxProvider: ctxProvider, connectionType: connectionType, watermarks: watermarks}
}

func (a *StateAPI) GetBalance(ctx context.Context, address common.Address, blockNrOrHash rpc.BlockNumberOrHash) (result *hexutil.Big, returnErr error) {
	startTime := time.Now()
	defer func() {
		recordMetricsWithError(ctx, "eth_getBalance", a.connectionType, startTime, returnErr, recover())
	}()
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
	defer func() {
		recordMetricsWithError(ctx, "eth_getCode", a.connectionType, startTime, returnErr, recover())
	}()
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
	defer func() {
		recordMetricsWithError(ctx, "eth_getStorageAt", a.connectionType, startTime, returnErr, recover())
	}()
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

// GetProof is registered but deliberately unimplemented, so callers get the
// documented -32000 rather than a -32601 "method not found".
func (a *StateAPI) GetProof(ctx context.Context, _ common.Address, _ []string, _ rpc.BlockNumberOrHash) (_ *ProofResult, returnErr error) {
	startTime := time.Now()
	defer func() {
		recordMetrics(ctx, "eth_getProof", a.connectionType, startTime)
	}()
	return nil, &ErrEVMNotSupported{Msg: "eth_getProof is not supported yet; please reach out to the Sei team if you need this endpoint"}
}

func (a *StateAPI) GetNonce(ctx context.Context, address common.Address) uint64 {
	startTime := time.Now()
	defer recordMetrics(ctx, "eth_getNonce", a.connectionType, startTime)
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
