package node

import (
	"context"
	"errors"

	gigaconfig "github.com/sei-protocol/sei-chain/giga/config"
	ablockstore "github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/blockstore"
	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/statesyncproof"
	atypes "github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/config"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/evmonlyapp"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/p2p"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/proxy"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/types"
)

// StartFullnodeForTesting runs the start of the fullnode that conf, giga and genDoc configure up
// to its giga router: the state sync start check, the Giga storage and the application, and the
// router with its data state. It returns the BlockDB range and the app height once the router is
// built, and closes what it opened. Intended for use in tests only.
func StartFullnodeForTesting(
	ctx context.Context,
	conf *config.Config,
	giga gigaconfig.Config,
	genDoc *types.GenesisDoc,
) (_ utils.Option[ablockstore.SuffixRange], _ int64, retErr error) {
	none := utils.None[ablockstore.SuffixRange]()
	start, err := readStateSyncStart(conf, giga, genDoc)
	if err != nil {
		return none, 0, err
	}
	app, manager, err := makeEVMApplication(ctx, conf, giga, genDoc)
	if err != nil {
		return none, 0, err
	}
	defer func() { retErr = errors.Join(retErr, manager.Close()) }()
	_, _, err = buildGigaRouter(
		ctx, conf, types.GenNodeKey(), utils.None[atypes.SecretKey](), proxy.NewEVM(app), genDoc,
		manager.BlockStore(), manager.AppHashBuilder(), nil, start,
	)
	if err != nil {
		return none, 0, err
	}
	return manager.BlockStore().Status(), app.LastBlockHeight(), nil
}

// EVMOnlyCursorRowForTesting returns the module, key and value of the cursor row that the
// EVM-only application commits with block height. Intended for use in tests only.
func EVMOnlyCursorRowForTesting(height int64) (string, []byte, []byte) {
	return evmonlyapp.CursorRowForTesting(height)
}

// GenesisEpochRegistryRowForTesting returns the epoch_registry row of a chain whose epoch
// registry holds only the genesis epochs of genesis. It writes registry files under scratchDir.
// Intended for use in tests only.
func GenesisEpochRegistryRowForTesting(genesis EpochGenesis, scratchDir string) ([]byte, error) {
	registry, err := p2p.OpenEpochRegistry(genesis.GenDoc, genesis.Validators, scratchDir)
	if err != nil {
		return nil, err
	}
	return registry.MarshalSnapshot(), nil
}

// StateSyncProofForTesting returns the state sync proof file of road height whose AppQC signs
// appHash, signed by keys, the keys of the genesis validators. It verifies against the epoch
// registry that holds only the genesis epochs of genesis, which it opens under scratchDir.
// Intended for use in tests only.
func StateSyncProofForTesting(
	genesis EpochGenesis,
	keys []atypes.SecretKey,
	height int64,
	appHash atypes.AppHash,
	scratchDir string,
) ([]byte, error) {
	registry, err := p2p.OpenEpochRegistry(genesis.GenDoc, genesis.Validators, scratchDir)
	if err != nil {
		return nil, err
	}
	road := atypes.RoadIndex(height) //nolint:gosec // test heights are positive.
	ep, err := registry.EpochAt(road)
	if err != nil {
		return nil, err
	}
	return statesyncproof.EncodeForTesting(ep, keys, road, appHash)
}
