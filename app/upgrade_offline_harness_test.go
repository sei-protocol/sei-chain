//go:build offline_upgrade

package app

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"

	serverconfig "github.com/sei-protocol/sei-chain/sei-cosmos/server/config"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	upgradetypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/upgrade/types"
	"github.com/sei-protocol/sei-chain/sei-db/common/utils"
	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/config"
	tmproto "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"
	"github.com/sei-protocol/sei-chain/sei-wasmd/x/wasm"
	evmtypes "github.com/sei-protocol/sei-chain/x/evm/types"
	"github.com/stretchr/testify/require"
	dbm "github.com/tendermint/tm-db"
)

const (
	offlineUpgradeArtifactEnv     = "UPGRADE_TEST_ARTIFACT"
	offlineUpgradeChainID         = "sei-test"
	offlineUpgradePhaseEnv        = "UPGRADE_TEST_PHASE"
	offlineUpgradeSnapshotHomeEnv = "UPGRADE_TEST_SNAPSHOT_HOME"
	// offlineUpgradeMigratedDir is the artifact subdirectory that holds the
	// committed post-upgrade database.
	offlineUpgradeMigratedDir = "clean"
)

type offlineUpgradeArtifact struct {
	Upgrade        string                       `json:"upgrade"`
	SourceHeight   int64                        `json:"source_height"`
	UpgradeHeight  int64                        `json:"upgrade_height"`
	ModuleVersions []string                     `json:"module_versions"`
	Stores         map[string]map[string]string `json:"stores"`
	Retained       offlineUpgradeRetainedState  `json:"retained"`
	MigratedRoot   string                       `json:"migrated_root"`
	UpgradeHash    string                       `json:"upgrade_hash"`
}

// offlineUpgradeRetainedState identifies the state the source phase wrote: the
// store keys of each retired module, the bank balances behind IBC escrow and
// voucher denoms, and the accounts a post-upgrade transaction moves funds
// between. TxSenderKey is a throwaway key generated for the fixture database,
// which the target phase needs in order to sign as that account.
type offlineUpgradeRetainedState struct {
	FeegrantGranter     string `json:"feegrant_granter"`
	FeegrantGrantee     string `json:"feegrant_grantee"`
	FeegrantKey         string `json:"feegrant_key"`
	CapabilityName      string `json:"capability_name"`
	CapabilityIndex     uint64 `json:"capability_index"`
	CapabilityOwnersKey string `json:"capability_owners_key"`
	IBCClientID         string `json:"ibc_client_id"`
	IBCClientStateKey   string `json:"ibc_client_state_key"`
	IBCConnectionID     string `json:"ibc_connection_id"`
	IBCConnectionKey    string `json:"ibc_connection_key"`
	IBCPortID           string `json:"ibc_port_id"`
	IBCChannelID        string `json:"ibc_channel_id"`
	IBCChannelKey       string `json:"ibc_channel_key"`
	TransferDenomHash   string `json:"transfer_denom_hash"`
	TransferIBCDenom    string `json:"transfer_ibc_denom"`
	TransferTraceKey    string `json:"transfer_trace_key"`
	EscrowAddress       string `json:"escrow_address"`
	EscrowAmount        string `json:"escrow_amount"`
	EscrowSupply        string `json:"escrow_supply"`
	VoucherHolder       string `json:"voucher_holder"`
	VoucherAmount       string `json:"voucher_amount"`
	VoucherSupply       string `json:"voucher_supply"`
	TxSender            string `json:"tx_sender"`
	TxSenderKey         string `json:"tx_sender_key"`
	TxRecipient         string `json:"tx_recipient"`
}

func requireOfflineUpgradePhase(t *testing.T, want string) string {
	t.Helper()
	require.Equal(t, want, os.Getenv(offlineUpgradePhaseEnv),
		"%s must select this test phase", offlineUpgradePhaseEnv)
	root := os.Getenv(offlineUpgradeArtifactEnv)
	require.NotEmpty(t, root, "%s is required", offlineUpgradeArtifactEnv)
	absolute, err := filepath.Abs(root)
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(absolute, 0o750))
	return absolute
}

type offlineUpgradeChainIDOpts struct {
	TestAppOpts
	chainID string
}

func (o offlineUpgradeChainIDOpts) Get(s string) interface{} {
	if s == "chain-id" {
		return o.chainID
	}
	return o.TestAppOpts.Get(s)
}

// requireOfflineUpgradeSnapshotHome returns the node home named by
// UPGRADE_TEST_SNAPSHOT_HOME. It skips when the variable is unset and fails
// when the path cannot be opened as a node home.
func requireOfflineUpgradeSnapshotHome(t *testing.T) string {
	t.Helper()
	home := os.Getenv(offlineUpgradeSnapshotHomeEnv)
	if home == "" {
		t.Skipf("%s is unset; set it to a node home directory to run retained-state checks against a real snapshot",
			offlineUpgradeSnapshotHomeEnv)
	}
	absolute, err := filepath.Abs(home)
	require.NoError(t, err, "%s=%q is not a usable path", offlineUpgradeSnapshotHomeEnv, home)
	info, err := os.Stat(absolute)
	require.NoError(t, err, "%s=%q does not exist", offlineUpgradeSnapshotHomeEnv, home)
	require.True(t, info.IsDir(), "%s=%q is not a directory", offlineUpgradeSnapshotHomeEnv, home)

	genesis := filepath.Join(absolute, "config", "genesis.json")
	_, err = os.Stat(genesis)
	require.NoError(t, err, "%s=%q is not a node home: missing config/genesis.json",
		offlineUpgradeSnapshotHomeEnv, home)

	// The store path a node resolves depends on which layout it was created
	// with, so ask the same resolver the app itself uses rather than guessing.
	commitStore := utils.GetCosmosSCStorePath(absolute)
	require.True(t, utils.DirExists(commitStore),
		"%s=%q has no state commitment store at %s",
		offlineUpgradeSnapshotHomeEnv, home, commitStore)
	return absolute
}

func readOfflineUpgradeGenesisChainID(t *testing.T, home string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(home, "config", "genesis.json"))
	require.NoError(t, err, "read %s/config/genesis.json", home)
	var genesis struct {
		ChainID string `json:"chain_id"`
	}
	require.NoError(t, json.Unmarshal(raw, &genesis), "parse %s/config/genesis.json", home)
	require.NotEmpty(t, genesis.ChainID, "%s/config/genesis.json has no chain_id", home)
	return genesis.ChainID
}

func openOfflineUpgradeSnapshotApp(t *testing.T, home, chainID string) *App {
	t.Helper()
	require.NotEmpty(t, chainID)
	encodingConfig := MakeEncodingConfig()
	options := []AppOption{
		func(app *App) {
			receiptStore, receiptErr := setupReceiptStore(app.keys[evmtypes.StoreKey])
			require.NoError(t, receiptErr)
			app.receiptStore = receiptStore
		},
	}
	testApp := New(
		dbm.NewMemDB(),
		nil,
		true,
		map[int64]bool{},
		home,
		1,
		false,
		config.TestConfig(),
		encodingConfig,
		wasm.EnableAllProposals,
		offlineUpgradeChainIDOpts{chainID: chainID},
		EmptyWasmOpts,
		options,
	)
	requireOfflineUpgradeExecutionConfig(t, testApp)
	require.Positive(t, testApp.LastBlockHeight(),
		"%s opened with LastBlockHeight 0; the application database is empty or unreadable", home)
	return testApp
}

func openOfflineUpgradeApp(t *testing.T, root string, initialize bool) *App {
	t.Helper()
	dbDir := filepath.Join(root, "application")
	require.NoError(t, os.MkdirAll(dbDir, 0o750))
	db, err := dbm.NewGoLevelDB("application", dbDir)
	require.NoError(t, err)

	encodingConfig := MakeEncodingConfig()
	var genesisStateBytes []byte
	if initialize {
		genesisState := NewDefaultGenesisState(encodingConfig.Marshaler)
		genesisStateBytes, err = json.Marshal(genesisState)
		require.NoError(t, err)
	}
	options := []AppOption{
		func(app *App) {
			receiptStore, receiptErr := setupReceiptStore(app.keys[evmtypes.StoreKey])
			require.NoError(t, receiptErr)
			app.receiptStore = receiptStore
		},
	}
	testApp := New(
		db,
		nil,
		true,
		map[int64]bool{},
		filepath.Join(root, "home"),
		1,
		false,
		config.TestConfig(),
		encodingConfig,
		wasm.EnableAllProposals,
		TestAppOpts{},
		EmptyWasmOpts,
		options,
	)
	requireOfflineUpgradeExecutionConfig(t, testApp)
	if initialize {
		initializeOfflineUpgradeApp(t, testApp, genesisStateBytes)
	}
	return testApp
}

// requireOfflineUpgradeExecutionConfig asserts that this harness constructs an
// app with OCC disabled and DefaultConcurrencyWorkers. The fleet sets
// occ-enabled = true and the live harness sets concurrency-workers = 4.
func requireOfflineUpgradeExecutionConfig(t *testing.T, testApp *App) {
	t.Helper()
	require.False(t, testApp.OccEnabled(),
		"offline upgrade tests ran with BaseApp.OccEnabled()=%v, want false; this layer's application-hash determinism is not the fleet's (occ-enabled = true)",
		testApp.OccEnabled())
	require.Equal(t, serverconfig.DefaultConcurrencyWorkers, testApp.ConcurrencyWorkers(),
		"offline upgrade tests ran with BaseApp.ConcurrencyWorkers()=%d, want DefaultConcurrencyWorkers=%d; the live harness sets 4",
		testApp.ConcurrencyWorkers(), serverconfig.DefaultConcurrencyWorkers)
}

// initializeOfflineUpgradeApp calls the InitChain signature provided by the
// branch under test. Both signatures accept the same request.
func initializeOfflineUpgradeApp(t *testing.T, testApp *App, stateBytes []byte) {
	t.Helper()
	request := &abci.RequestInitChain{
		ConsensusParams: DefaultConsensusParams,
		ChainId:         offlineUpgradeChainID,
		AppStateBytes:   stateBytes,
	}

	method := reflect.ValueOf(testApp).MethodByName("InitChain")
	require.True(t, method.IsValid(), "app has no InitChain method")
	args := []reflect.Value{reflect.ValueOf(request)}
	if method.Type().NumIn() == 2 {
		args = append([]reflect.Value{reflect.ValueOf(context.Background())}, args...)
	}
	require.Len(t, args, method.Type().NumIn(), "unsupported InitChain signature")
	results := method.Call(args)
	require.NotEmpty(t, results, "InitChain returned no values")
	if last := results[len(results)-1]; last.Type().Implements(reflect.TypeFor[error]()) && !last.IsNil() {
		t.Fatalf("InitChain: %v", last.Interface())
	}
}

func closeOfflineUpgradeApp(t *testing.T, testApp *App) {
	t.Helper()
	require.NoError(t, testApp.Close())
}

// copyOfflineUpgradeDatabase copies the application database directories under
// srcRoot to a new dstRoot.
func copyOfflineUpgradeDatabase(t *testing.T, srcRoot, dstRoot string) {
	t.Helper()
	require.NoError(t, os.RemoveAll(dstRoot))
	require.NoError(t, os.MkdirAll(dstRoot, 0o750))
	for _, name := range []string{"application", "home"} {
		src := filepath.Join(srcRoot, name)
		if _, err := os.Stat(src); os.IsNotExist(err) {
			continue
		}
		copyOfflineUpgradeTree(t, src, filepath.Join(dstRoot, name))
	}
	_, err := os.Stat(filepath.Join(dstRoot, "application"))
	require.NoError(t, err, "copied database has no application directory")
}

func copyOfflineUpgradeTree(t *testing.T, src, dst string) {
	t.Helper()
	info, err := os.Stat(src)
	require.NoError(t, err)
	require.True(t, info.IsDir(), "%s is not a directory", src)
	require.NoError(t, os.MkdirAll(dst, 0o750))
	entries, err := os.ReadDir(src)
	require.NoError(t, err)
	for _, entry := range entries {
		from := filepath.Join(src, entry.Name())
		to := filepath.Join(dst, entry.Name())
		info, err := os.Lstat(from)
		require.NoError(t, err)
		switch {
		case info.Mode()&os.ModeSymlink != 0:
			copyOfflineUpgradeSymlink(t, from, to)
		case info.IsDir():
			copyOfflineUpgradeTree(t, from, to)
		default:
			copyOfflineUpgradeFile(t, from, to)
		}
	}
}

func copyOfflineUpgradeSymlink(t *testing.T, src, dst string) {
	t.Helper()
	target, err := os.Readlink(src)
	require.NoError(t, err)
	require.NoError(t, os.Symlink(target, dst))
}

func copyOfflineUpgradeFile(t *testing.T, src, dst string) {
	t.Helper()
	in, err := os.Open(src)
	require.NoError(t, err)
	defer func() {
		require.NoError(t, in.Close())
	}()
	info, err := in.Stat()
	require.NoError(t, err)
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, info.Mode().Perm())
	require.NoError(t, err)
	defer func() {
		require.NoError(t, out.Close())
	}()
	_, err = io.Copy(out, in)
	require.NoError(t, err)
}

func commitOfflineUpgradeApp(t *testing.T, testApp *App) {
	t.Helper()
	_, err := testApp.Commit(context.Background())
	require.NoError(t, err)
}

func committedOfflineUpgradeHash(t *testing.T, testApp *App) []byte {
	t.Helper()
	hash := append([]byte(nil), testApp.LastCommitID().Hash...)
	require.NotEmpty(t, hash, "committed application hash is empty")
	return hash
}

func offlineUpgradeHashString(hash []byte) string {
	return hex.EncodeToString(hash)
}

// offlineUpgradeMigratedDatabase returns the committed post-upgrade database
// named by artifact.MigratedRoot under root.
func offlineUpgradeMigratedDatabase(t *testing.T, root string, artifact offlineUpgradeArtifact) string {
	t.Helper()
	require.NotEmpty(t, artifact.MigratedRoot, "target phase did not record the migrated database path")
	require.Equal(t, filepath.Base(artifact.MigratedRoot), artifact.MigratedRoot,
		"migrated database path must be a directory name under the artifact root")
	migrated := filepath.Join(root, artifact.MigratedRoot)
	_, err := os.Stat(filepath.Join(migrated, "application"))
	require.NoError(t, err, "migrated database %s has no application directory", migrated)
	return migrated
}

func offlineUpgradeReadContext(testApp *App, height int64) sdk.Context {
	return offlineUpgradeContext(testApp, height, offlineUpgradeChainID)
}

func offlineUpgradeContext(testApp *App, height int64, chainID string) sdk.Context {
	return testApp.NewUncachedContext(false, tmproto.Header{
		ChainID: chainID,
		Height:  height,
	})
}

func snapshotOfflineUpgradeStore(
	t *testing.T,
	testApp *App,
	ctx sdk.Context,
	storeName string,
) map[string]string {
	t.Helper()
	storeKey := testApp.GetKey(storeName)
	require.NotNil(t, storeKey, "%s store is not mounted", storeName)
	iterator := ctx.KVStore(storeKey).Iterator(nil, nil)
	defer func() {
		require.NoError(t, iterator.Close())
	}()

	entries := map[string]string{}
	for ; iterator.Valid(); iterator.Next() {
		key := encodeOfflineUpgradeKey(iterator.Key())
		entries[key] = base64.StdEncoding.EncodeToString(iterator.Value())
	}
	return entries
}

func snapshotOfflineUpgradeStores(
	t *testing.T,
	testApp *App,
	ctx sdk.Context,
	storeNames []string,
) map[string]map[string]string {
	t.Helper()
	stores := make(map[string]map[string]string, len(storeNames))
	for _, storeName := range storeNames {
		stores[storeName] = snapshotOfflineUpgradeStore(t, testApp, ctx, storeName)
	}
	return stores
}

func encodeOfflineUpgradeKey(key []byte) string {
	return base64.StdEncoding.EncodeToString(key)
}

func requireOfflineUpgradeStoreKey(t *testing.T, snapshot map[string]string, name, key string) {
	t.Helper()
	require.NotEmpty(t, key, "%s was not recorded", name)
	_, ok := snapshot[key]
	require.True(t, ok, "retained %s disappeared", name)
}

// committedOfflineUpgradePlan returns the pending upgrade plan in the committed
// upgrade store.
func committedOfflineUpgradePlan(t *testing.T, testApp *App) (upgradetypes.Plan, bool) {
	t.Helper()
	return testApp.UpgradeKeeper.GetUpgradePlan(offlineUpgradeReadContext(testApp, testApp.LastBlockHeight()))
}

func committedUpgradeStore(t *testing.T, testApp *App) sdk.KVStore {
	t.Helper()
	key := testApp.GetKey(upgradetypes.StoreKey)
	require.NotNil(t, key, "upgrade store is not mounted")
	store := testApp.CommitMultiStore().GetCommitKVStore(key)
	require.NotNil(t, store, "upgrade store is not in the commit multistore")
	return store
}

func offlineUpgradeModuleVersionKey(module string) []byte {
	return append([]byte{upgradetypes.VersionMapByte}, []byte(module)...)
}

// offlineUpgradeModuleVersions returns module names stored under the version-map
// prefix in the committed upgrade store.
func offlineUpgradeModuleVersions(t *testing.T, testApp *App) []string {
	t.Helper()
	iterator := sdk.KVStorePrefixIterator(committedUpgradeStore(t, testApp), []byte{upgradetypes.VersionMapByte})
	defer func() {
		require.NoError(t, iterator.Close())
	}()

	names := make([]string, 0)
	for ; iterator.Valid(); iterator.Next() {
		key := iterator.Key()
		require.Greater(t, len(key), 1)
		names = append(names, string(key[1:]))
	}
	sort.Strings(names)
	return names
}

func offlineUpgradeHasModuleVersion(t *testing.T, testApp *App, module string) bool {
	t.Helper()
	return committedUpgradeStore(t, testApp).Has(offlineUpgradeModuleVersionKey(module))
}

func requireOfflineUpgradeStoresMounted(t *testing.T, testApp *App, storeNames []string) {
	t.Helper()
	cms := testApp.CommitMultiStore()
	mounted := map[string]struct{}{}
	for _, key := range cms.StoreKeys() {
		mounted[key.Name()] = struct{}{}
	}
	for _, name := range storeNames {
		_, ok := mounted[name]
		require.True(t, ok, "%s is not present in the commit multistore", name)
		key := testApp.GetKey(name)
		require.NotNil(t, key, "%s store is not mounted", name)
		require.NotNil(t, cms.GetCommitKVStore(key), "%s is not in the commit multistore", name)
	}
}

func snapshotCommittedOfflineUpgradeStore(t *testing.T, testApp *App, storeName string) map[string]string {
	t.Helper()
	storeKey := testApp.GetKey(storeName)
	require.NotNil(t, storeKey, "%s store is not mounted", storeName)
	store := testApp.CommitMultiStore().GetCommitKVStore(storeKey)
	require.NotNil(t, store, "%s is not in the commit multistore", storeName)
	iterator := store.Iterator(nil, nil)
	defer func() {
		require.NoError(t, iterator.Close())
	}()
	entries := map[string]string{}
	for ; iterator.Valid(); iterator.Next() {
		entries[encodeOfflineUpgradeKey(iterator.Key())] = base64.StdEncoding.EncodeToString(iterator.Value())
	}
	return entries
}

// requireOfflineUpgradeStoreProof verifies a commitment proof for the
// lexicographically first encoded key in snapshot.
func requireOfflineUpgradeStoreProof(t *testing.T, testApp *App, storeName string, snapshot map[string]string) {
	t.Helper()
	require.NotEmpty(t, snapshot, "%s snapshot is empty; a proof query would be vacuous", storeName)
	encodedKeys := make([]string, 0, len(snapshot))
	for encodedKey := range snapshot {
		encodedKeys = append(encodedKeys, encodedKey)
	}
	sort.Strings(encodedKeys)
	encodedKey := encodedKeys[0]
	key, err := base64.StdEncoding.DecodeString(encodedKey)
	require.NoError(t, err)
	want, err := base64.StdEncoding.DecodeString(snapshot[encodedKey])
	require.NoError(t, err)
	queryable, ok := testApp.CommitMultiStore().(sdk.Queryable)
	require.True(t, ok, "commit multistore does not support queries")
	resp := queryable.Query(context.Background(), abci.RequestQuery{
		Path:  "/" + storeName + "/key",
		Data:  key,
		Prove: true,
	})
	require.Equal(t, uint32(0), resp.Code, "query /%s/key: %s", storeName, resp.Log)
	require.Equal(t, want, resp.Value, "query /%s/key returned a different value", storeName)
	require.NotNil(t, resp.ProofOps, "%s is missing from the commitment set", storeName)
	require.NotEmpty(t, resp.ProofOps.Ops, "%s is missing from the commitment set", storeName)
}

func requireOfflineUpgradeRetainedStores(t *testing.T, testApp *App, want map[string]map[string]string) {
	t.Helper()
	names := make([]string, 0, len(want))
	for name := range want {
		names = append(names, name)
	}
	sort.Strings(names)
	requireOfflineUpgradeStoresMounted(t, testApp, names)
	for _, name := range names {
		got := snapshotCommittedOfflineUpgradeStore(t, testApp, name)
		require.Equal(t, want[name], got, "v6.7 changed retained %s state", name)
		requireOfflineUpgradeStoreProof(t, testApp, name, want[name])
	}
}

func writeOfflineUpgradeArtifact(t *testing.T, root string, artifact offlineUpgradeArtifact) {
	t.Helper()
	content, err := json.MarshalIndent(artifact, "", "  ")
	require.NoError(t, err)
	content = append(content, '\n')
	require.NoError(t, os.WriteFile(filepath.Join(root, "manifest.json"), content, 0o600))
}

func readOfflineUpgradeArtifact(t *testing.T, root string) offlineUpgradeArtifact {
	t.Helper()
	content, err := os.ReadFile(filepath.Join(root, "manifest.json"))
	require.NoError(t, err)
	var artifact offlineUpgradeArtifact
	require.NoError(t, json.Unmarshal(content, &artifact))
	require.NotEmpty(t, artifact.Upgrade)
	require.NotEmpty(t, artifact.ModuleVersions)
	require.NotEmpty(t, artifact.Stores)
	return artifact
}

func offlineUpgradeDifference(left, right []string) []string {
	rightSet := make(map[string]struct{}, len(right))
	for _, value := range right {
		rightSet[value] = struct{}{}
	}
	var difference []string
	for _, value := range left {
		if _, ok := rightSet[value]; !ok {
			difference = append(difference, value)
		}
	}
	sort.Strings(difference)
	return difference
}
