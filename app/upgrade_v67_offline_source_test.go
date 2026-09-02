//go:build upgrade_v67 && offline_upgrade && upgrade_source

package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sei-protocol/sei-chain/sei-cosmos/crypto/keys/secp256k1"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	capabilitytypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/capability/types"
	"github.com/sei-protocol/sei-chain/sei-cosmos/x/feegrant"
	upgradetypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/upgrade/types"
	ibctransfertypes "github.com/sei-protocol/sei-chain/sei-ibc-go/modules/apps/transfer/types"
	ibcclienttypes "github.com/sei-protocol/sei-chain/sei-ibc-go/modules/core/02-client/types"
	connectiontypes "github.com/sei-protocol/sei-chain/sei-ibc-go/modules/core/03-connection/types"
	channeltypes "github.com/sei-protocol/sei-chain/sei-ibc-go/modules/core/04-channel/types"
	commitmenttypes "github.com/sei-protocol/sei-chain/sei-ibc-go/modules/core/23-commitment/types"
	ibchost "github.com/sei-protocol/sei-chain/sei-ibc-go/modules/core/24-host"
	ibctmtypes "github.com/sei-protocol/sei-chain/sei-ibc-go/modules/light-clients/07-tendermint/types"
	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	tmproto "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"
	minttypes "github.com/sei-protocol/sei-chain/x/mint/types"
	"github.com/stretchr/testify/require"
)

var v67OfflineSourceStores = []string{
	"feegrant",
	"capability",
	"ibc",
	"transfer",
}

const (
	v67OfflineEscrowAmount  int64 = 777_777
	v67OfflineVoucherAmount int64 = 1_234_567
)

func TestV67OfflineUpgradeSource(t *testing.T) {
	root := requireOfflineUpgradePhase(t, "source")
	testApp := openOfflineUpgradeApp(t, root, true)
	ctx := testApp.GetContextForDeliverTx(nil).WithBlockTime(time.Now().UTC())

	retained := seedV67OfflineUpgradeState(t, testApp, ctx)
	stores := snapshotV67OfflineUpgradeStores(t, testApp, ctx)
	upgradeHeight := ctx.BlockHeight() + 2
	require.NoError(t, testApp.UpgradeKeeper.ScheduleUpgrade(ctx, upgradetypes.Plan{
		Name:   "v6.7",
		Height: upgradeHeight,
	}))

	commitOfflineUpgradeApp(t, testApp)
	sourceHeight := testApp.LastBlockHeight()
	moduleVersions := offlineUpgradeModuleVersions(t, testApp)
	expectedModules := append([]string(nil), v67OfflineSourceStores...)
	expectedModules = append(expectedModules, "oracle")
	for _, module := range expectedModules {
		require.Contains(t, moduleVersions, module,
			"v6.6 module version map does not contain %s", module)
	}
	closeOfflineUpgradeApp(t, testApp)

	writeOfflineUpgradeArtifact(t, root, offlineUpgradeArtifact{
		Upgrade:        "v6.7",
		SourceHeight:   sourceHeight,
		UpgradeHeight:  upgradeHeight,
		ModuleVersions: moduleVersions,
		Stores:         stores,
		Retained:       retained,
	})

	requireV67OfflineUnupgradedHalt(t, root, sourceHeight, upgradeHeight)
}

func TestV67OfflineUpgradeReopen(t *testing.T) {
	root := requireOfflineUpgradePhase(t, "reopen")
	artifact := readOfflineUpgradeArtifact(t, root)
	require.Equal(t, "v6.7", artifact.Upgrade)
	require.NotEmpty(t, artifact.UpgradeHash, "target phase did not record the post-upgrade application hash")

	migrated := offlineUpgradeMigratedDatabase(t, root, artifact)
	reopenRoot := filepath.Join(root, "reopen")
	copyOfflineUpgradeDatabase(t, migrated, reopenRoot)

	testApp := openOfflineUpgradeApp(t, reopenRoot, false)
	defer closeOfflineUpgradeApp(t, testApp)

	require.Equal(t, artifact.UpgradeHeight, testApp.LastBlockHeight(),
		"v6.6 opened the migrated database at a different height than v6.7 left it")
	openedHash := offlineUpgradeHashString(committedOfflineUpgradeHash(t, testApp))
	require.NotEqual(t, artifact.UpgradeHash, openedHash,
		"v6.6 opened the migrated database with the same application hash v6.7 committed at height %d; an operator rolling back would not fork at the upgrade height\nv6.7=%s\nv6.6=%s",
		artifact.UpgradeHeight, artifact.UpgradeHash, openedHash)

	versions := offlineUpgradeModuleVersions(t, testApp)
	for _, module := range v67OfflineSourceStores {
		require.NotContains(t, versions, module,
			"v6.6 still sees a version-map entry for %s after v6.7 deleted it", module)
	}
	require.Contains(t, versions, "oracle")

	ctx := offlineUpgradeReadContext(testApp, testApp.LastBlockHeight())
	_, havePlan := testApp.UpgradeKeeper.GetUpgradePlan(ctx)
	require.False(t, havePlan,
		"v6.6 still sees a pending upgrade plan on the migrated database")

	requireOfflineUpgradeStoresMounted(t, testApp, v67OfflineSourceStores)
	for _, storeName := range v67OfflineSourceStores {
		got := snapshotCommittedOfflineUpgradeStore(t, testApp, storeName)
		require.Equal(t, artifact.Stores[storeName], got,
			"v6.6 cannot read the retained %s state the upgrade left behind", storeName)
	}

	lastName, lastHeight := testApp.UpgradeKeeper.GetLastCompletedUpgrade(ctx)
	require.Equal(t, "v6.7", lastName)
	require.Equal(t, artifact.UpgradeHeight, lastHeight)
	require.False(t, testApp.UpgradeKeeper.HasHandler("v6.7"),
		"v6.6 registered a v6.7 upgrade handler")

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
		require.NoError(t, err, "v6.6 returned from FinalizeBlock without panicking")
	}()
	require.NotNil(t, panicked, "v6.6 produced a block on the migrated database")
	require.Contains(t, fmt.Sprint(panicked), "upgrade handler is missing for v6.7 upgrade plan",
		"v6.6 panicked for a different reason: %v", panicked)
}

// requireV67OfflineUnupgradedHalt drives a copy of the pre-upgrade database
// through the v6.7 plan height.
func requireV67OfflineUnupgradedHalt(t *testing.T, root string, sourceHeight, upgradeHeight int64) {
	t.Helper()
	haltRoot := filepath.Join(root, "unupgraded-halt")
	copyOfflineUpgradeDatabase(t, root, haltRoot)

	testApp := openOfflineUpgradeApp(t, haltRoot, false)
	require.Equal(t, sourceHeight, testApp.LastBlockHeight())
	require.False(t, testApp.UpgradeKeeper.HasHandler("v6.7"),
		"v6.6 registered a v6.7 upgrade handler")

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
		require.NoError(t, err, "v6.6 returned from FinalizeBlock without panicking")
	}()
	require.NotNil(t, panicked, "v6.6 produced the v6.7 plan height")
	msg := fmt.Sprint(panicked)
	require.Contains(t, msg, `UPGRADE "v6.7" NEEDED`,
		"halt panic is missing the upgrade name: %v", panicked)
	require.Contains(t, msg, fmt.Sprintf("height: %d", upgradeHeight),
		"halt panic is missing plan height %d: %v", upgradeHeight, panicked)
	require.Equal(t, sourceHeight, testApp.LastBlockHeight(),
		"v6.6 committed the v6.7 plan height")
	closeOfflineUpgradeApp(t, testApp)

	reopened := openOfflineUpgradeApp(t, haltRoot, false)
	defer closeOfflineUpgradeApp(t, reopened)
	require.Equal(t, sourceHeight, reopened.LastBlockHeight(),
		"v6.6 left committed state behind after halting at the v6.7 plan height")
}

// seedV67OfflineUpgradeState writes real feegrant, capability, IBC, transfer,
// escrow and voucher state through the v6.6 keepers.
func seedV67OfflineUpgradeState(t *testing.T, testApp *App, ctx sdk.Context) offlineUpgradeRetainedState {
	t.Helper()
	retained := offlineUpgradeRetainedState{}
	seedV67Feegrant(t, testApp, ctx, &retained)
	seedV67Capability(t, testApp, ctx, &retained)
	seedV67IBC(t, testApp, ctx, &retained)
	seedV67Transfer(t, testApp, ctx, &retained)
	return retained
}

func seedV67Feegrant(t *testing.T, testApp *App, ctx sdk.Context, retained *offlineUpgradeRetainedState) {
	t.Helper()
	granter := fundOfflineUpgradeAccount(t, testApp, ctx)
	grantee := fundOfflineUpgradeAccount(t, testApp, ctx)
	secondGrantee := fundOfflineUpgradeAccount(t, testApp, ctx)
	allowance := &feegrant.BasicAllowance{
		SpendLimit: sdk.NewCoins(sdk.NewInt64Coin("usei", 1_000_000)),
	}
	require.NoError(t, testApp.FeeGrantKeeper.GrantAllowance(ctx, granter, grantee, allowance))
	require.NoError(t, testApp.FeeGrantKeeper.GrantAllowance(ctx, granter, secondGrantee, &feegrant.BasicAllowance{
		SpendLimit: sdk.NewCoins(sdk.NewInt64Coin("usei", 1)),
	}))

	got, err := testApp.FeeGrantKeeper.GetAllowance(ctx, granter, grantee)
	require.NoError(t, err)
	require.NotNil(t, got)

	retained.FeegrantGranter = granter.String()
	retained.FeegrantGrantee = grantee.String()
	retained.FeegrantKey = encodeOfflineUpgradeKey(feegrant.FeeAllowanceKey(granter, grantee))
}

func seedV67Capability(t *testing.T, testApp *App, ctx sdk.Context, retained *offlineUpgradeRetainedState) {
	t.Helper()
	const name = "offline-upgrade"
	capability, err := testApp.ScopedIBCKeeper.NewCapability(ctx, name)
	require.NoError(t, err)
	require.NoError(t, testApp.ScopedTransferKeeper.ClaimCapability(ctx, capability, name))

	fetched, ok := testApp.ScopedIBCKeeper.GetCapability(ctx, name)
	require.True(t, ok)
	require.Equal(t, capability.Index, fetched.Index)

	owners, found := testApp.CapabilityKeeper.GetOwners(ctx, capability.Index)
	require.True(t, found)
	require.Len(t, owners.Owners, 2)

	retained.CapabilityName = name
	retained.CapabilityIndex = capability.Index
	retained.CapabilityOwnersKey = encodeOfflineUpgradeKey(append(
		append([]byte{}, capabilitytypes.KeyPrefixIndexCapability...),
		capabilitytypes.IndexToKey(capability.Index)...,
	))
}

func seedV67IBC(t *testing.T, testApp *App, ctx sdk.Context, retained *offlineUpgradeRetainedState) {
	t.Helper()
	clientState := ibctmtypes.NewClientState(
		"testchain",
		ibctmtypes.DefaultTrustLevel,
		14*24*time.Hour,
		21*24*time.Hour,
		10*time.Second,
		ibcclienttypes.NewHeight(0, 1),
		commitmenttypes.GetSDKSpecs(),
		[]string{"upgrade", "upgradedIBCState"},
		true,
		true,
	)
	hash := sha256.Sum256([]byte("offline-upgrade-consensus"))
	consensusState := ibctmtypes.NewConsensusState(
		time.Unix(1_700_000_000, 0).UTC(),
		commitmenttypes.NewMerkleRoot(hash[:]),
		hash[:],
	)
	clientID, err := testApp.IBCKeeper.ClientKeeper.CreateClient(ctx, clientState, consensusState)
	require.NoError(t, err)
	storedClient, found := testApp.IBCKeeper.ClientKeeper.GetClientState(ctx, clientID)
	require.True(t, found)
	require.Equal(t, clientState.ClientType(), storedClient.ClientType())

	counterparty := connectiontypes.NewCounterparty(
		"07-tendermint-1",
		"connection-0",
		commitmenttypes.NewMerklePrefix([]byte("ibc")),
	)
	connectionID, err := testApp.IBCKeeper.ConnectionKeeper.ConnOpenInit(ctx, clientID, counterparty, nil, 0)
	require.NoError(t, err)
	_, found = testApp.IBCKeeper.ConnectionKeeper.GetConnection(ctx, connectionID)
	require.True(t, found)

	channelID := testApp.IBCKeeper.ChannelKeeper.GenerateChannelIdentifier(ctx)
	channel := channeltypes.NewChannel(
		channeltypes.OPEN,
		channeltypes.UNORDERED,
		channeltypes.NewCounterparty(ibctransfertypes.PortID, "channel-0"),
		[]string{connectionID},
		ibctransfertypes.Version,
	)
	testApp.IBCKeeper.ChannelKeeper.SetChannel(ctx, ibctransfertypes.PortID, channelID, channel)
	testApp.IBCKeeper.ChannelKeeper.SetNextSequenceSend(ctx, ibctransfertypes.PortID, channelID, 1)
	storedChannel, found := testApp.IBCKeeper.ChannelKeeper.GetChannel(ctx, ibctransfertypes.PortID, channelID)
	require.True(t, found)
	require.Equal(t, channeltypes.OPEN, storedChannel.State)

	retained.IBCClientID = clientID
	retained.IBCClientStateKey = encodeOfflineUpgradeKey(ibchost.FullClientStateKey(clientID))
	retained.IBCConnectionID = connectionID
	retained.IBCConnectionKey = encodeOfflineUpgradeKey(ibchost.ConnectionKey(connectionID))
	retained.IBCPortID = ibctransfertypes.PortID
	retained.IBCChannelID = channelID
	retained.IBCChannelKey = encodeOfflineUpgradeKey(ibchost.ChannelKey(ibctransfertypes.PortID, channelID))
}

func seedV67Transfer(t *testing.T, testApp *App, ctx sdk.Context, retained *offlineUpgradeRetainedState) {
	t.Helper()
	trace := ibctransfertypes.DenomTrace{
		Path:      "transfer/channel-0",
		BaseDenom: "uatom",
	}
	testApp.TransferKeeper.SetDenomTrace(ctx, trace)
	got, found := testApp.TransferKeeper.GetDenomTrace(ctx, trace.Hash())
	require.True(t, found)
	require.Equal(t, trace, got)
	require.Equal(t, "ibc/"+strings.ToUpper(hex.EncodeToString(trace.Hash())), got.IBCDenom())

	retained.TransferDenomHash = strings.ToUpper(hex.EncodeToString(trace.Hash()))
	retained.TransferIBCDenom = got.IBCDenom()
	retained.TransferTraceKey = encodeOfflineUpgradeKey(append(
		append([]byte{}, ibctransfertypes.DenomTraceKey...),
		trace.Hash()...,
	))

	seedV67TransferEscrow(t, testApp, ctx, retained)
	seedV67TransferVoucher(t, testApp, ctx, retained)
	recordV67EscrowSupply(t, testApp, ctx, retained)
}

// recordV67EscrowSupply records the current usei total supply.
func recordV67EscrowSupply(t *testing.T, testApp *App, ctx sdk.Context, retained *offlineUpgradeRetainedState) {
	t.Helper()
	supply := testApp.BankKeeper.GetSupply(ctx, "usei")
	escrowAmt, ok := sdk.NewIntFromString(retained.EscrowAmount)
	require.True(t, ok)
	require.True(t, supply.Amount.GTE(escrowAmt),
		"escrowed coins are not counted in total supply")
	retained.EscrowSupply = supply.Amount.String()
}

// seedV67TransferEscrow locks native coins in the escrow account for the seeded
// port and channel.
func seedV67TransferEscrow(t *testing.T, testApp *App, ctx sdk.Context, retained *offlineUpgradeRetainedState) {
	t.Helper()
	escrowAddr := ibctransfertypes.GetEscrowAddress(retained.IBCPortID, retained.IBCChannelID)
	sender := fundOfflineUpgradeAccount(t, testApp, ctx)
	locked := sdk.NewInt64Coin("usei", v67OfflineEscrowAmount)
	require.NoError(t, testApp.BankKeeper.SendCoins(ctx, sender, escrowAddr, sdk.NewCoins(locked)))

	got := testApp.BankKeeper.GetBalance(ctx, escrowAddr, locked.Denom)
	require.Equal(t, locked, got, "escrow account was not credited")

	retained.EscrowAddress = escrowAddr.String()
	retained.EscrowAmount = got.Amount.String()
}

// seedV67TransferVoucher mints the seeded denom trace's IBC voucher to a holder
// account.
func seedV67TransferVoucher(t *testing.T, testApp *App, ctx sdk.Context, retained *offlineUpgradeRetainedState) {
	t.Helper()
	require.NotEmpty(t, retained.TransferIBCDenom)
	holder := fundOfflineUpgradeAccount(t, testApp, ctx)
	voucher := sdk.NewInt64Coin(retained.TransferIBCDenom, v67OfflineVoucherAmount)
	require.NoError(t, testApp.BankKeeper.MintCoins(ctx, ibctransfertypes.ModuleName, sdk.NewCoins(voucher)))
	require.NoError(t, testApp.BankKeeper.SendCoinsFromModuleToAccount(
		ctx, ibctransfertypes.ModuleName, holder, sdk.NewCoins(voucher)))

	got := testApp.BankKeeper.GetBalance(ctx, holder, voucher.Denom)
	require.Equal(t, voucher, got, "voucher holder was not credited")
	supply := testApp.BankKeeper.GetSupply(ctx, voucher.Denom)
	require.Equal(t, got, supply, "voucher total supply does not match the holder balance")
	require.False(t, supply.IsZero(), "voucher total supply is zero")

	retained.VoucherHolder = holder.String()
	retained.VoucherAmount = got.Amount.String()
	retained.VoucherSupply = supply.Amount.String()
}

func fundOfflineUpgradeAccount(t *testing.T, testApp *App, ctx sdk.Context) sdk.AccAddress {
	t.Helper()
	addr := sdk.AccAddress(secp256k1.GenPrivKey().PubKey().Address())
	acc := testApp.AccountKeeper.NewAccountWithAddress(ctx, addr)
	testApp.AccountKeeper.SetAccount(ctx, acc)
	coins := sdk.NewCoins(sdk.NewInt64Coin("usei", 1_000_000_000))
	require.NoError(t, testApp.BankKeeper.MintCoins(ctx, minttypes.ModuleName, coins))
	require.NoError(t, testApp.BankKeeper.SendCoinsFromModuleToAccount(ctx, minttypes.ModuleName, addr, coins))
	require.False(t, testApp.BankKeeper.GetBalance(ctx, addr, "usei").IsZero())
	return addr
}

func snapshotV67OfflineUpgradeStores(t *testing.T, testApp *App, ctx sdk.Context) map[string]map[string]string {
	t.Helper()
	stores := snapshotOfflineUpgradeStores(t, testApp, ctx, v67OfflineSourceStores)
	for storeName, entries := range stores {
		require.Greater(t, len(entries), 1,
			"%s store has %d keys after keeper writes; a sentinel write produces one",
			storeName, len(entries))
	}
	return stores
}
