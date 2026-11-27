package cosmoslogical

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-db/common/keys"
	"github.com/sei-protocol/sei-chain/sei-db/common/utils"
	"github.com/sei-protocol/sei-chain/sei-db/config"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/composite"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv"
	flatkvconfig "github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/config"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/memiavl"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/migration"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/types"
)

func init() { progressOut = io.Discard }

// writtenModules are the Cosmos modules the test workload writes. The other Cosmos modules stay
// empty, so they digest to a zero count.
var writtenModules = []string{keys.AuthStoreKey, keys.BankStoreKey, keys.DistributionStoreKey, keys.StakingStoreKey}

func testMemiavlConfig() memiavl.Config {
	cfg := memiavl.DefaultConfig()
	cfg.AsyncCommitBuffer = 0
	cfg.SnapshotKeepRecent = 1000
	return cfg
}

func newTestMemiavlStore(t *testing.T, home string) *memiavl.CommitStore {
	t.Helper()
	store := memiavl.NewCommitStore(home, testMemiavlConfig())
	store.Initialize(keys.MemIAVLStoreKeys)
	_, err := store.LoadVersion(0, false)
	require.NoError(t, err)
	return store
}

const (
	workloadKeySpace = 48
	cosmosBatchSize  = 7
)

// workload writes the same deterministic blocks to every store it is given.
type workload struct {
	rng *rand.Rand
	// live is the set of keys each written module holds.
	live map[string]map[int]bool
}

func newWorkload(seed int64) *workload {
	w := &workload{rng: rand.New(rand.NewSource(seed)), live: map[string]map[int]bool{}} //nolint:gosec
	for _, m := range writtenModules {
		w.live[m] = map[int]bool{}
	}
	return w
}

func workloadKey(i int) []byte { return []byte{0x02, byte(i >> 8), byte(i)} }

// genesisBlock fills the lower half of every written module's key space, so that keys the later
// blocks create for the first time land right of an in-flight boundary.
func (w *workload) genesisBlock() []*proto.NamedChangeSet {
	var out []*proto.NamedChangeSet
	for _, m := range writtenModules {
		var pairs []*proto.KVPair
		for i := 0; i < workloadKeySpace/2; i++ {
			pairs = append(pairs, &proto.KVPair{Key: workloadKey(i), Value: []byte(fmt.Sprintf("%s-genesis-%d", m, i))})
			w.live[m][i] = true
		}
		out = append(out, &proto.NamedChangeSet{Name: m, Changeset: proto.ChangeSet{Pairs: pairs}})
	}
	return append(out, w.evmChangeSet(0))
}

// block writes, overwrites, creates, and deletes keys over the whole key space of every written
// module.
func (w *workload) block(height int64) []*proto.NamedChangeSet {
	var out []*proto.NamedChangeSet
	for _, m := range writtenModules {
		byKey := map[int]*proto.KVPair{}
		for n := 0; n < 4; n++ {
			i := w.rng.Intn(workloadKeySpace)
			byKey[i] = &proto.KVPair{Key: workloadKey(i), Value: []byte(fmt.Sprintf("%s-h%d-%d", m, height, i))}
			w.live[m][i] = true
		}
		i := w.rng.Intn(workloadKeySpace)
		if w.live[m][i] {
			byKey[i] = &proto.KVPair{Key: workloadKey(i), Delete: true}
			delete(w.live[m], i)
		}
		idx := make([]int, 0, len(byKey))
		for i := range byKey {
			idx = append(idx, i)
		}
		sort.Ints(idx)
		pairs := make([]*proto.KVPair, 0, len(idx))
		for _, i := range idx {
			pairs = append(pairs, byKey[i])
		}
		out = append(out, &proto.NamedChangeSet{Name: m, Changeset: proto.ChangeSet{Pairs: pairs}})
	}
	return append(out, w.evmChangeSet(height))
}

func (w *workload) evmChangeSet(height int64) *proto.NamedChangeSet {
	addr := bytes.Repeat([]byte{byte(w.rng.Intn(4) + 1)}, keys.AddressLen)
	nonce := make([]byte, 8)
	nonce[7] = byte(height)
	return &proto.NamedChangeSet{Name: keys.EVMStoreKey, Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{
		{Key: keys.BuildEVMKey(keys.EVMKeyNonce, addr), Value: nonce},
	}}}
}

func compositeAutoConfig() config.StateCommitConfig {
	cfg := config.DefaultStateCommitConfig()
	cfg.WriteMode = types.Auto
	cfg.MemIAVLConfig = testMemiavlConfig()
	return cfg
}

// migrationFixture is a memiavl-only reference node and an auto node that ran migrate_evm and then
// migrate_cosmos, both given the same blocks.
type migrationFixture struct {
	referenceDir string
	memiavlDir   string
	flatkvDir    string
	// cosmosStart is the last height before migrate_cosmos moved its first batch.
	cosmosStart int64
	// cosmosDone is the height at which migrate_cosmos recorded completion.
	cosmosDone int64
	tip        int64
}

func buildMigrationFixture(t *testing.T) migrationFixture {
	t.Helper()
	refHome, home := t.TempDir(), t.TempDir()

	ref := newTestMemiavlStore(t, refHome)
	openComposite := func(batch int) *composite.CompositeCommitStore {
		cs, err := composite.NewCompositeCommitStore(context.Background(), home, compositeAutoConfig())
		require.NoError(t, err)
		require.NoError(t, cs.SetMigrationBatchSize(batch))
		require.NoError(t, cs.Initialize(keys.MemIAVLStoreKeys))
		require.NoError(t, cs.LoadLatest())
		return cs
	}
	cs := openComposite(0)
	// snapshotBoth writes a memiavl snapshot at the current height on both nodes. The composite
	// store has no snapshot call, so its memiavl dir is opened alone while the store is closed.
	snapshotBoth := func(batch int) {
		require.NoError(t, ref.GetDB().RewriteSnapshot(context.Background()))
		require.NoError(t, cs.Close())
		mem := memiavl.NewCommitStore(home, testMemiavlConfig())
		_, err := mem.LoadVersion(0, false)
		require.NoError(t, err)
		require.NoError(t, mem.GetDB().RewriteSnapshot(context.Background()))
		require.NoError(t, mem.Close())
		cs = openComposite(batch)
	}

	w := newWorkload(0xC05)
	height := int64(0)
	commit := func(changesets []*proto.NamedChangeSet) {
		height++
		require.NoError(t, ref.ApplyChangeSets(changesets))
		_, err := ref.Commit(height)
		require.NoError(t, err)
		require.NoError(t, cs.ApplyChangeSets(changesets))
		_, err = cs.Commit(height)
		require.NoError(t, err)
	}
	// SetWriteMode refuses to leave a migration mode until its migration is complete.
	commitUntilModeAllowed := func(next types.WriteMode, maxBlocks int64) {
		for start := height; cs.SetWriteMode(next) != nil; {
			require.Less(t, height-start, maxBlocks, "migration to %s did not finish", next)
			commit(w.block(height + 1))
		}
	}

	commit(w.genesisBlock())
	commit(w.block(height + 1))
	snapshotBoth(0)

	require.NoError(t, cs.SetMigrationBatchSize(1000))
	require.NoError(t, cs.SetWriteMode(types.MigrateEVM))
	commitUntilModeAllowed(types.EVMMigrated, 20)
	commit(w.block(height + 1))

	fx := migrationFixture{referenceDir: utils.GetCosmosSCStorePath(refHome), cosmosStart: height}
	require.NoError(t, cs.SetMigrationBatchSize(cosmosBatchSize))
	require.NoError(t, cs.SetWriteMode(types.MigrateCosmos))
	commit(w.block(height + 1))
	commit(w.block(height + 1))
	snapshotBoth(cosmosBatchSize)
	require.Equal(t, types.MigrateCosmos, cs.GetWriteMode(), "the reopened node must resume migrate_cosmos")
	commitUntilModeAllowed(types.FlatKVOnly, 100)
	fx.cosmosDone = height
	commit(w.block(height + 1))
	commit(w.block(height + 1))
	fx.tip = height

	require.NoError(t, ref.Close())
	require.NoError(t, cs.Close())
	fx.memiavlDir = utils.GetCosmosSCStorePath(home)
	fx.flatkvDir = utils.GetFlatKVPath(home)
	return fx
}

// runDigest runs the command with flags and returns its JSON report and error.
func runDigest(t *testing.T, flags map[string]string) (report, error) {
	t.Helper()
	cmd := CosmosLogicalDigestCmd()
	for name, value := range flags {
		require.NoError(t, cmd.Flags().Set(name, value), "flag %s", name)
	}
	require.NoError(t, cmd.Flags().Set("json", "true"))
	var out bytes.Buffer
	cmd.SetOut(&out)
	runErr := runCosmosLogicalDigest(cmd, nil)
	var r report
	if out.Len() > 0 {
		require.Equal(t, 1, bytes.Count(out.Bytes(), []byte("\n")), "a report is one line: %s", out.String())
		require.NoError(t, json.Unmarshal(out.Bytes(), &r))
	}
	return r, runErr
}

func requireDigest(t *testing.T, flags map[string]string) report {
	t.Helper()
	r, err := runDigest(t, flags)
	require.NoError(t, err, "flags %v", flags)
	return r
}

func referenceFlags(fx migrationFixture, height int64, mode string) map[string]string {
	return map[string]string{"backend": backendMemiavl, "db-dir": fx.referenceDir,
		"height": strconv.FormatInt(height, 10), "memiavl-open-mode": mode}
}

func compositeFlags(fx migrationFixture, height int64, mode string) map[string]string {
	return map[string]string{"backend": backendComposite, "flatkv-dir": fx.flatkvDir, "memiavl-dir": fx.memiavlDir,
		"height": strconv.FormatInt(height, 10), "memiavl-open-mode": mode}
}

func flatKVFlags(fx migrationFixture, height int64) map[string]string {
	return map[string]string{"backend": backendFlatKV, "db-dir": fx.flatkvDir, "height": strconv.FormatInt(height, 10)}
}

// TestCompositeDigestMatchesReferenceAtEveryMigrationHeight requires a node in migrate_cosmos to
// digest, at every height, to the same per-module state as a node that never migrated.
func TestCompositeDigestMatchesReferenceAtEveryMigrationHeight(t *testing.T) {
	fx := buildMigrationFixture(t)
	require.Greater(t, fx.cosmosDone-fx.cosmosStart, int64(3), "the migration must span several blocks")

	sawBoundaryInsideModule, sawSnapshot := false, false
	for h := fx.cosmosStart; h <= fx.tip; h++ {
		want := requireDigest(t, referenceFlags(fx, h, openModeReplay))
		require.Equal(t, h, *want.MemiavlVersion)
		modes := []string{openModeReplay, openModeChangelog}
		if hasSnapshot(fx.memiavlDir, h) && hasSnapshot(fx.referenceDir, h) {
			sawSnapshot = true
			modes = append(modes, openModeSnapshot)
			require.Equal(t, want.Modules, requireDigest(t, referenceFlags(fx, h, openModeSnapshot)).Modules, "height %d", h)
		}
		for _, mode := range modes {
			got := requireDigest(t, compositeFlags(fx, h, mode))
			require.Equal(t, want.Modules, got.Modules, "height %d mode %s boundary %s", h, mode, got.MigrationBoundary)
			require.Equal(t, want.Final, got.Final, "height %d mode %s", h, mode)
			require.Equal(t, h, *got.FlatKVVersion)
		}
		if b := boundaryAt(t, fx, h); b.Status() == migration.MigrationInProgress && slices.Contains(writtenModules, b.ModuleName()) {
			sawBoundaryInsideModule = true
		}
	}
	require.True(t, sawBoundaryInsideModule, "no checked height had its boundary inside a written module")
	require.True(t, sawSnapshot, "no checked height had a memiavl snapshot on both nodes")

	for _, m := range requireDigest(t, referenceFlags(fx, fx.tip, openModeReplay)).Modules {
		if slices.Contains(writtenModules, m.Module) {
			require.NotZero(t, m.Count, "module %s", m.Module)
		} else {
			require.Zero(t, m.Count, "module %s", m.Module)
		}
	}
}

// TestFlatKVDigestMatchesReferenceAfterMigration requires the FlatKV store alone to hold the whole
// Cosmos state once migrate_cosmos is complete.
func TestFlatKVDigestMatchesReferenceAfterMigration(t *testing.T) {
	fx := buildMigrationFixture(t)
	for h := fx.cosmosDone; h <= fx.tip; h++ {
		want := requireDigest(t, referenceFlags(fx, h, openModeReplay))
		got := requireDigest(t, flatKVFlags(fx, h))
		require.Equal(t, want.Modules, got.Modules, "height %d", h)
		require.Equal(t, migration.MigrationBoundaryComplete.String(), got.MigrationBoundary)
		require.Equal(t, int64(migration.Version2_FlatKVOnly), *got.MigrationVersion)
	}
	// Before completion FlatKV holds only part of the state.
	partial := requireDigest(t, flatKVFlags(fx, fx.cosmosStart+2))
	full := requireDigest(t, referenceFlags(fx, fx.cosmosStart+2, openModeReplay))
	require.Less(t, partial.Final.Count, full.Final.Count)
}

// TestDigestSeesOneChangedValue requires a single overwritten value to change the module digest and
// leave every other module's digest alone.
func TestDigestSeesOneChangedValue(t *testing.T) {
	refHome := t.TempDir()
	write := func(store *memiavl.CommitStore, version int64, value string) {
		require.NoError(t, store.ApplyChangeSets([]*proto.NamedChangeSet{
			{Name: keys.BankStoreKey, Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{
				{Key: []byte("a"), Value: []byte("1")}, {Key: []byte("b"), Value: []byte(value)},
			}}},
			{Name: keys.StakingStoreKey, Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{{Key: []byte("a"), Value: []byte("1")}}}},
		}))
		_, err := store.Commit(version)
		require.NoError(t, err)
	}
	store := newTestMemiavlStore(t, refHome)
	write(store, 1, "1")
	write(store, 2, "1")
	require.NoError(t, store.GetDB().RewriteSnapshot(context.Background()))
	write(store, 3, "2")
	write(store, 4, "3")
	require.NoError(t, store.Close())

	dir := utils.GetCosmosSCStorePath(refHome)
	at := func(h int64) map[string]moduleReport {
		r := requireDigest(t, map[string]string{"backend": backendMemiavl, "db-dir": dir,
			"height": strconv.FormatInt(h, 10), "memiavl-open-mode": openModeChangelog})
		out := map[string]moduleReport{}
		for _, m := range r.Modules {
			out[m.Module] = m
		}
		return out
	}
	one, two := at(3), at(4)
	require.NotEqual(t, one[keys.BankStoreKey].Digest, two[keys.BankStoreKey].Digest)
	require.Equal(t, one[keys.BankStoreKey].Count, two[keys.BankStoreKey].Count)
	require.Equal(t, one[keys.StakingStoreKey], two[keys.StakingStoreKey])
}

// TestCompositeDigestFailsOnRowsThatMigrationMustHaveMoved builds a composite pair whose memiavl
// still holds a migrated key and whose FlatKV holds an unmigrated key memiavl also holds, and
// requires both to be counted and the command to exit with an error.
func TestCompositeDigestFailsOnRowsThatMigrationMustHaveMoved(t *testing.T) {
	memHome := t.TempDir()
	mem := newTestMemiavlStore(t, memHome)
	require.NoError(t, mem.ApplyChangeSets([]*proto.NamedChangeSet{{Name: keys.BankStoreKey, Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{
		{Key: []byte("a"), Value: []byte("left")}, {Key: []byte("x"), Value: []byte("right-old")},
	}}}}))
	_, err := mem.Commit(1)
	require.NoError(t, err)
	require.NoError(t, mem.Close())

	cfg := flatkvconfig.DefaultTestConfig(t)
	wal, err := flatkv.OpenStateWAL(cfg)
	require.NoError(t, err)
	flat, err := flatkv.NewCommitStore(context.Background(), cfg, wal)
	require.NoError(t, err)
	require.NoError(t, flat.LoadLatest())
	boundary := migration.NewMigrationBoundary(keys.BankStoreKey, []byte("m"))
	require.NoError(t, flat.ApplyChangeSets(1, []*proto.NamedChangeSet{
		{Name: keys.BankStoreKey, Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{
			{Key: []byte("a"), Value: []byte("left")}, {Key: []byte("x"), Value: []byte("right-new")},
		}}},
		{Name: migration.MigrationStore, Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{
			{Key: []byte(migration.MigrationBoundaryKey), Value: boundary.Serialize()},
		}}},
	}))
	_, err = flat.Commit(1)
	require.NoError(t, err)
	require.NoError(t, flat.FlushSnapshots())
	require.NoError(t, flat.Close())

	r, err := runDigest(t, map[string]string{"backend": backendComposite, "flatkv-dir": cfg.DataDir,
		"memiavl-dir": utils.GetCosmosSCStorePath(memHome), "height": "1", "memiavl-open-mode": openModeReplay})
	require.ErrorIs(t, err, errIntegrity)
	require.Equal(t, uint64(1), r.Integrity.MemiavlRowsLeftOfBoundary)
	require.Equal(t, uint64(1), r.Integrity.KeysInBothStores)
	for _, m := range r.Modules {
		if m.Module == keys.BankStoreKey {
			require.Equal(t, uint64(2), m.Count, "a counted once, x from memiavl")
		}
	}
}

// TestListModeListsTheRowsOfOneModule requires list mode to print the rows it digests, filtered by
// key prefix and cut at the limit.
func TestListModeListsTheRowsOfOneModule(t *testing.T) {
	fx := buildMigrationFixture(t)
	h := fx.cosmosStart + 2
	flags := compositeFlags(fx, h, openModeReplay)
	flags["module"] = keys.BankStoreKey
	flags["list"] = "true"
	flags["list-limit"] = "0"
	all := requireDigest(t, flags)
	require.Equal(t, all.Modules[0].Count, *all.ListMatched)
	require.Len(t, all.Entries, int(all.Modules[0].Count))
	require.True(t, sort.SliceIsSorted(all.Entries, func(i, j int) bool { return all.Entries[i].Key < all.Entries[j].Key }))

	ref := referenceFlags(fx, h, openModeReplay)
	for k, v := range map[string]string{"module": keys.BankStoreKey, "list": "true", "list-limit": "0"} {
		ref[k] = v
	}
	require.Equal(t, requireDigest(t, ref).Entries, all.Entries)

	flags["key-prefix"] = hex.EncodeToString(workloadKey(1)[:2])
	flags["list-limit"] = "2"
	cut := requireDigest(t, flags)
	require.Len(t, cut.Entries, 2)
	require.Greater(t, *cut.ListMatched, uint64(2))
	require.Equal(t, all.Modules, cut.Modules, "list filters do not change the digest")
}

func TestParseOptionsRefusesBadFlags(t *testing.T) {
	for name, flags := range map[string]map[string]string{
		"no backend":          {"db-dir": "x"},
		"memiavl without dir": {"backend": backendMemiavl},
		"composite half":      {"backend": backendComposite, "flatkv-dir": "x"},
		"evm module":          {"backend": backendFlatKV, "db-dir": "x", "module": keys.EVMStoreKey},
		"list two modules":    {"backend": backendFlatKV, "db-dir": "x", "list": "true", "module": "bank,acc"},
		"prefix without list": {"backend": backendFlatKV, "db-dir": "x", "key-prefix": "01"},
		"bad prefix":          {"backend": backendFlatKV, "db-dir": "x", "list": "true", "module": "bank", "key-prefix": "zz"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := runDigest(t, flags)
			require.Error(t, err)
		})
	}
}

// TestEntryHashIsPinned pins the unit every digest is built from, so that digests taken by two
// builds stay comparable.
func TestEntryHashIsPinned(t *testing.T) {
	sum := entryHash("bank", []byte{0x02, 0x01}, []byte("v"))
	require.Equal(t, "987938A629DD19B037AA9D510FF4E9F76EEC10CE7486A4E1D61EC79D1873963F", fmt.Sprintf("%X", sum))
}

func hasSnapshot(memiavlDir string, h int64) bool {
	_, err := os.Stat(filepath.Join(memiavlDir, fmt.Sprintf("%s%020d", memiavl.SnapshotPrefix, h)))
	return err == nil
}

func boundaryAt(t *testing.T, fx migrationFixture, h int64) migration.MigrationBoundary {
	t.Helper()
	src, err := openFlatKVSource(fx.flatkvDir, h)
	require.NoError(t, err)
	defer src.close()
	return src.state.boundary
}
