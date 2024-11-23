package keeper_test

import (
	"testing"
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	epochTypes "github.com/sei-protocol/sei-chain/x/epoch/types"
	"github.com/sei-protocol/sei-chain/x/mint/keeper"
	mintKeeper "github.com/sei-protocol/sei-chain/x/mint/keeper"
	"github.com/sei-protocol/sei-chain/x/mint/types"
	mintTypes "github.com/sei-protocol/sei-chain/x/mint/types"

	minttypes "github.com/sei-protocol/sei-chain/x/mint/types"
	"github.com/stretchr/testify/require"
)

type MockAccountKeeper struct {
	ModuleAddress       sdk.AccAddress
	ModuleAccount       authtypes.ModuleAccountI
	moduleNameToAddress map[string]string
}

func (m MockAccountKeeper) GetModuleAddress(name string) sdk.AccAddress {
	if addrStr, ok := m.moduleNameToAddress[name]; ok {
		addr, _ := sdk.AccAddressFromBech32(addrStr)
		return addr
	}
	return nil
}

func (m MockAccountKeeper) SetModuleAccount(ctx sdk.Context, account authtypes.ModuleAccountI) {
	m.ModuleAccount = account
}

func (m MockAccountKeeper) GetModuleAccount(ctx sdk.Context, moduleName string) authtypes.ModuleAccountI {
	return m.ModuleAccount
}

func (m MockAccountKeeper) SetModuleAddress(name, address string) {
	m.moduleNameToAddress[name] = address
}

type MockMintHooks struct {
	afterDistributeMintedCoinCalled bool
}

func (h *MockMintHooks) AfterDistributeMintedCoin(ctx sdk.Context, mintedCoin sdk.Coin) {
	h.afterDistributeMintedCoinCalled = true
}

func TestGetNextScheduledTokenRelease(t *testing.T) {
	t.Parallel()

	currentTime := time.Now().UTC()
	epoch := epochTypes.Epoch{
		CurrentEpochStartTime: currentTime,
		CurrentEpochHeight:    100,
	}
	currentMinter := mintTypes.DefaultInitialMinter()

	tokenReleaseSchedule := []mintTypes.ScheduledTokenRelease{
		{
			StartDate:          currentTime.AddDate(0, 0, 30).Format(minttypes.TokenReleaseDateFormat),
			EndDate:            currentTime.AddDate(0, 2, 0).Format(minttypes.TokenReleaseDateFormat),
			TokenReleaseAmount: 200,
		},
		{
			StartDate:          currentTime.AddDate(1, 0, 0).Format(minttypes.TokenReleaseDateFormat),
			EndDate:            currentTime.AddDate(2, 0, 0).Format(minttypes.TokenReleaseDateFormat),
			TokenReleaseAmount: 300,
		},
		{
			StartDate:          currentTime.AddDate(0, 0, 1).Format(minttypes.TokenReleaseDateFormat),
			EndDate:            currentTime.AddDate(0, 0, 15).Format(minttypes.TokenReleaseDateFormat),
			TokenReleaseAmount: 100,
		},
	}

	t.Run("Get the next scheduled token release", func(t *testing.T) {
		// No next scheduled token release intially
		epoch.CurrentEpochStartTime = currentTime.AddDate(0, 0, 0)
		nextScheduledRelease := mintKeeper.GetNextScheduledTokenRelease(epoch, tokenReleaseSchedule, currentMinter)
		require.Nil(t, nextScheduledRelease)

		epoch.CurrentEpochStartTime = currentTime.AddDate(0, 0, 1)
		nextScheduledRelease = mintKeeper.GetNextScheduledTokenRelease(epoch, tokenReleaseSchedule, currentMinter)
		require.NotNil(t, nextScheduledRelease)
		require.Equal(t, uint64(100), nextScheduledRelease.TokenReleaseAmount)
	})

	t.Run("No next scheduled token release, assume we are on the second period", func(t *testing.T) {
		// No next scheduled token release intially
		epoch.CurrentEpochStartTime = currentTime.AddDate(0, 0, 0)
		nextScheduledRelease := mintKeeper.GetNextScheduledTokenRelease(epoch, tokenReleaseSchedule, currentMinter)
		require.Nil(t, nextScheduledRelease)

		secondMinter := mintTypes.NewMinter(
			currentTime.AddDate(0, 0, 30).Format(minttypes.TokenReleaseDateFormat),
			currentTime.AddDate(0, 2, 0).Format(minttypes.TokenReleaseDateFormat),
			"usei",
			200,
		)
		epoch.CurrentEpochStartTime = currentTime.AddDate(0, 5, 0)
		nextScheduledRelease = mintKeeper.GetNextScheduledTokenRelease(epoch, tokenReleaseSchedule, secondMinter)
		require.Nil(t, nextScheduledRelease)
	})

	t.Run("test case where we skip the start date due to outage for two days", func(t *testing.T) {
		// No next scheduled token release intially
		epoch.CurrentEpochStartTime = currentTime.AddDate(0, 0, 0)
		nextScheduledRelease := mintKeeper.GetNextScheduledTokenRelease(epoch, tokenReleaseSchedule, currentMinter)
		require.Nil(t, nextScheduledRelease)

		// First mint was +1 but the chain recoverd on +3
		epoch.CurrentEpochStartTime = currentTime.AddDate(0, 0, 3)
		nextScheduledRelease = mintKeeper.GetNextScheduledTokenRelease(epoch, tokenReleaseSchedule, currentMinter)
		require.Equal(t, uint64(100), nextScheduledRelease.GetTokenReleaseAmount())
		require.Equal(t, currentTime.AddDate(0, 0, 1).Format(minttypes.TokenReleaseDateFormat), nextScheduledRelease.GetStartDate())
	})

	t.Run("Panic on invalid format", func(t *testing.T) {
		// No next scheduled token release intially
		tokenReleaseSchedule := []mintTypes.ScheduledTokenRelease{
			{
				StartDate:          "Bad Start Date",
				EndDate:            currentTime.AddDate(0, 2, 0).Format(minttypes.TokenReleaseDateFormat),
				TokenReleaseAmount: 200,
			},
		}
		epoch.CurrentEpochStartTime = currentTime.AddDate(0, 0, 0)
		require.Panics(t, func() {
			mintKeeper.GetNextScheduledTokenRelease(epoch, tokenReleaseSchedule, currentMinter)
		})
	})
}

func TestGetOrUpdateLatestMinter(t *testing.T) {
	t.Parallel()
	app, ctx := createTestApp(false)
	mintKeeper := app.MintKeeper
	currentTime := time.Now()
	epoch := epochTypes.Epoch{
		CurrentEpochStartTime: currentTime,
	}

	t.Run("No ongoing release", func(t *testing.T) {
		currentMinter := mintKeeper.GetOrUpdateLatestMinter(ctx, epoch)
		require.False(t, currentMinter.OngoingRelease())
	})

	t.Run("No ongoing release, but there's a scheduled release", func(t *testing.T) {
		mintKeeper.SetMinter(ctx, mintTypes.NewMinter(
			currentTime.Format(minttypes.TokenReleaseDateFormat),
			currentTime.AddDate(1, 0, 0).Format(minttypes.TokenReleaseDateFormat),
			"usei",
			1000,
		))
		epoch.CurrentEpochStartTime = currentTime
		currentMinter := mintKeeper.GetOrUpdateLatestMinter(ctx, epoch)
		require.True(t, currentMinter.OngoingRelease())
		require.Equal(t, currentTime.Format(minttypes.TokenReleaseDateFormat), currentMinter.StartDate)
		mintKeeper.SetMinter(ctx, mintTypes.DefaultInitialMinter())
	})

	t.Run("Ongoing release same day", func(t *testing.T) {
		params := mintKeeper.GetParams(ctx)
		params.TokenReleaseSchedule = []types.ScheduledTokenRelease{
			{
				StartDate:          currentTime.AddDate(0, 0, 0).Format(minttypes.TokenReleaseDateFormat),
				EndDate:            currentTime.AddDate(0, 0, 0).Format(minttypes.TokenReleaseDateFormat),
				TokenReleaseAmount: 1000,
			},
		}
		mintKeeper.SetParams(ctx, params)

		minter := types.Minter{
			StartDate:           currentTime.Format(minttypes.TokenReleaseDateFormat),
			EndDate:             currentTime.Format(minttypes.TokenReleaseDateFormat),
			Denom:               "usei",
			TotalMintAmount:     100,
			RemainingMintAmount: 0,
			LastMintAmount:      100,
			LastMintDate:        "2023-04-01",
			LastMintHeight:      0,
		}
		mintKeeper.SetMinter(ctx, minter)

		epoch.CurrentEpochStartTime = currentTime
		currentMinter := mintKeeper.GetOrUpdateLatestMinter(ctx, epoch)
		amount := currentMinter.GetReleaseAmountToday(currentTime).IsZero()
		require.Zero(t, currentMinter.GetRemainingMintAmount())
		require.True(t, amount)
		require.False(t, currentMinter.OngoingRelease())
		require.Equal(t, currentTime.Format(minttypes.TokenReleaseDateFormat), currentMinter.StartDate)
		mintKeeper.SetMinter(ctx, mintTypes.DefaultInitialMinter())
	})

	t.Run("TokenReleaseSchedule not sorted", func(t *testing.T) {
		params := mintKeeper.GetParams(ctx)
		params.TokenReleaseSchedule = []types.ScheduledTokenRelease{
			{
				StartDate:          currentTime.AddDate(0, 20, 0).Format(minttypes.TokenReleaseDateFormat),
				EndDate:            currentTime.AddDate(0, 45, 0).Format(minttypes.TokenReleaseDateFormat),
				TokenReleaseAmount: 2000,
			},
			{
				StartDate:          currentTime.Format(minttypes.TokenReleaseDateFormat),
				EndDate:            currentTime.AddDate(0, 15, 0).Format(minttypes.TokenReleaseDateFormat),
				TokenReleaseAmount: 1000,
			},
		}
		mintKeeper.SetParams(ctx, params)

		epoch.CurrentEpochStartTime = currentTime
		currentMinter := mintKeeper.GetOrUpdateLatestMinter(ctx, epoch)
		require.True(t, currentMinter.OngoingRelease())
		require.Equal(t, currentTime.Format(minttypes.TokenReleaseDateFormat), currentMinter.StartDate)
	})
}

func TestBaseCases(t *testing.T) {
	t.Parallel()
	app, ctx := createTestApp(false)
	mintKeeper := app.MintKeeper

	t.Run("invalid module name", func(t *testing.T) {
		mockAccountKeeper := MockAccountKeeper{}

		require.Panics(t, func() {
			keeper.NewKeeper(
				mintKeeper.GetCdc(),
				mintKeeper.GetStoreKey(),
				mintKeeper.GetParamSpace(),
				nil,
				mockAccountKeeper,
				nil,
				nil,
				"invalid module",
			)
		})
	})

	t.Run("set hooks", func(t *testing.T) {
		newHook := &MockMintHooks{}
		mintKeeper.SetHooks(newHook)

		require.PanicsWithValue(t, "cannot set mint hooks twice", func() {
			mintKeeper.SetHooks(newHook)
		})
	})

	t.Run("nil minter", func(t *testing.T) {
		nilApp, nilCtx := createTestApp(false)

		store := nilCtx.KVStore(nilApp.MintKeeper.GetStoreKey())
		store.Delete(types.MinterKey)
		require.PanicsWithValue(t, "stored minter should not have been nil", func() {
			nilApp.MintKeeper.GetMinter(nilCtx)
		})
	})

	t.Run("staking keeper calls", func(t *testing.T) {
		require.False(t, mintKeeper.StakingTokenSupply(ctx).IsNil())
		require.False(t, mintKeeper.BondedRatio(ctx).IsNil())
	})

	t.Run("mint keeper calls", func(t *testing.T) {
		require.NotNil(t, mintKeeper.GetStoreKey())
		require.NotNil(t, mintKeeper.GetCdc())
		require.NotNil(t, mintKeeper.GetParamSpace())
		require.NotPanics(t, func() {
			mintKeeper.SetParamSpace(mintKeeper.GetParamSpace())
		})
	})

	t.Run("staking keeper calls", func(t *testing.T) {
		require.Nil(t, mintKeeper.MintCoins(ctx, sdk.NewCoins()))
	})

}
