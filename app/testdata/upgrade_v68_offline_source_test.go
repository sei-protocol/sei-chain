//go:build upgrade_v68 && offline_upgrade && upgrade_source

package app

import (
	"context"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/gogo/protobuf/proto"
	"github.com/sei-protocol/sei-chain/sei-cosmos/crypto/keys/secp256k1"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	sdkerrors "github.com/sei-protocol/sei-chain/sei-cosmos/types/errors"
	authtypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/auth/types"
	vestingtypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/auth/vesting/types"
	upgradetypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/upgrade/types"
	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	tmproto "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"
	minttypes "github.com/sei-protocol/sei-chain/x/mint/types"
	"github.com/stretchr/testify/require"
)

const (
	// v68OfflineVestingEndTime is 3000-01-01, the end time of every vesting
	// account on pacific-1, so v6.7 locks each fixture's whole balance.
	v68OfflineVestingEndTime int64  = 32_503_680_000
	v68OfflineVestingAmount  int64  = 1_000_000_000
	v68OfflineAuthVersion    uint64 = 3
)

func TestV68OfflineUpgradeSource(t *testing.T) {
	root := requireOfflineUpgradePhase(t, "source")
	testApp := openOfflineUpgradeApp(t, root, true)
	ctx := testApp.GetContextForDeliverTx(nil).WithBlockTime(time.Now().UTC())

	vesting, retained := seedV68OfflineVestingAccounts(t, testApp, ctx)
	requireV68OfflineBalancesLocked(t, testApp, ctx, retained)
	stores := snapshotOfflineUpgradeStores(t, testApp, ctx, []string{authtypes.StoreKey})
	upgradeHeight := ctx.BlockHeight() + 2
	require.NoError(t, testApp.UpgradeKeeper.ScheduleUpgrade(ctx, upgradetypes.Plan{
		Name:   "v6.8",
		Height: upgradeHeight,
	}))

	commitOfflineUpgradeApp(t, testApp)
	sourceHeight := testApp.LastBlockHeight()
	plan, found := committedOfflineUpgradePlan(t, testApp)
	require.True(t, found, "scheduled upgrade plan was not committed")
	require.Equal(t, "v6.8", plan.Name)
	require.Equal(t, upgradeHeight, plan.Height)
	moduleVersions := offlineUpgradeModuleVersions(t, testApp)
	require.Contains(t, moduleVersions, "vesting", "v6.7 module version map does not contain vesting")
	versionMap := testApp.UpgradeKeeper.GetModuleVersionMap(offlineUpgradeReadContext(testApp, sourceHeight))
	require.Equal(t, v68OfflineAuthVersion, versionMap[authtypes.ModuleName])
	closeOfflineUpgradeApp(t, testApp)

	writeOfflineUpgradeArtifact(t, root, offlineUpgradeArtifact{
		Upgrade:         plan.Name,
		SourceHeight:    sourceHeight,
		UpgradeHeight:   upgradeHeight,
		ModuleVersions:  moduleVersions,
		Stores:          stores,
		Retained:        retained,
		VestingAccounts: vesting,
	})

	requireV68OfflineUnupgradedHalt(t, root, sourceHeight, upgradeHeight)
}

func TestV68OfflineUpgradeReopen(t *testing.T) {
	root := requireOfflineUpgradePhase(t, "reopen")
	artifact := readOfflineUpgradeArtifact(t, root)
	require.Equal(t, "v6.8", artifact.Upgrade)
	require.NotEmpty(t, artifact.UpgradeHash, "target phase did not record the post-upgrade application hash")

	migrated := offlineUpgradeMigratedDatabase(t, root, artifact)
	reopenRoot := filepath.Join(root, "reopen")
	copyOfflineUpgradeDatabase(t, migrated, reopenRoot)

	testApp := openOfflineUpgradeApp(t, reopenRoot, false)
	defer closeOfflineUpgradeApp(t, testApp)

	require.Equal(t, artifact.UpgradeHeight, testApp.LastBlockHeight(),
		"v6.7 opened the migrated database at a different height than v6.8 left it")
	require.NotContains(t, offlineUpgradeModuleVersions(t, testApp), "vesting",
		"v6.7 still sees a vesting version-map entry after v6.8 deleted it")

	ctx := offlineUpgradeReadContext(testApp, testApp.LastBlockHeight())
	for _, recorded := range artifact.VestingAccounts {
		address := sdk.MustAccAddressFromBech32(recorded.Address)
		account, ok := testApp.AccountKeeper.GetAccount(ctx, address).(*authtypes.BaseAccount)
		require.True(t, ok, "v6.7 does not read the %s v6.8 rewrote as a base account", recorded.TypeURL)
		require.Equal(t, recorded.AccountNumber, account.GetAccountNumber())
		require.Equal(t, recorded.Sequence, account.GetSequence())
		require.Equal(t, recorded.PubKey, hex.EncodeToString(account.GetPubKey().Bytes()))
		balance, err := sdk.ParseCoinsNormalized(recorded.Balance)
		require.NoError(t, err)
		require.Equal(t, balance, testApp.BankKeeper.SpendableCoins(ctx, address),
			"v6.7 still locks the balance of the %s v6.8 rewrote", recorded.TypeURL)
	}

	lastName, lastHeight := testApp.UpgradeKeeper.GetLastCompletedUpgrade(ctx)
	require.Equal(t, artifact.Upgrade, lastName)
	require.Equal(t, artifact.UpgradeHeight, lastHeight)
	require.False(t, testApp.UpgradeKeeper.HasHandler("v6.8"), "v6.7 registered a v6.8 upgrade handler")

	var panicked any
	func() {
		defer func() { panicked = recover() }()
		_, err := testApp.FinalizeBlock(context.Background(), &abci.RequestFinalizeBlock{
			Hash: []byte("offline-upgrade-reopen"),
			Header: &tmproto.Header{
				ChainID: offlineUpgradeChainID,
				Height:  artifact.UpgradeHeight + 1,
			},
		})
		require.NoError(t, err, "v6.7 returned from FinalizeBlock without panicking")
	}()
	require.NotNil(t, panicked, "v6.7 produced a block on the migrated database")
	require.Contains(t, fmt.Sprint(panicked), "upgrade handler is missing for v6.8 upgrade plan",
		"v6.7 panicked for a different reason: %v", panicked)
}

// seedV68OfflineVestingAccounts writes one account of every vesting type
// through the v6.7 keepers, each keyed and holding a balance its schedule
// locks. The delayed vesting account is recorded as the sender of a
// post-upgrade transaction that spends that balance.
func seedV68OfflineVestingAccounts(
	t *testing.T,
	testApp *App,
	ctx sdk.Context,
) ([]offlineUpgradeVestingAccount, offlineUpgradeRetainedState) {
	t.Helper()
	locked := sdk.NewCoins(sdk.NewInt64Coin("usei", v68OfflineVestingAmount))
	start := ctx.BlockTime().Unix()
	admin := sdk.AccAddress(secp256k1.GenPrivKey().PubKey().Address())
	newVestingAccounts := []func(*authtypes.BaseAccount) authtypes.AccountI{
		func(base *authtypes.BaseAccount) authtypes.AccountI {
			return vestingtypes.NewDelayedVestingAccount(base, locked, v68OfflineVestingEndTime, nil)
		},
		func(base *authtypes.BaseAccount) authtypes.AccountI {
			account := vestingtypes.NewContinuousVestingAccount(base, locked, start, v68OfflineVestingEndTime, admin)
			account.DelegatedFree = sdk.NewCoins(sdk.NewInt64Coin("usei", 1))
			return account
		},
		func(base *authtypes.BaseAccount) authtypes.AccountI {
			periods := vestingtypes.Periods{{Length: v68OfflineVestingEndTime - start, Amount: locked}}
			return vestingtypes.NewPeriodicVestingAccount(base, locked, start, periods, nil)
		},
		func(base *authtypes.BaseAccount) authtypes.AccountI {
			return vestingtypes.NewPermanentLockedAccount(base, locked, nil)
		},
		func(base *authtypes.BaseAccount) authtypes.AccountI {
			return vestingtypes.NewBaseVestingAccount(base, locked, v68OfflineVestingEndTime, nil)
		},
	}

	var retained offlineUpgradeRetainedState
	accounts := make([]offlineUpgradeVestingAccount, 0, len(newVestingAccounts))
	for i, newVestingAccount := range newVestingAccounts {
		priv := secp256k1.GenPrivKey()
		address := sdk.AccAddress(priv.PubKey().Address())
		sequence := uint64(i) //nolint:gosec // small test index
		base := authtypes.NewBaseAccount(address, priv.PubKey(), testApp.AccountKeeper.GetNextAccountNumber(ctx), sequence)
		account := newVestingAccount(base)
		testApp.AccountKeeper.SetAccount(ctx, account)
		require.NoError(t, testApp.BankKeeper.MintCoins(ctx, minttypes.ModuleName, locked))
		require.NoError(t, testApp.BankKeeper.SendCoinsFromModuleToAccount(ctx, minttypes.ModuleName, address, locked))
		// A bare BaseVestingAccount carries no schedule, so v6.7 locks nothing on it.
		if _, bare := account.(*vestingtypes.BaseVestingAccount); !bare {
			require.True(t, testApp.BankKeeper.SpendableCoins(ctx, address).IsZero(),
				"v6.7 does not lock the balance of a %T", account)
		}

		accounts = append(accounts, offlineUpgradeVestingAccount{
			Address:       address.String(),
			TypeURL:       "/" + proto.MessageName(account),
			PubKey:        hex.EncodeToString(priv.PubKey().Bytes()),
			AccountNumber: base.GetAccountNumber(),
			Sequence:      sequence,
			Balance:       testApp.BankKeeper.GetAllBalances(ctx, address).String(),
		})
		if _, ok := account.(*vestingtypes.DelayedVestingAccount); ok {
			retained.TxSender = address.String()
			retained.TxSenderKey = hex.EncodeToString(priv.Bytes())
		}
	}

	recipient := sdk.AccAddress(secp256k1.GenPrivKey().PubKey().Address())
	testApp.AccountKeeper.SetAccount(ctx, testApp.AccountKeeper.NewAccountWithAddress(ctx, recipient))
	retained.TxRecipient = recipient.String()
	require.NotEmpty(t, retained.TxSender, "no delayed vesting account was seeded")
	return accounts, retained
}

// requireV68OfflineBalancesLocked requires v6.7 to refuse a bank send of the
// balance the recorded sender's schedule locks.
func requireV68OfflineBalancesLocked(t *testing.T, testApp *App, ctx sdk.Context, retained offlineUpgradeRetainedState) {
	t.Helper()
	sendCtx, _ := ctx.CacheContext()
	err := testApp.BankKeeper.SendCoins(sendCtx,
		sdk.MustAccAddressFromBech32(retained.TxSender),
		sdk.MustAccAddressFromBech32(retained.TxRecipient),
		sdk.NewCoins(sdk.NewInt64Coin("usei", 1)))
	require.ErrorIs(t, err, sdkerrors.ErrInsufficientFunds,
		"v6.7 let a delayed vesting account spend the balance its schedule locks")
}

// requireV68OfflineUnupgradedHalt drives a copy of the pre-upgrade database
// through the v6.8 plan height.
func requireV68OfflineUnupgradedHalt(t *testing.T, root string, sourceHeight, upgradeHeight int64) {
	t.Helper()
	haltRoot := filepath.Join(root, "unupgraded-halt")
	copyOfflineUpgradeDatabase(t, root, haltRoot)

	testApp := openOfflineUpgradeApp(t, haltRoot, false)
	require.Equal(t, sourceHeight, testApp.LastBlockHeight())
	require.False(t, testApp.UpgradeKeeper.HasHandler("v6.8"), "v6.7 registered a v6.8 upgrade handler")

	var panicked any
	func() {
		defer func() { panicked = recover() }()
		_, err := testApp.FinalizeBlock(context.Background(), &abci.RequestFinalizeBlock{
			Hash: []byte("offline-upgrade-unupgraded-halt"),
			Header: &tmproto.Header{
				ChainID: offlineUpgradeChainID,
				Height:  upgradeHeight,
			},
		})
		require.NoError(t, err, "v6.7 returned from FinalizeBlock without panicking")
	}()
	require.NotNil(t, panicked, "v6.7 produced the v6.8 plan height")
	msg := fmt.Sprint(panicked)
	require.Contains(t, msg, `UPGRADE "v6.8" NEEDED`, "halt panic is missing the upgrade name: %v", panicked)
	require.Contains(t, msg, fmt.Sprintf("height: %d", upgradeHeight),
		"halt panic is missing plan height %d: %v", upgradeHeight, panicked)
	require.Equal(t, sourceHeight, testApp.LastBlockHeight(), "v6.7 committed the v6.8 plan height")
	closeOfflineUpgradeApp(t, testApp)

	reopened := openOfflineUpgradeApp(t, haltRoot, false)
	defer closeOfflineUpgradeApp(t, reopened)
	require.Equal(t, sourceHeight, reopened.LastBlockHeight(),
		"v6.7 left committed state behind after halting at the v6.8 plan height")
}
