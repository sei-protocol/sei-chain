package keeper_test

import (
	"testing"
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"
	capabilitykeeper "github.com/cosmos/cosmos-sdk/x/capability/keeper"
	"github.com/stretchr/testify/suite"

	stakingkeeper "github.com/cosmos/cosmos-sdk/x/staking/keeper"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	upgradekeeper "github.com/cosmos/cosmos-sdk/x/upgrade/keeper"
	clienttypes "github.com/cosmos/ibc-go/v3/modules/core/02-client/types"
	ibchost "github.com/cosmos/ibc-go/v3/modules/core/24-host"
	ibckeeper "github.com/cosmos/ibc-go/v3/modules/core/keeper"
	ibctesting "github.com/cosmos/ibc-go/v3/testing"
)

type KeeperTestSuite struct {
	suite.Suite

	coordinator *ibctesting.Coordinator

	chainA *ibctesting.TestChain
	chainB *ibctesting.TestChain
}

func (suite *KeeperTestSuite) SetupTest() {
	suite.coordinator = ibctesting.NewCoordinator(suite.T(), 2)

	suite.chainA = suite.coordinator.GetChain(ibctesting.GetChainID(1))
	suite.chainB = suite.coordinator.GetChain(ibctesting.GetChainID(2))

	// TODO: remove
	// commit some blocks so that QueryProof returns valid proof (cannot return valid query if height <= 1)
	suite.coordinator.CommitNBlocks(suite.chainA, 2)
	suite.coordinator.CommitNBlocks(suite.chainB, 2)
}

func TestKeeperTestSuite(t *testing.T) {
	suite.Run(t, new(KeeperTestSuite))
}

// MockStakingKeeper implements clienttypes.StakingKeeper used in ibckeeper.NewKeeper
type MockStakingKeeper struct {
	mockField string
}

func (d MockStakingKeeper) GetHistoricalInfo(ctx sdk.Context, height int64) (stakingtypes.HistoricalInfo, bool) {
	return stakingtypes.HistoricalInfo{}, true
}

func (d MockStakingKeeper) UnbondingTime(ctx sdk.Context) time.Duration {
	return 0
}

// Test ibckeeper.NewKeeper used to initialize IBCKeeper when creating an app instance.
// It verifies if ibckeeper.NewKeeper panic when any of the keepers passed in is empty.
func (suite *KeeperTestSuite) TestNewKeeper() {
	var (
		stakingKeeper clienttypes.StakingKeeper
		upgradeKeeper clienttypes.UpgradeKeeper
		scopedKeeper  capabilitykeeper.ScopedKeeper
		newIBCKeeper  = func() {
			ibckeeper.NewKeeper(
				suite.chainA.GetSimApp().AppCodec(),
				suite.chainA.GetSimApp().GetKey(ibchost.StoreKey),
				suite.chainA.GetSimApp().GetSubspace(ibchost.ModuleName),
				stakingKeeper,
				upgradeKeeper,
				scopedKeeper,
			)
		}
	)

	testCases := []struct {
		name     string
		malleate func()
		expPass  bool
	}{
		{"failure: empty staking keeper", func() {
			emptyStakingKeeper := stakingkeeper.Keeper{}

			stakingKeeper = emptyStakingKeeper
		}, false},
		{"failure: empty mock staking keeper", func() {
			// use a different implementation of clienttypes.StakingKeeper
			emptyMockStakingKeeper := MockStakingKeeper{}

			stakingKeeper = emptyMockStakingKeeper
		}, false},
		{"failure: empty upgrade keeper", func() {
			emptyUpgradeKeeper := upgradekeeper.Keeper{}

			upgradeKeeper = emptyUpgradeKeeper
		}, false},
		{"failure: empty scoped keeper", func() {
			emptyScopedKeeper := capabilitykeeper.ScopedKeeper{}

			scopedKeeper = emptyScopedKeeper
		}, false},
		{"success: replace stakingKeeper with non-empty MockStakingKeeper", func() {
			// use a different implementation of clienttypes.StakingKeeper
			mockStakingKeeper := MockStakingKeeper{"not empty"}

			stakingKeeper = mockStakingKeeper
		}, true},
	}

	for _, tc := range testCases {
		tc := tc
		suite.SetupTest()

		suite.Run(tc.name, func() {
			stakingKeeper = suite.chainA.GetSimApp().StakingKeeper
			upgradeKeeper = suite.chainA.GetSimApp().UpgradeKeeper
			scopedKeeper = suite.chainA.GetSimApp().ScopedIBCKeeper

			tc.malleate()

			if tc.expPass {
				suite.Require().NotPanics(
					newIBCKeeper,
				)
			} else {
				suite.Require().Panics(
					newIBCKeeper,
				)
			}
		})
	}
}
