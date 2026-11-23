package node

import (
	"errors"
	"fmt"

	"github.com/sei-protocol/sei-chain/giga/apphash"
	"github.com/sei-protocol/sei-chain/giga/apphash/vault"
	gigaconfig "github.com/sei-protocol/sei-chain/giga/config"
	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/statesyncproof"
	atypes "github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/config"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/evmonlyapp"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/types"
)

// RestoreFixture is the FlatKV row of an epoch registry and a state sync proof file that verifies
// against that registry. Intended for use in tests only.
type RestoreFixture struct {
	// Module, Key and Value are the FlatKV row of the epoch registry.
	Module string
	Key    []byte
	Value  []byte
	// Proof is the state sync proof file of the road of the fixture.
	Proof []byte
	// AppHash is the app hash that the AppQC of Proof signs.
	AppHash [32]byte
}

// GenRestoreFixture returns a RestoreFixture of road height. Its registry holds the genesis epochs
// of a committee that rng generates, with epoch 0 starting at height. Intended for use in tests only.
func GenRestoreFixture(rng utils.Rng, height int64) (RestoreFixture, error) {
	keys := rng.GenSliceN(4, atypes.GenSecretKey)
	weights := make(map[atypes.PublicKey]uint64, len(keys))
	for _, k := range keys {
		weights[k.Public()] = 1
	}
	registry, err := atypes.GenesisRegistry(weights, atypes.RoadHeight(height), 10) //nolint:gosec // test heights are positive.
	if err != nil {
		return RestoreFixture{}, err
	}
	appHash := [32]byte(utils.GenBytes(rng, 32))
	proof, err := statesyncproof.EncodeForEpochForTesting(rng, registry.Epochs()[0], keys, appHash[:])
	if err != nil {
		return RestoreFixture{}, err
	}
	module, key, value := evmonlyapp.EpochRegistryRowForTesting(registry)
	return RestoreFixture{Module: module, Key: key, Value: value, Proof: proof, AppHash: appHash}, nil
}

// ReadNewestAppHashForTesting returns the newest record of the app-hash vault of layout on a chain
// whose genesis is genDoc. Intended for use in tests only.
func ReadNewestAppHashForTesting(layout AutobahnLayout, genDoc *types.GenesisDoc) (_ *apphash.AppHashData, retErr error) {
	storageConfig, err := buildGigaStorageConfig(layout.PersistentStateDir, config.ModeFull, gigaconfig.StorageConfig{}, genDoc)
	if err != nil {
		return nil, err
	}
	cfg := *storageConfig.AppHashBuilderConfig
	cfg.HashVaultConfig.Path = layout.AppHashVaultDir
	hashVault, err := vault.NewStandardHashVault(cfg.HashVaultConfig, cfg.ChainID, cfg.GigaActivationHeight)
	if err != nil {
		return nil, err
	}
	defer func() { retErr = errors.Join(retErr, hashVault.Close()) }()
	_, newest, err := hashVault.Bounds()
	if err != nil {
		return nil, err
	}
	it, err := hashVault.Iterator(newest, newest)
	if err != nil {
		return nil, err
	}
	defer func() { retErr = errors.Join(retErr, it.Close()) }()
	ok, err := it.Next()
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("the app-hash vault holds no record of %d", newest)
	}
	return it.Entry(), nil
}
