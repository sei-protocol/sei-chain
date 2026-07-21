package statesync

import (
	"bytes"
	"context"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	tmcfg "github.com/sei-protocol/sei-chain/sei-tendermint/config"
	sm "github.com/sei-protocol/sei-chain/sei-tendermint/internal/state"
	internalstatesync "github.com/sei-protocol/sei-chain/sei-tendermint/internal/statesync"
	tmstore "github.com/sei-protocol/sei-chain/sei-tendermint/internal/store"
	"github.com/sei-protocol/sei-chain/sei-tendermint/light"
	lighthttp "github.com/sei-protocol/sei-chain/sei-tendermint/light/provider/http"
	rpchttp "github.com/sei-protocol/sei-chain/sei-tendermint/rpc/client/http"
	"github.com/sei-protocol/sei-chain/sei-tendermint/types"
)

// BootstrapFromRPC verifies the archived app hash with the same RPC light-client
// state provider used by Tendermint state sync, then writes the state and seen
// commit records needed for a node to start block syncing from height.
func BootstrapFromRPC(
	ctx context.Context,
	cfg *tmcfg.Config,
	chainID string,
	height int64,
	appHash []byte,
	rpcServers []string,
	trustHeight int64,
	trustHashHex string,
	trustPeriod time.Duration,
) error {
	if cfg == nil {
		return fmt.Errorf("config is required")
	}
	if chainID == "" {
		return fmt.Errorf("chain ID is required")
	}
	if height <= 0 {
		return fmt.Errorf("height must be positive, got %d", height)
	}
	if len(appHash) == 0 {
		return fmt.Errorf("app hash is required")
	}
	trustHash, err := hex.DecodeString(trustHashHex)
	if err != nil {
		return fmt.Errorf("decode trust hash: %w", err)
	}
	if trustPeriod <= 0 {
		trustPeriod = cfg.StateSync.TrustPeriod
	}
	if trustPeriod <= 0 {
		return fmt.Errorf("trust period must be positive")
	}
	verifyTimeout := cfg.StateSync.VerifyLightBlockTimeout
	if verifyTimeout <= 0 {
		verifyTimeout = 30 * time.Second
	}
	blacklistTTL := cfg.StateSync.BlacklistTTL
	if blacklistTTL <= 0 {
		blacklistTTL = time.Minute
	}

	provider, err := internalstatesync.NewRPCStateProvider(
		ctx,
		chainID,
		0,
		verifyTimeout,
		rpcServers,
		light.TrustOptions{
			Period: trustPeriod,
			Height: trustHeight,
			Hash:   trustHash,
		},
		blacklistTTL,
	)
	if err != nil {
		return fmt.Errorf("create RPC state provider: %w", err)
	}

	verifiedAppHash, err := provider.AppHash(ctx, uint64(height)) //nolint:gosec // height checked above
	if err != nil {
		return fmt.Errorf("verify app hash at height %d: %w", height, err)
	}
	if !bytes.Equal(verifiedAppHash, appHash) {
		return fmt.Errorf("archived app hash %X does not match light-client app hash %X at height %d",
			appHash, verifiedAppHash, height)
	}

	state, err := provider.State(ctx, uint64(height)) //nolint:gosec // height checked above
	if err != nil {
		return fmt.Errorf("fetch trusted state at height %d: %w", height, err)
	}
	commit, err := provider.Commit(ctx, uint64(height)) //nolint:gosec // height checked above
	if err != nil {
		return fmt.Errorf("fetch trusted commit at height %d: %w", height, err)
	}
	lightBlockProvider, err := lighthttp.New(state.ChainID, rpcServers[0])
	if err != nil {
		return fmt.Errorf("create light block provider: %w", err)
	}
	lightBlock, err := lightBlockProvider.LightBlock(ctx, state.LastBlockHeight)
	if err != nil {
		return fmt.Errorf("fetch trusted light block at height %d: %w", state.LastBlockHeight, err)
	}
	if !bytes.Equal(lightBlock.Hash(), state.LastBlockID.Hash) {
		return fmt.Errorf("trusted light block hash %X does not match state LastBlockID hash %X at height %d",
			lightBlock.Hash(), state.LastBlockID.Hash, state.LastBlockHeight)
	}
	rpcClient, err := rpchttp.New(withHTTPDefault(rpcServers[0]))
	if err != nil {
		return fmt.Errorf("create RPC block client: %w", err)
	}
	blockResult, err := rpcClient.Block(ctx, &state.LastBlockHeight)
	if err != nil {
		return fmt.Errorf("fetch trusted block at height %d: %w", state.LastBlockHeight, err)
	}
	if blockResult == nil || blockResult.Block == nil {
		return fmt.Errorf("RPC returned no block at height %d", state.LastBlockHeight)
	}
	if !bytes.Equal(blockResult.Block.Hash(), state.LastBlockID.Hash) {
		return fmt.Errorf("trusted block hash %X does not match state LastBlockID hash %X at height %d",
			blockResult.Block.Hash(), state.LastBlockID.Hash, state.LastBlockHeight)
	}
	blockParts, err := blockResult.Block.MakePartSet(types.BlockPartSizeBytes)
	if err != nil {
		return fmt.Errorf("make block part set: %w", err)
	}

	stateDB, err := tmcfg.DefaultDBProvider(&tmcfg.DBContext{ID: "state", Config: cfg})
	if err != nil {
		return fmt.Errorf("open state db: %w", err)
	}
	defer func() { _ = stateDB.Close() }()
	stateStore := sm.NewStore(stateDB)
	if err := stateStore.Bootstrap(state); err != nil {
		return fmt.Errorf("bootstrap state store: %w", err)
	}
	if err := stateStore.SaveFinalizeBlockResponses(state.LastBlockHeight, &abci.ResponseFinalizeBlock{
		AppHash: appHash,
	}); err != nil {
		return fmt.Errorf("save empty finalize block response: %w", err)
	}

	blockStoreDB, err := tmcfg.DefaultDBProvider(&tmcfg.DBContext{ID: "blockstore", Config: cfg})
	if err != nil {
		return fmt.Errorf("open blockstore db: %w", err)
	}
	defer func() { _ = blockStoreDB.Close() }()
	blockStore := tmstore.NewBlockStore(blockStoreDB)
	blockStore.SaveBlock(blockResult.Block, blockParts, commit)
	return nil
}

func withHTTPDefault(remote string) string {
	if strings.Contains(remote, "://") {
		return remote
	}
	return "http://" + remote
}
