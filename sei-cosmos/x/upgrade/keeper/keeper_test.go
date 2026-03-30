package keeper_test

import (
	"encoding/binary"
	"path/filepath"
	"testing"
	"time"

	tmproto "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"
	"github.com/stretchr/testify/suite"

	seiapp "github.com/sei-protocol/sei-chain/app"
	"github.com/sei-protocol/sei-chain/sei-cosmos/store/prefix"
	store "github.com/sei-protocol/sei-chain/sei-cosmos/store/types"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/sei-protocol/sei-chain/sei-cosmos/types/module"
	"github.com/sei-protocol/sei-chain/sei-cosmos/x/upgrade/keeper"
	"github.com/sei-protocol/sei-chain/sei-cosmos/x/upgrade/types"
)

type KeeperTestSuite struct {
	suite.Suite

	homeDir string
	app     *seiapp.App
	ctx     sdk.Context
}

func (s *KeeperTestSuite) SetupTest() {
	app := seiapp.Setup(s.T(), false, false, false)
	homeDir := filepath.Join(s.T().TempDir(), "x_upgrade_keeper_test")
	app.UpgradeKeeper = keeper.NewKeeper( // recreate keeper in order to use a custom home path
		make(map[int64]bool), app.GetKey(types.StoreKey), app.AppCodec(), homeDir, app.BaseApp,
	)
	s.T().Log("home dir:", homeDir)
	s.homeDir = homeDir
	s.app = app
	s.ctx = app.BaseApp.NewContext(false, tmproto.Header{
		Time:   time.Now(),
		Height: 10,
	})
}

func (s *KeeperTestSuite) TestReadUpgradeInfoFromDisk() {
	// require no error when the upgrade info file does not exist
	_, err := s.app.UpgradeKeeper.ReadUpgradeInfoFromDisk()
	s.Require().NoError(err)

	expected := store.UpgradeInfo{
		Name:   "test_upgrade",
		Height: 100,
	}

	// create an upgrade info file
	s.Require().NoError(s.app.UpgradeKeeper.DumpUpgradeInfoToDisk(expected.Height, expected.Name))

	ui, err := s.app.UpgradeKeeper.ReadUpgradeInfoFromDisk()
	s.Require().NoError(err)
	s.Require().Equal(expected, ui)
}

func (s *KeeperTestSuite) TestScheduleUpgrade() {
	cases := []struct {
		name    string
		plan    types.Plan
		setup   func()
		expPass bool
	}{
		{
			name: "successful height schedule",
			plan: types.Plan{
				Name:   "all-good",
				Info:   "some text here",
				Height: 123450000,
			},
			setup:   func() {},
			expPass: true,
		},
		{
			name: "successful overwrite",
			plan: types.Plan{
				Name:   "all-good",
				Info:   "some text here",
				Height: 123450000,
			},
			setup: func() {
				s.app.UpgradeKeeper.ScheduleUpgrade(s.ctx, types.Plan{
					Name:   "alt-good",
					Info:   "new text here",
					Height: 543210000,
				})
			},
			expPass: true,
		},
		{
			name: "unsuccessful schedule: invalid plan",
			plan: types.Plan{
				Height: 123450000,
			},
			setup:   func() {},
			expPass: false,
		},
		{
			name: "unsuccessful height schedule: due date in past",
			plan: types.Plan{
				Name:   "all-good",
				Info:   "some text here",
				Height: 1,
			},
			setup:   func() {},
			expPass: false,
		},
		{
			name: "unsuccessful schedule: schedule already executed",
			plan: types.Plan{
				Name:   "all-good",
				Info:   "some text here",
				Height: 123450000,
			},
			setup: func() {
				s.app.UpgradeKeeper.SetUpgradeHandler("all-good", func(ctx sdk.Context, plan types.Plan, vm module.VersionMap) (module.VersionMap, error) {
					return vm, nil
				})
				s.app.UpgradeKeeper.ApplyUpgrade(s.ctx, types.Plan{
					Name:   "all-good",
					Info:   "some text here",
					Height: 123450000,
				})
			},
			expPass: false,
		},
	}

	for _, tc := range cases {
		tc := tc

		s.Run(tc.name, func() {
			// reset suite
			s.SetupTest()

			// setup test case
			tc.setup()

			err := s.app.UpgradeKeeper.ScheduleUpgrade(s.ctx, tc.plan)

			if tc.expPass {
				s.Require().NoError(err, "valid test case failed")
			} else {
				s.Require().Error(err, "invalid test case passed")
			}
		})
	}
}

func (s *KeeperTestSuite) TestSetUpgradedClient() {
	cs := []byte("IBC client state")

	cases := []struct {
		name   string
		height int64
		setup  func()
		exists bool
	}{
		{
			name:   "no upgraded client exists",
			height: 10,
			setup:  func() {},
			exists: false,
		},
		{
			name:   "success",
			height: 10,
			setup: func() {
				s.app.UpgradeKeeper.SetUpgradedClient(s.ctx, 10, cs)
			},
			exists: true,
		},
	}

	for _, tc := range cases {
		// reset suite
		s.SetupTest()

		// setup test case
		tc.setup()

		gotCs, exists := s.app.UpgradeKeeper.GetUpgradedClient(s.ctx, tc.height)
		if tc.exists {
			s.Require().Equal(cs, gotCs, "valid case: %s did not retrieve correct client state", tc.name)
			s.Require().True(exists, "valid case: %s did not retrieve client state", tc.name)
		} else {
			s.Require().Nil(gotCs, "invalid case: %s retrieved valid client state", tc.name)
			s.Require().False(exists, "invalid case: %s retrieved valid client state", tc.name)
		}
	}

}

// Test that the protocol version successfully increments after an
// upgrade and is successfully set on BaseApp's appVersion.
func (s *KeeperTestSuite) TestIncrementProtocolVersion() {
	oldProtocolVersion := s.app.BaseApp.AppVersion()
	s.app.UpgradeKeeper.SetUpgradeHandler("dummy", func(_ sdk.Context, _ types.Plan, vm module.VersionMap) (module.VersionMap, error) { return vm, nil })
	dummyPlan := types.Plan{
		Name:   "dummy",
		Info:   "some text here",
		Height: 100,
	}
	s.app.UpgradeKeeper.ApplyUpgrade(s.ctx, dummyPlan)
	upgradedProtocolVersion := s.app.BaseApp.AppVersion()

	s.Require().Equal(oldProtocolVersion+1, upgradedProtocolVersion)
}

// Tests that the underlying state of x/upgrade is set correctly after
// an upgrade.
func (s *KeeperTestSuite) TestMigrations() {
	initialVM := module.VersionMap{"bank": uint64(1)}
	s.app.UpgradeKeeper.SetModuleVersionMap(s.ctx, initialVM)
	vmBefore := s.app.UpgradeKeeper.GetModuleVersionMap(s.ctx)
	s.app.UpgradeKeeper.SetUpgradeHandler("dummy", func(_ sdk.Context, _ types.Plan, vm module.VersionMap) (module.VersionMap, error) {
		// simulate upgrading the bank module
		vm["bank"] = vm["bank"] + 1
		return vm, nil
	})
	dummyPlan := types.Plan{
		Name:   "dummy",
		Info:   "some text here",
		Height: 123450000,
	}

	s.app.UpgradeKeeper.ApplyUpgrade(s.ctx, dummyPlan)
	vm := s.app.UpgradeKeeper.GetModuleVersionMap(s.ctx)
	s.Require().Equal(vmBefore["bank"]+1, vm["bank"])
}

func (s *KeeperTestSuite) TestLastCompletedUpgrade() {
	keeper := s.app.UpgradeKeeper
	require := s.Require()

	s.T().Log("verify empty name if applied upgrades are empty")
	name, height := keeper.GetLastCompletedUpgrade(s.ctx)
	require.Equal("", name)
	require.Equal(int64(0), height)

	keeper.SetUpgradeHandler("test-v0.9", func(_ sdk.Context, _ types.Plan, vm module.VersionMap) (module.VersionMap, error) {
		return vm, nil
	})

	keeper.ApplyUpgrade(s.ctx, types.Plan{
		Name:   "test-v0.9",
		Height: 10,
	})

	s.T().Log("verify valid upgrade name and height")
	name, height = keeper.GetLastCompletedUpgrade(s.ctx)
	require.Equal("test-v0.9", name)
	require.Equal(int64(10), height)

	keeper.SetUpgradeHandler("test-v0.10", func(_ sdk.Context, _ types.Plan, vm module.VersionMap) (module.VersionMap, error) {
		return vm, nil
	})

	newCtx := s.ctx.WithBlockHeight(15)
	keeper.ApplyUpgrade(newCtx, types.Plan{
		Name:   "test-v0.10",
		Height: 15,
	})

	s.T().Log("verify valid upgrade name and height with multiple upgrades")
	name, height = keeper.GetLastCompletedUpgrade(newCtx)
	require.Equal("test-v0.10", name)
	require.Equal(int64(15), height)
}

func (s *KeeperTestSuite) TestGetClosestUpgrade() {
	// Set up some upgrades
	upgrades := []struct {
		name   string
		height int64
	}{
		{"upgrade1", 15},
		{"upgrade2", 20},
		{"upgrade3", 25},
	}

	for _, u := range upgrades {
		s.app.UpgradeKeeper.SetDone(s.ctx, u.name)
		store := prefix.NewStore(s.ctx.KVStore(s.app.GetKey(types.StoreKey)), []byte{types.DoneByte})
		bz := make([]byte, 8)
		binary.BigEndian.PutUint64(bz, uint64(u.height))
		store.Set([]byte(u.name), bz)
	}

	tests := []struct {
		name      string
		height    int64
		expName   string
		expHeight int64
	}{
		{"closest to 18", 18, "upgrade2", 20},
		{"closest to 22", 22, "upgrade3", 25},
		{"closest to 10", 10, "upgrade1", 15},
	}

	for _, tt := range tests {
		s.Run(tt.name, func() {
			name, height := s.app.UpgradeKeeper.GetClosestUpgrade(s.ctx, tt.height)
			s.Require().Equal(tt.expName, name)
			s.Require().Equal(tt.expHeight, height)
		})
	}
}

// TestGetDoneHeight covers both the cache-hit and cache-miss paths of GetDoneHeight.
func (s *KeeperTestSuite) TestGetDoneHeight() {
	// Unknown upgrade returns 0 and must NOT be cached — SetDone may be called later.
	s.Require().Equal(int64(0), s.app.UpgradeKeeper.GetDoneHeight(s.ctx, "no-such-upgrade"))
	s.app.UpgradeKeeper.SetUpgradeHandler("no-such-upgrade", func(_ sdk.Context, _ types.Plan, vm module.VersionMap) (module.VersionMap, error) {
		return vm, nil
	})
	s.app.UpgradeKeeper.ApplyUpgrade(s.ctx, types.Plan{Name: "no-such-upgrade", Height: s.ctx.BlockHeight()})
	s.Require().NotEqual(int64(0), s.app.UpgradeKeeper.GetDoneHeight(s.ctx, "no-such-upgrade"))

	// SetDone writes to KV and populates the cache.
	s.app.UpgradeKeeper.SetUpgradeHandler("test-upgrade", func(_ sdk.Context, _ types.Plan, vm module.VersionMap) (module.VersionMap, error) {
		return vm, nil
	})
	s.app.UpgradeKeeper.ApplyUpgrade(s.ctx, types.Plan{Name: "test-upgrade", Height: s.ctx.BlockHeight()})

	// Cache hit: same keeper instance — the value was stored by SetDone.
	s.Require().Equal(s.ctx.BlockHeight(), s.app.UpgradeKeeper.GetDoneHeight(s.ctx, "test-upgrade"))

	// Cache miss: fresh keeper with empty cache but value present in the KV store.
	freshKeeper := keeper.NewKeeper(
		make(map[int64]bool),
		s.app.GetKey(types.StoreKey),
		s.app.AppCodec(),
		s.homeDir,
		s.app.BaseApp,
	)
	s.Require().Equal(s.ctx.BlockHeight(), freshKeeper.GetDoneHeight(s.ctx, "test-upgrade"))

	// Second call on fresh keeper should now hit the cache.
	s.Require().Equal(s.ctx.BlockHeight(), freshKeeper.GetDoneHeight(s.ctx, "test-upgrade"))
}

// TestGetDoneHeightHistoricalContext verifies that a cache entry populated by a
// recent-block call does not cause GetDoneHeight to return the wrong answer for
// a historical context that predates the upgrade.
func (s *KeeperTestSuite) TestGetDoneHeightHistoricalContext() {
	const upgradeName = "history-test-upgrade"
	upgradeHeight := int64(100)

	// Apply the upgrade at block 100 — this warms the cache with height 100.
	upgradeCtx := s.ctx.WithBlockHeight(upgradeHeight)
	s.app.UpgradeKeeper.SetUpgradeHandler(upgradeName, func(_ sdk.Context, _ types.Plan, vm module.VersionMap) (module.VersionMap, error) {
		return vm, nil
	})
	s.app.UpgradeKeeper.ApplyUpgrade(upgradeCtx, types.Plan{Name: upgradeName, Height: upgradeHeight})

	// Historical context before the upgrade: must return 0, not the cached 100.
	beforeCtx := s.ctx.WithBlockHeight(upgradeHeight - 1)
	s.Require().Equal(int64(0), s.app.UpgradeKeeper.GetDoneHeight(beforeCtx, upgradeName))

	// Context exactly at the upgrade height: must return 100.
	s.Require().Equal(upgradeHeight, s.app.UpgradeKeeper.GetDoneHeight(upgradeCtx, upgradeName))

	// Context after the upgrade: must return 100.
	afterCtx := s.ctx.WithBlockHeight(upgradeHeight + 50)
	s.Require().Equal(upgradeHeight, s.app.UpgradeKeeper.GetDoneHeight(afterCtx, upgradeName))
}

func TestKeeperTestSuite(t *testing.T) {
	suite.Run(t, new(KeeperTestSuite))
}
