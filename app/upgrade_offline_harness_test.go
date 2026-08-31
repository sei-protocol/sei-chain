//go:build offline_upgrade

package app

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"

	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/config"
	tmproto "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"
	"github.com/sei-protocol/sei-chain/sei-wasmd/x/wasm"
	evmtypes "github.com/sei-protocol/sei-chain/x/evm/types"
	"github.com/stretchr/testify/require"
	dbm "github.com/tendermint/tm-db"
)

const (
	offlineUpgradeArtifactEnv = "UPGRADE_TEST_ARTIFACT"
	offlineUpgradeChainID     = "sei-test"
	offlineUpgradePhaseEnv    = "UPGRADE_TEST_PHASE"
)

type offlineUpgradeArtifact struct {
	Upgrade        string                       `json:"upgrade"`
	SourceHeight   int64                        `json:"source_height"`
	UpgradeHeight  int64                        `json:"upgrade_height"`
	ModuleVersions []string                     `json:"module_versions"`
	Stores         map[string]map[string]string `json:"stores"`
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
	if initialize {
		initializeOfflineUpgradeApp(t, testApp, genesisStateBytes)
	}
	return testApp
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

func commitOfflineUpgradeApp(t *testing.T, testApp *App) {
	t.Helper()
	_, err := testApp.Commit(context.Background())
	require.NoError(t, err)
}

func offlineUpgradeReadContext(testApp *App, height int64) sdk.Context {
	return testApp.NewUncachedContext(false, tmproto.Header{
		ChainID: offlineUpgradeChainID,
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
		key := base64.StdEncoding.EncodeToString(iterator.Key())
		entries[key] = base64.StdEncoding.EncodeToString(iterator.Value())
	}
	return entries
}

func offlineUpgradeModuleVersions(testApp *App, ctx sdk.Context) []string {
	versionMap := testApp.UpgradeKeeper.GetModuleVersionMap(ctx)
	names := make([]string, 0, len(versionMap))
	for name := range versionMap {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
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

func seedOfflineUpgradeStores(
	t *testing.T,
	testApp *App,
	ctx sdk.Context,
	storeNames []string,
) map[string]map[string]string {
	t.Helper()
	for _, storeName := range storeNames {
		storeKey := testApp.GetKey(storeName)
		require.NotNil(t, storeKey, "%s store is not mounted", storeName)
		ctx.KVStore(storeKey).Set(
			[]byte("offline-upgrade"),
			[]byte(fmt.Sprintf("retained/%s", storeName)),
		)
	}

	stores := make(map[string]map[string]string, len(storeNames))
	for _, storeName := range storeNames {
		stores[storeName] = snapshotOfflineUpgradeStore(t, testApp, ctx, storeName)
	}
	return stores
}
