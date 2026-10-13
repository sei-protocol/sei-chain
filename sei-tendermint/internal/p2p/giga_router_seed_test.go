package p2p

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/block/littblock"
	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/blockstore"
	atypes "github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/proxy"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/tcp"
	"github.com/sei-protocol/sei-chain/sei-tendermint/types"
)

func newSeedTestConfig(t *testing.T) (*GigaRouterCommonConfig, atypes.BlockStore) {
	rng := utils.TestRng()
	_, validatorKeys := atypes.GenCommittee(rng, 3)
	addrs := map[atypes.PublicKey]GigaNodeAddr{}
	for _, validatorKey := range validatorKeys {
		addrs[validatorKey.Public()] = GigaNodeAddr{
			Key:      makeKey(rng).Public(),
			HostPort: tcp.HostPort{Hostname: "127.0.0.1", Port: 1},
		}
	}
	genDoc := &types.GenesisDoc{
		ChainID:         "giga-router-seed-test",
		InitialHeight:   1,
		AppState:        testAppStateJSON(rng),
		ConsensusParams: types.DefaultConsensusParams(),
	}
	require.NoError(t, genDoc.ValidateAndComplete())
	dir := t.TempDir()
	littCfg, err := littblock.DefaultConfig(filepath.Join(dir, "blockdb"))
	require.NoError(t, err)
	db, err := littblock.NewBlockDB(littCfg)
	require.NoError(t, err)
	blockStore, err := blockstore.New(db)
	require.NoError(t, err)
	t.Cleanup(func() { _ = blockStore.Close() })
	return &GigaRouterCommonConfig{
		DialInterval:       time.Second,
		ValidatorAddrs:     addrs,
		PersistentStateDir: dir,
		App:                proxy.New(newTestApp()),
		GenDoc:             genDoc,
	}, blockStore
}

func TestBuildFullnodeDataStateWithoutMarkerDoesNotSeed(t *testing.T) {
	cfg, blockStore := newSeedTestConfig(t)
	_, err := BuildFullnodeDataState(t.Context(), cfg, makeKey(utils.TestRng()), blockStore)
	require.NoError(t, err)
	require.False(t, blockStore.Status().IsPresent())
}

func TestBuildFullnodeDataStateMarkerBelowInitialHeightFails(t *testing.T) {
	cfg, blockStore := newSeedTestConfig(t)
	marker := filepath.Join(cfg.PersistentStateDir, StateSyncRestoredMarker)
	require.NoError(t, os.WriteFile(marker, nil, 0o600))
	_, err := BuildFullnodeDataState(t.Context(), cfg, makeKey(utils.TestRng()), blockStore)
	require.Error(t, err)
	require.False(t, blockStore.Status().IsPresent())
	_, err = os.Stat(marker)
	require.NoError(t, err, "a failed seed keeps the marker")
}
