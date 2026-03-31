package types_test

import (
	"testing"

	storev2rootmulti "github.com/sei-protocol/sei-chain/sei-cosmos/storev2/rootmulti"
	storetypes "github.com/sei-protocol/sei-chain/sei-cosmos/store/types"
	seidbconfig "github.com/sei-protocol/sei-chain/sei-db/config"
	"github.com/stretchr/testify/suite"
)

type MerkleTestSuite struct {
	suite.Suite

	store     *storev2rootmulti.Store
	storeKey  *storetypes.KVStoreKey
	iavlStore storetypes.KVStore
}

func (suite *MerkleTestSuite) SetupTest() {
	scConfig := seidbconfig.DefaultStateCommitConfig()
	scConfig.MemIAVLConfig.AsyncCommitBuffer = 0
	scConfig.MemIAVLConfig.SnapshotMinTimeInterval = 0
	ssConfig := seidbconfig.StateStoreConfig{}

	suite.store = storev2rootmulti.NewStore(suite.T().TempDir(), scConfig, ssConfig, nil)
	suite.storeKey = storetypes.NewKVStoreKey("iavlStoreKey")

	suite.store.MountStoreWithDB(suite.storeKey, storetypes.StoreTypeIAVL, nil)
	suite.store.LoadLatestVersion()

	suite.iavlStore = suite.store.GetCommitKVStore(suite.storeKey)
}

func TestMerkleTestSuite(t *testing.T) {
	suite.Run(t, new(MerkleTestSuite))
}
