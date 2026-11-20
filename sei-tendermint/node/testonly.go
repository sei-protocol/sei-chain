package node

import (
	"context"
	"errors"
	"fmt"

	gigaconfig "github.com/sei-protocol/sei-chain/giga/config"
	ablockstore "github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/blockstore"
	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/statesyncproof"
	atypes "github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/config"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/evmonlyapp"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/proxy"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/types"
)

// StartFullnodeForTesting runs the start of the fullnode that conf, giga and genDoc configure up
// to its giga router: the Giga storage, the application, and the router with its data state. It
// returns the BlockDB range and the app height once the router is built, and closes what it
// opened. Intended for use in tests only.
func StartFullnodeForTesting(
	ctx context.Context,
	conf *config.Config,
	giga gigaconfig.Config,
	genDoc *types.GenesisDoc,
) (_ utils.Option[ablockstore.SuffixRange], _ int64, retErr error) {
	none := utils.None[ablockstore.SuffixRange]()
	app, manager, err := makeEVMApplication(ctx, conf, giga, genDoc)
	if err != nil {
		return none, 0, err
	}
	defer func() { retErr = errors.Join(retErr, manager.Close()) }()
	_, err = buildGigaRouter(
		conf, types.GenNodeKey(), utils.None[atypes.SecretKey](), proxy.NewEVM(app), genDoc,
		manager.BlockStore(), manager.AppHashBuilder(), nil,
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

// EVMOnlyChainIDRowForTesting returns the module, key and value of the chain ID row that the
// EVM-only application commits with every block on chain chainID. Intended for use in tests only.
func EVMOnlyChainIDRowForTesting(chainID uint64) (string, []byte, []byte) {
	return evmonlyapp.ChainIDRowForTesting(chainID)
}

// StateSyncProofForTesting returns a state sync proof file of road height whose AppQC signs
// appHash, signed by keys, the validators of the genesis epoch of genDoc with unit weight.
// Intended for use in tests only.
func StateSyncProofForTesting(genDoc *types.GenesisDoc, keys []atypes.SecretKey, height int64, appHash []byte) ([]byte, error) {
	firstRoad, err := genesisFirstRoad(genDoc)
	if err != nil {
		return nil, err
	}
	weights := make(map[atypes.PublicKey]uint64, len(keys))
	for _, k := range keys {
		weights[k.Public()] = 1
	}
	registry, err := atypes.GenesisRegistry(weights, firstRoad, genesisEpochLength(genDoc))
	if err != nil {
		return nil, err
	}
	road, ok := utils.SafeCast[atypes.RoadHeight](height)
	if !ok {
		return nil, fmt.Errorf("height %d is not a road", height)
	}
	epochs, ok := registry.ForAnchor(road)
	if !ok {
		return nil, fmt.Errorf("road %d is not in a genesis epoch with a successor", road)
	}
	ep := epochs[0]
	proposal, blocks := atypes.ProposalAtBlocks(ep, atypes.View{Height: road}, 1)
	commitVote := atypes.NewCommitVote(atypes.BuildPrepareQC(keys, proposal))
	commitVotes := make([]*atypes.Signed[*atypes.CommitVote], 0, len(keys))
	for _, k := range keys {
		commitVotes = append(commitVotes, atypes.Sign(k, commitVote))
	}
	commitQC := atypes.NewCommitQC(commitVotes)
	appVote := atypes.NewAppVote(atypes.NewAppProposal(commitQC.View(), appHash))
	appVotes := make([]*atypes.Signed[*atypes.AppVote], 0, len(keys))
	for _, k := range keys {
		appVotes = append(appVotes, atypes.Sign(k, appVote))
	}
	roadBlock := atypes.NewRoadBlock(atypes.NewFullCommitQC(commitQC, []*atypes.LaneBlockHeader{blocks[0].Header()}), blocks)
	return statesyncproof.Encode(roadBlock, atypes.NewAppQC(appVotes))
}
