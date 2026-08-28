package flatkv

import (
	"flag"
	"fmt"
	"hash/fnv"
	"math/rand"
	"sort"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-db/common/keys"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/config"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/ktype"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/lthash"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/vtype"
)

// agreementSeedFlag overrides the deterministic seed for the agreement suite when non-zero.
var agreementSeedFlag = flag.Int64("lthash-agreement-seed", 0,
	"seed for the lattice-hash agreement suite (0 = derive from test name)")

// agreementSeed derives a stable seed from the test's label so a green run is reproducible and a red
// one names the seed that produced it.
func agreementSeed(t *testing.T, label string) int64 {
	t.Helper()
	if *agreementSeedFlag != 0 {
		t.Logf("lthash agreement seed=%d (from -lthash-agreement-seed)", *agreementSeedFlag)
		return *agreementSeedFlag
	}
	h := fnv.New64a()
	_, _ = h.Write([]byte(label))
	seed := int64(h.Sum64()) //nolint:gosec // deterministic test data only
	t.Logf("lthash agreement seed=%d (reproduce with -lthash-agreement-seed=%d)", seed, seed)
	return seed
}

// =============================================================================
// The model
// =============================================================================

// The model is an independent account of which rows flatKV should hold and what logical fields each
// one carries. It derives that from the changeset stream alone — it never reads the store — which is
// what makes it a third opinion rather than another disk scanner: a bug that writes the wrong row is
// invisible to the maintained hash and to a full rescan alike, because both describe what is on disk.
//
// It deliberately performs the account field merge itself, by plain assignment, rather than calling
// PendingAccountWrite.Merge. That merge is the code under test. Serialization and hashing are reused
// from vtype and lthash: those are mechanical, separately unit-tested, and reproducing them would
// test the test.

// modelAccount is one merged account row's logical fields. Nonce, codehash and balance share a single
// physical row, so partial writes fold onto whatever this already holds.
type modelAccount struct {
	balance     vtype.Balance
	nonce       uint64
	codeHash    vtype.CodeHash
	blockHeight int64
}

// isEmpty reports whether every hashed field is zero, which is what makes the row a deletion.
func (a modelAccount) isEmpty() bool {
	if a.nonce != 0 {
		return false
	}
	for _, b := range a.balance {
		if b != 0 {
			return false
		}
	}
	for _, b := range a.codeHash {
		if b != 0 {
			return false
		}
	}
	return true
}

// modelStorage is one storage slot's logical value.
type modelStorage struct {
	value       [32]byte
	blockHeight int64
}

// modelBytes is one code or misc row's logical value.
type modelBytes struct {
	value       []byte
	blockHeight int64
}

// stateModel tracks the rows flatKV should hold, keyed by physical key, one map per database.
type stateModel struct {
	accounts map[string]modelAccount
	storage  map[string]modelStorage
	code     map[string]modelBytes
	misc     map[string]modelBytes

	// coverage counts what the run exercised rather than what survived it. A row written and later
	// deleted still exercised its database, so measuring the terminal state would understate the
	// workload and fail on a seed that happens to end with a database empty.
	coverage agreementCoverage
}

// agreementCoverage records which shapes a run actually produced.
//
// Creates, updates and deletes are counted separately per database because each is a distinct risk: a
// create has no prior value to mix out, an update must mix out exactly the value it replaces, and a
// delete must mix out the value and leave no row behind. A workload heavy in creates can look busy
// while barely exercising the other two.
//
// absentDeletes counts deletes aimed at a key that was not there. That is a legal operation and must
// be exercised deliberately, not merely tolerated: it has to be a strict no-op on every hash, and a
// bug that mixed something out for it would corrupt the root while looking like ordinary churn.
type agreementCoverage struct {
	rowsWritten      map[string]int
	creates          map[string]int
	updates          map[string]int
	deletes          map[string]int
	absentDeletes    map[string]int
	contractAccounts int
	eoaAccounts      int
	miscModules      map[string]bool

	// accountShapes counts account writes by which fields the write carried and whether a row was
	// already there, keyed "create/nonce-only", "update/both" and so on.
	//
	// Nonce and codehash are separate logical keys folded into one physical row, so a write carrying
	// only one of them must preserve the other. That is the merge, and it is only exercised by a
	// single-field write onto a row that already exists — a run made entirely of two-field writes would
	// never touch it while looking like thorough account coverage.
	accountShapes map[string]int
}

// accountWriteShape names an account write by the fields it carried and whether it landed on an
// existing row.
func accountWriteShape(nonceSet bool, codeHashSet bool, existed bool) string {
	kind := "create"
	if existed {
		kind = "update"
	}
	switch {
	case nonceSet && codeHashSet:
		return kind + "/both"
	case codeHashSet:
		return kind + "/codehash-only"
	default:
		return kind + "/nonce-only"
	}
}

// accountShapeNames lists every shape the workload must produce.
var accountShapeNames = []string{
	"create/nonce-only", "create/codehash-only", "create/both",
	"update/nonce-only", "update/codehash-only", "update/both",
}

// recordWrite counts a row write against the database it landed in, splitting create from update.
func (c *agreementCoverage) recordWrite(dir string, existed bool) {
	c.rowsWritten[dir]++
	if existed {
		c.updates[dir]++
		return
	}
	c.creates[dir]++
}

// recordRemoval counts a removal, distinguishing one that removed a row from one aimed at an absent
// key. Both are wanted; they are different tests.
func (c *agreementCoverage) recordRemoval(dir string, existed bool) {
	if existed {
		c.deletes[dir]++
		return
	}
	c.absentDeletes[dir]++
}

func newStateModel() *stateModel {
	return &stateModel{
		accounts: make(map[string]modelAccount),
		storage:  make(map[string]modelStorage),
		code:     make(map[string]modelBytes),
		misc:     make(map[string]modelBytes),
		coverage: agreementCoverage{
			rowsWritten:   make(map[string]int),
			creates:       make(map[string]int),
			updates:       make(map[string]int),
			deletes:       make(map[string]int),
			absentDeletes: make(map[string]int),
			miscModules:   make(map[string]bool),
			accountShapes: make(map[string]int),
		},
	}
}

// accountEdit is the pending nonce/codehash write for one address within a single apply call.
type accountEdit struct {
	nonce       uint64
	nonceSet    bool
	codeHash    vtype.CodeHash
	codeHashSet bool
}

// apply folds one ApplyChangeSets call into the model at blockHeight.
//
// Several calls may share a height, in which case each folds onto the previous one's result — which is
// what the store does too, since it re-reads accounts live rather than from the committed version.
func (m *stateModel) apply(t *testing.T, blockHeight int64, changeSets []*proto.NamedChangeSet) {
	t.Helper()

	// Within one call the last write for a physical key wins, matching classifyAndPrefix's per-kind
	// maps. Accounts additionally accumulate across the two logical keys that share their row.
	accountEdits := make(map[string]*accountEdit)
	storage := make(map[string][]byte)
	code := make(map[string][]byte)
	misc := make(map[string][]byte)
	deleted := make(map[string]bool)

	for _, cs := range changeSets {
		if cs == nil || len(cs.Changeset.Pairs) == 0 {
			continue
		}
		for _, pair := range cs.Changeset.Pairs {
			if cs.Name != keys.EVMStoreKey {
				physKey := string(ktype.ModulePhysicalKey(cs.Name, pair.Key))
				misc[physKey] = pair.Value
				deleted[physKey] = pair.Delete
				continue
			}

			kind, stripped := keys.ParseEVMKey(pair.Key)
			require.NotEqual(t, keys.EVMKeyEmpty, kind, "model: empty EVM key in changeset")

			if kind == keys.EVMKeyMisc {
				physKey := string(ktype.ModulePhysicalKey(keys.EVMStoreKey, pair.Key))
				misc[physKey] = pair.Value
				deleted[physKey] = pair.Delete
				continue
			}

			physKey := string(ktype.EVMPhysicalKey(kind, stripped))
			switch kind {
			case keys.EVMKeyNonce, keys.EVMKeyCodeHash:
				edit := accountEdits[physKey]
				if edit == nil {
					edit = &accountEdit{}
					accountEdits[physKey] = edit
				}
				if kind == keys.EVMKeyNonce {
					edit.nonceSet = true
					if pair.Delete {
						// Deleting a nonce sets it to zero; it does not remove the row unless
						// every other field is already zero.
						edit.nonce = 0
					} else {
						nonce, err := vtype.ParseNonce(pair.Value)
						require.NoError(t, err, "model: nonce value")
						edit.nonce = nonce
					}
				} else {
					edit.codeHashSet = true
					if pair.Delete {
						edit.codeHash = vtype.CodeHash{}
					} else {
						ch, err := vtype.ParseCodeHash(pair.Value)
						require.NoError(t, err, "model: codehash value")
						edit.codeHash = *ch
					}
				}
			case keys.EVMKeyCode:
				code[physKey] = pair.Value
				deleted[physKey] = pair.Delete
			case keys.EVMKeyStorage:
				storage[physKey] = pair.Value
				deleted[physKey] = pair.Delete
			default:
				t.Fatalf("model: unhandled EVM key kind %v", kind)
			}
		}
	}

	for physKey, edit := range accountEdits {
		row, existed := m.accounts[physKey]
		if edit.nonceSet {
			row.nonce = edit.nonce
		}
		if edit.codeHashSet {
			row.codeHash = edit.codeHash
		}
		row.blockHeight = blockHeight
		if row.isEmpty() {
			// Every hashed field is zero, so the row is garbage-collected rather than stored.
			delete(m.accounts, physKey)
			m.coverage.recordRemoval(accountDBDir, existed)
			continue
		}
		m.accounts[physKey] = row
		m.coverage.recordWrite(accountDBDir, existed)
		m.coverage.accountShapes[accountWriteShape(edit.nonceSet, edit.codeHashSet, existed)]++
		if row.codeHash == (vtype.CodeHash{}) {
			m.coverage.eoaAccounts++
		} else {
			m.coverage.contractAccounts++
		}
	}

	for physKey, raw := range storage {
		_, existed := m.storage[physKey]
		if deleted[physKey] {
			delete(m.storage, physKey)
			m.coverage.recordRemoval(storageDBDir, existed)
			continue
		}
		value, err := vtype.ParseStorageValue(raw)
		require.NoError(t, err, "model: storage value")
		if *value == ([32]byte{}) {
			// An all-zero storage value is indistinguishable from a delete once stored.
			delete(m.storage, physKey)
			m.coverage.recordRemoval(storageDBDir, existed)
			continue
		}
		m.storage[physKey] = modelStorage{value: *value, blockHeight: blockHeight}
		m.coverage.recordWrite(storageDBDir, existed)
	}

	for physKey, raw := range code {
		_, existed := m.code[physKey]
		if deleted[physKey] || len(raw) == 0 {
			// Empty bytecode is a deletion; there is no such thing as a live zero-length code row.
			delete(m.code, physKey)
			m.coverage.recordRemoval(codeDBDir, existed)
			continue
		}
		m.code[physKey] = modelBytes{value: append([]byte(nil), raw...), blockHeight: blockHeight}
		m.coverage.recordWrite(codeDBDir, existed)
	}

	for physKey, raw := range misc {
		_, existed := m.misc[physKey]
		if deleted[physKey] {
			delete(m.misc, physKey)
			m.coverage.recordRemoval(miscDBDir, existed)
			continue
		}
		// A zero-length misc value is a live row, not a deletion — Cosmos modules store them.
		m.misc[physKey] = modelBytes{value: append([]byte(nil), raw...), blockHeight: blockHeight}
		m.coverage.recordWrite(miscDBDir, existed)
		if module, _, err := ktype.StripModulePrefix([]byte(physKey)); err == nil {
			m.coverage.miscModules[module] = true
		}
	}
}

// rowsByDB returns the physical rows the model expects, serialized exactly as they should appear on
// disk, keyed by database directory and then by physical key.
func (m *stateModel) rowsByDB() map[string]map[string][]byte {
	out := map[string]map[string][]byte{
		accountDBDir: make(map[string][]byte, len(m.accounts)),
		storageDBDir: make(map[string][]byte, len(m.storage)),
		codeDBDir:    make(map[string][]byte, len(m.code)),
		miscDBDir:    make(map[string][]byte, len(m.misc)),
	}
	for physKey, row := range m.accounts {
		balance, codeHash := row.balance, row.codeHash
		out[accountDBDir][physKey] = vtype.NewAccountData().
			SetBlockHeight(row.blockHeight).
			SetBalance(&balance).
			SetNonce(row.nonce).
			SetCodeHash(&codeHash).
			Serialize()
	}
	for physKey, row := range m.storage {
		value := row.value
		out[storageDBDir][physKey] = vtype.NewStorageData().
			SetBlockHeight(row.blockHeight).
			SetValue(&value).
			Serialize()
	}
	for physKey, row := range m.code {
		out[codeDBDir][physKey] = vtype.NewCodeData().
			SetBlockHeight(row.blockHeight).
			SetBytecode(row.value).
			Serialize()
	}
	for physKey, row := range m.misc {
		out[miscDBDir][physKey] = vtype.NewMiscData().
			SetBlockHeight(row.blockHeight).
			SetValue(row.value).
			Serialize()
	}
	return out
}

// expectedState is everything the model predicts about one committed version: the rows, the per-database
// roots, and the store-wide root.
//
// It is materialized once per assertion because serializing and hashing the whole state is O(state);
// recomputing it per database, as separate accessors would, dominated the suite's runtime once blocks
// carried hundreds of operations.
type expectedState struct {
	rowsByDB map[string]map[string][]byte
	rows     map[string][]byte
	perDB    map[string]*lthash.LtHash
	root     *lthash.LtHash
}

// expect materializes the model's prediction for the current version.
func (m *stateModel) expect() *expectedState {
	byDB := m.rowsByDB()
	out := &expectedState{
		rowsByDB: byDB,
		rows:     make(map[string][]byte, len(m.accounts)+len(m.storage)+len(m.code)+len(m.misc)),
		perDB:    make(map[string]*lthash.LtHash, len(dataDBDirs)),
		root:     lthash.New(),
	}
	for _, dir := range dataDBDirs {
		byKey := byDB[dir]
		pairs := make([]lthash.KVPairWithLastValue, 0, len(byKey))
		for physKey, value := range byKey {
			pairs = append(pairs, lthash.KVPairWithLastValue{Key: []byte(physKey), Value: value})
			out.rows[physKey] = value
		}
		root, _ := lthash.ComputeLtHash(nil, pairs)
		out.perDB[dir] = root
		out.root.MixIn(root)
	}
	return out
}

// =============================================================================
// Assertions
// =============================================================================

// requireModelAgrees checks the store's maintained hashes against the model, at the store-wide and
// per-database level, and checks that the store holds exactly the rows the model expects.
func requireModelAgrees(t *testing.T, s *CommitStore, m *stateModel, because string) {
	t.Helper()

	want := m.expect()
	for _, dir := range dataDBDirs {
		require.True(t, want.perDB[dir].Equal(s.perDBWorkingLtHash[dir]),
			"%s: %s per-DB root disagrees with the model\n  model: %x\n  store: %x",
			because, dir, want.perDB[dir].Checksum(), s.perDBWorkingLtHash[dir].Checksum())
	}
	require.True(t, want.root.Equal(s.workingLtHash),
		"%s: store-wide root disagrees with the model\n  model: %x\n  store: %x",
		because, want.root.Checksum(), s.workingLtHash.Checksum())

	requireRowsEqual(t, s, want, because)
}

// requireRowsEqual checks the store's committed rows against the model's, key by key.
//
// This is what catches a wrong row that hashes consistently: the maintained hash and a full rescan
// both describe whatever is on disk, so only an independently derived keyset can tell them apart.
func requireRowsEqual(t *testing.T, s *CommitStore, expected *expectedState, because string) {
	t.Helper()

	want := expected.rows
	got := make(map[string][]byte, len(want))

	iter, err := s.RawGlobalIterator()
	require.NoError(t, err, "%s: open raw global iterator", because)
	for ; iter.Valid(); iter.Next() {
		got[string(iter.Key())] = append([]byte(nil), iter.Value()...)
	}
	require.NoError(t, iter.Error(), "%s: iterate raw global", because)
	require.NoError(t, iter.Close())

	for _, physKey := range sortedKeys(want) {
		gotValue, ok := got[physKey]
		require.Truef(t, ok, "%s: model expects row %x but the store has none", because, physKey)
		require.Equalf(t, want[physKey], gotValue,
			"%s: row %x differs\n  model: %x\n  store: %x", because, physKey, want[physKey], gotValue)
	}
	for _, physKey := range sortedKeys(got) {
		_, ok := want[physKey]
		require.Truef(t, ok, "%s: store holds row %x that the model does not expect: %x",
			because, physKey, got[physKey])
	}
}

// requireStoresAgree checks that two stores report the same version and the same hashes at every level.
// storeComparator compares two stores repeatedly across a run.
//
// It remembers which equivalent-but-differently-shaped bookkeeping it has already reported, so a note
// that holds on every block is logged once instead of once per block.
type storeComparator struct {
	notedShapes map[string]bool
}

func newStoreComparator() *storeComparator {
	return &storeComparator{notedShapes: make(map[string]bool)}
}

func requireStoresAgree(t *testing.T, want *CommitStore, got *CommitStore, because string) {
	t.Helper()
	newStoreComparator().requireAgree(t, want, got, because)
}

func (sc *storeComparator) requireAgree(t *testing.T, want *CommitStore, got *CommitStore, because string) {
	t.Helper()

	wantHash, wantVersion := want.RootHash()
	gotHash, gotVersion := got.RootHash()
	require.Equalf(t, wantVersion, gotVersion, "%s: version", because)
	require.Equalf(t, wantHash, gotHash,
		"%s: store-wide root at version %d", because, wantVersion)

	for _, dir := range dataDBDirs {
		require.Truef(t, want.perDBWorkingLtHash[dir].Equal(got.perDBWorkingLtHash[dir]),
			"%s: %s per-DB root", because, dir)
	}
	sc.requireModuleBookkeepingAgrees(t, want, got, because)
}

// requireModuleBookkeepingAgrees compares the per-module hash and stats maps of two stores, reporting
// every discrepancy at once.
//
// An absent module entry counts as the identity, because that is what the surrounding code already
// treats it as: the per-DB root is the homomorphic sum of the entries, to which an identity entry and
// an absent one contribute alike. The two arise from the same state by different routes — a module a
// block touched but netted to nothing keeps a zeroed entry on the live path, whereas an import only
// sees surviving rows and so never learns that module existed. Presence differences are logged rather
// than failed, so they stay visible without asserting a parity the code does not promise.
func (sc *storeComparator) requireModuleBookkeepingAgrees(
	t *testing.T,
	want *CommitStore,
	got *CommitStore,
	because string,
) {
	t.Helper()

	var problems []string
	for _, dir := range dataDBDirs {
		modules := make(map[string]bool)
		for module := range want.perDBModuleWorkingLtHash[dir] {
			modules[module] = true
		}
		for module := range got.perDBModuleWorkingLtHash[dir] {
			modules[module] = true
		}
		for _, module := range sortedStrings(modules) {
			wantHash, wantOK := want.perDBModuleWorkingLtHash[dir][module]
			gotHash, gotOK := got.perDBModuleWorkingLtHash[dir][module]
			if wantOK != gotOK {
				present, absent := wantHash, "second"
				if !wantOK {
					present, absent = gotHash, "first"
				}
				if !present.IsZero() {
					problems = append(problems, fmt.Sprintf(
						"%s/%s: absent from the %s store but non-identity in the other (%s)",
						dir, module, absent, describeLtHash(present)))
					continue
				}
				note := fmt.Sprintf("%s/%s absent in the %s store, identity in the other", dir, module, absent)
				if !sc.notedShapes[note] {
					sc.notedShapes[note] = true
					t.Logf("note: %s — equivalent to the lattice sum, so not a failure (first seen at %s)",
						note, because)
				}
				continue
			}
			if wantOK && !wantHash.Equal(gotHash) {
				problems = append(problems, fmt.Sprintf("%s/%s: hashes differ (first %s, second %s)",
					dir, module, describeLtHash(wantHash), describeLtHash(gotHash)))
			}
		}

		wantStats := want.perDBModuleWorkingStats[dir]
		gotStats := got.perDBModuleWorkingStats[dir]
		statModules := make(map[string]bool)
		for module := range wantStats {
			statModules[module] = true
		}
		for module := range gotStats {
			statModules[module] = true
		}
		for _, module := range sortedStrings(statModules) {
			if wantStats[module] != gotStats[module] {
				problems = append(problems, fmt.Sprintf("%s/%s: stats differ (first %+v, second %+v)",
					dir, module, wantStats[module], gotStats[module]))
			}
		}
	}

	require.Emptyf(t, problems, "%s: per-module bookkeeping disagrees:\n  %s",
		because, joinLines(problems))
}

// describeLtHash renders a hash for an error message, naming the identity explicitly since a zeroed
// entry and an absent one are the case most often confused here.
func describeLtHash(h *lthash.LtHash) string {
	if h == nil {
		return "nil"
	}
	if h.IsZero() {
		return "identity"
	}
	sum := h.Checksum()
	return fmt.Sprintf("%x", sum[:8])
}

func sortedStrings(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for s := range set {
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

func joinLines(lines []string) string {
	out := ""
	for i, line := range lines {
		if i > 0 {
			out += "\n  "
		}
		out += line
	}
	return out
}

func sortedKeys(m map[string][]byte) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// =============================================================================
// Export / import
// =============================================================================

// importFromInto exports src at version and imports the stream into dst, returning how many rows
// crossed the boundary.
//
// The count is returned because AddNode silently drops any node whose version does not match the
// importer's: without asserting it, an empty destination would compare equal to nothing and the whole
// suite would pass vacuously.
func importFromInto(t *testing.T, src *CommitStore, dst *CommitStore, version int64) int {
	t.Helper()

	exp, err := src.Exporter(version)
	require.NoError(t, err, "export at version %d", version)
	nodes := drainExporter(t, exp)
	require.NoError(t, exp.Close())

	imp, err := dst.Importer(version)
	require.NoError(t, err, "importer at version %d", version)
	for _, node := range nodes {
		imp.AddNode(node)
	}
	require.NoError(t, imp.Close(), "import at version %d", version)
	require.Equal(t, version, dst.Version(), "imported store must land at the exported version")
	return len(nodes)
}

// =============================================================================
// Workload generator
// =============================================================================

// =============================================================================
// Workload generator
// =============================================================================

// orderedSet is a set with deterministic sampling.
//
// Sampling has to be reproducible from the seed, and ranging over a Go map is not: its iteration order
// is deliberately randomized. Keeping an explicit slice alongside the index makes selection a plain
// integer draw, and removal a swap with the tail so it stays O(1).
type orderedSet[K comparable] struct {
	items []K
	index map[K]int
}

func newOrderedSet[K comparable]() *orderedSet[K] {
	return &orderedSet[K]{index: make(map[K]int)}
}

func (s *orderedSet[K]) len() int { return len(s.items) }

func (s *orderedSet[K]) has(k K) bool {
	_, ok := s.index[k]
	return ok
}

func (s *orderedSet[K]) add(k K) {
	if s.has(k) {
		return
	}
	s.index[k] = len(s.items)
	s.items = append(s.items, k)
}

func (s *orderedSet[K]) remove(k K) {
	i, ok := s.index[k]
	if !ok {
		return
	}
	last := len(s.items) - 1
	s.items[i] = s.items[last]
	s.index[s.items[i]] = i
	s.items = s.items[:last]
	delete(s.index, k)
}

// sample returns a random member and true, or the zero value and false when empty.
func (s *orderedSet[K]) sample(rng *rand.Rand) (K, bool) {
	var zero K
	if len(s.items) == 0 {
		return zero, false
	}
	return s.items[rng.Intn(len(s.items))], true
}

// storageCoord and miscCoord name a row by its logical coordinates, which is what the generator picks.
type storageCoord struct {
	addr ktype.Address
	slot ktype.Slot
}

type miscCoord struct {
	module string
	key    string
}

// agreementWorkload generates randomized blocks with an explicit per-block budget of creates, updates,
// deletes of existing rows, and deletes of absent rows.
//
// It keeps its own view of which rows are live purely in order to aim: an update or a delete has to
// name a row that exists, or it silently becomes a create or a no-op and the category goes untested.
// That view is best-effort — the model, which derives state from the changesets alone, is what
// actually measures the outcome, so any drift here shows up as a coverage failure rather than as a
// false pass.
type agreementWorkload struct {
	rng *rand.Rand

	// Index-derived pools rather than the byte-indexed addrN/slotN helpers, which top out at 256
	// values — too few to absorb hundreds of operations per block.
	addrs       []ktype.Address
	slots       []ktype.Slot
	miscModules []string

	// Live rows, for aiming updates and deletes.
	liveAccounts *orderedSet[ktype.Address]
	liveCode     *orderedSet[ktype.Address]
	liveStorage  *orderedSet[storageCoord]
	liveMisc     *orderedSet[miscCoord]

	// Account field state, needed because an account row survives until every field is zero: deleting
	// only the nonce of an account that still has a codehash is an update, not a delete.
	accountNonce    map[ktype.Address]uint64
	accountCodeHash map[ktype.Address]bool
}

const (
	agreementAddressPool = 384
	agreementSlotPool    = 96
	agreementMiscKeyPool = 256
	agreementAimAttempts = 8
)

func newAgreementWorkload(rng *rand.Rand) *agreementWorkload {
	w := &agreementWorkload{
		rng:             rng,
		miscModules:     []string{keys.EVMStoreKey, "bank", "staking"},
		liveAccounts:    newOrderedSet[ktype.Address](),
		liveCode:        newOrderedSet[ktype.Address](),
		liveStorage:     newOrderedSet[storageCoord](),
		liveMisc:        newOrderedSet[miscCoord](),
		accountNonce:    make(map[ktype.Address]uint64),
		accountCodeHash: make(map[ktype.Address]bool),
	}
	for i := 0; i < agreementAddressPool; i++ {
		w.addrs = append(w.addrs, addrIndexed(i))
	}
	for i := 0; i < agreementSlotPool; i++ {
		w.slots = append(w.slots, slotIndexed(i))
	}
	return w
}

// addrIndexed builds a distinct address per index, well past the 256 that addrN can express.
func addrIndexed(i int) ktype.Address {
	var a ktype.Address
	a[16] = byte(i >> 24)
	a[17] = byte(i >> 16)
	a[18] = byte(i >> 8)
	a[19] = byte(i)
	return a
}

// slotIndexed builds a distinct storage slot per index.
func slotIndexed(i int) ktype.Slot {
	var s ktype.Slot
	s[28] = byte(i >> 24)
	s[29] = byte(i >> 16)
	s[30] = byte(i >> 8)
	s[31] = byte(i)
	return s
}

// blockPlan is one block's budget. Every category is non-zero on every block, so each is exercised
// per block rather than on average over a run.
type blockPlan struct {
	creates       int
	updates       int
	deletes       int
	absentDeletes int
}

func (p blockPlan) total() int { return p.creates + p.updates + p.deletes + p.absentDeletes }

// planBlock splits a block's operation budget across the four categories.
//
// Creates lead so state grows rather than churning in place, but updates and both kinds of delete each
// get a guaranteed share: a category that only appears sometimes is a category that is untested on the
// blocks where it does not.
func (w *agreementWorkload) planBlock() blockPlan {
	span := agreementMaxOpsPerBlock - agreementMinOpsPerBlock + 1
	total := agreementMinOpsPerBlock + w.rng.Intn(span)
	plan := blockPlan{
		creates:       total * 40 / 100,
		updates:       total * 30 / 100,
		deletes:       total * 20 / 100,
		absentDeletes: total * 10 / 100,
	}
	// Guarantee at least one of each even at the smallest budget.
	if plan.creates == 0 {
		plan.creates = 1
	}
	if plan.updates == 0 {
		plan.updates = 1
	}
	if plan.deletes == 0 {
		plan.deletes = 1
	}
	if plan.absentDeletes == 0 {
		plan.absentDeletes = 1
	}
	return plan
}

// nextBlock produces the changesets for one block, honouring the block's plan.
func (w *agreementWorkload) nextBlock(height int64) []*proto.NamedChangeSet {
	plan := w.planBlock()
	b := newBlockBuilder()

	// Creates first, so that the updates and deletes that follow have rows to aim at even on block 1.
	for i := 0; i < plan.creates; i++ {
		w.emitCreate(b, height, i)
	}
	for i := 0; i < plan.updates; i++ {
		w.emitUpdate(b, height, i)
	}
	for i := 0; i < plan.deletes; i++ {
		w.emitDelete(b)
	}
	for i := 0; i < plan.absentDeletes; i++ {
		w.emitAbsentDelete(b)
	}
	return b.changeSets()
}

// blockBuilder accumulates a block's pairs per module, emitting them in a fixed order.
type blockBuilder struct {
	evm      []*proto.KVPair
	byModule map[string][]*proto.KVPair
}

func newBlockBuilder() *blockBuilder {
	return &blockBuilder{byModule: make(map[string][]*proto.KVPair)}
}

func (b *blockBuilder) evmPair(pair *proto.KVPair) { b.evm = append(b.evm, pair) }

func (b *blockBuilder) modulePair(module string, pair *proto.KVPair) {
	b.byModule[module] = append(b.byModule[module], pair)
}

// changeSets renders the block. Module order is fixed because Go's map iteration order is randomized
// and a seeded run has to produce byte-identical blocks.
func (b *blockBuilder) changeSets() []*proto.NamedChangeSet {
	var out []*proto.NamedChangeSet
	if len(b.evm) > 0 {
		out = append(out, &proto.NamedChangeSet{
			Name:      keys.EVMStoreKey,
			Changeset: proto.ChangeSet{Pairs: b.evm},
		})
	}
	modules := make([]string, 0, len(b.byModule))
	for module := range b.byModule {
		modules = append(modules, module)
	}
	sort.Strings(modules)
	for _, module := range modules {
		out = append(out, &proto.NamedChangeSet{
			Name:      module,
			Changeset: proto.ChangeSet{Pairs: b.byModule[module]},
		})
	}
	return out
}

// freshAddr returns an address with no live account row, or false if the pool is saturated.
func (w *agreementWorkload) freshAddr() (ktype.Address, bool) {
	for i := 0; i < agreementAimAttempts; i++ {
		addr := w.addrs[w.rng.Intn(len(w.addrs))]
		if !w.liveAccounts.has(addr) {
			return addr, true
		}
	}
	return ktype.Address{}, false
}

// freshStorage returns a storage coordinate with no live row.
func (w *agreementWorkload) freshStorage() (storageCoord, bool) {
	for i := 0; i < agreementAimAttempts; i++ {
		c := storageCoord{
			addr: w.addrs[w.rng.Intn(len(w.addrs))],
			slot: w.slots[w.rng.Intn(len(w.slots))],
		}
		if !w.liveStorage.has(c) {
			return c, true
		}
	}
	return storageCoord{}, false
}

// freshMisc returns a misc coordinate with no live row.
func (w *agreementWorkload) freshMisc() (miscCoord, bool) {
	for i := 0; i < agreementAimAttempts; i++ {
		c := w.randomMiscCoord()
		if !w.liveMisc.has(c) {
			return c, true
		}
	}
	return miscCoord{}, false
}

func (w *agreementWorkload) randomMiscCoord() miscCoord {
	module := w.miscModules[w.rng.Intn(len(w.miscModules))]
	n := w.rng.Intn(agreementMiscKeyPool)
	if module == keys.EVMStoreKey {
		return miscCoord{module: module, key: string([]byte{0x09, byte(n >> 8), byte(n)})}
	}
	return miscCoord{module: module, key: fmt.Sprintf("k%04d", n)}
}

// emitCreate writes a row that does not exist yet, choosing a database at random.
func (w *agreementWorkload) emitCreate(b *blockBuilder, height int64, i int) {
	switch w.rng.Intn(6) {
	case 0: // a fresh EOA: nonce only, so the 49-byte account form is exercised
		addr, ok := w.freshAddr()
		if !ok {
			return
		}
		nonce := uint64(height)*1000 + uint64(i) + 1
		b.evmPair(noncePair(addr, nonce))
		w.setNonce(addr, nonce)
	case 1: // a fresh contract: nonce and codehash together, so the 81-byte form is exercised
		addr, ok := w.freshAddr()
		if !ok {
			return
		}
		nonce := uint64(height)*1000 + uint64(i) + 1
		b.evmPair(noncePair(addr, nonce))
		b.evmPair(codeHashPair(addr, codeHashN(byte(i%251)+1)))
		w.setNonce(addr, nonce)
		w.setCodeHash(addr, true)
	case 2: // a fresh row from a codehash alone, leaving the nonce zero
		//
		// The row still lives, because liveness needs only one non-zero field. It is the mirror of the
		// EOA case: a merge onto nothing that carries the other field.
		addr, ok := w.freshAddr()
		if !ok {
			return
		}
		b.evmPair(codeHashPair(addr, codeHashN(byte(i%251)+1)))
		w.setCodeHash(addr, true)
	case 3: // a fresh storage slot
		c, ok := w.freshStorage()
		if !ok {
			return
		}
		b.evmPair(storagePair(c.addr, c.slot, w.storageValue(height, i)))
		w.liveStorage.add(c)
	case 4: // deploy code for an address that has none
		//
		// Preferring an address that already has an account row keeps code and account rows correlated
		// as they are on chain, but any address will do — code is keyed independently.
		addr, ok := w.addrWithoutCode()
		if !ok {
			return
		}
		b.evmPair(codePair(addr, []byte{0x60, 0x80, byte(height), byte(i)}))
		w.liveCode.add(addr)
	default: // a fresh misc row, occasionally with an empty value, which is a live row
		c, ok := w.freshMisc()
		if !ok {
			return
		}
		value := []byte{byte(height), byte(i)}
		if w.rng.Intn(8) == 0 {
			value = []byte{}
		}
		b.modulePair(c.module, &proto.KVPair{Key: []byte(c.key), Value: value})
		w.liveMisc.add(c)
	}
}

// addrWithoutCode returns an address with no live code row, preferring one that already has an account.
func (w *agreementWorkload) addrWithoutCode() (ktype.Address, bool) {
	for i := 0; i < agreementAimAttempts; i++ {
		if addr, ok := w.liveAccounts.sample(w.rng); ok && !w.liveCode.has(addr) {
			return addr, true
		}
	}
	for i := 0; i < agreementAimAttempts; i++ {
		addr := w.addrs[w.rng.Intn(len(w.addrs))]
		if !w.liveCode.has(addr) {
			return addr, true
		}
	}
	return ktype.Address{}, false
}

// emitUpdate rewrites a row that already exists.
func (w *agreementWorkload) emitUpdate(b *blockBuilder, height int64, i int) {
	switch w.rng.Intn(6) {
	case 0: // bump an existing account's nonce alone, which must preserve its codehash
		addr, ok := w.liveAccounts.sample(w.rng)
		if !ok {
			return
		}
		nonce := w.accountNonce[addr] + uint64(i) + 1
		b.evmPair(noncePair(addr, nonce))
		w.setNonce(addr, nonce)
	case 1: // rewrite an existing account's codehash alone, which must preserve its nonce
		addr, ok := w.liveAccounts.sample(w.rng)
		if !ok {
			return
		}
		b.evmPair(codeHashPair(addr, codeHashN(byte(i%251)+1)))
		w.setCodeHash(addr, true)
	case 2: // rewrite both fields of an existing account in one write, replacing rather than merging
		addr, ok := w.liveAccounts.sample(w.rng)
		if !ok {
			return
		}
		nonce := w.accountNonce[addr] + uint64(i) + 1
		b.evmPair(noncePair(addr, nonce))
		b.evmPair(codeHashPair(addr, codeHashN(byte((i+7)%251)+1)))
		w.setNonce(addr, nonce)
		w.setCodeHash(addr, true)
	case 3: // overwrite an existing storage slot
		c, ok := w.liveStorage.sample(w.rng)
		if !ok {
			return
		}
		b.evmPair(storagePair(c.addr, c.slot, w.storageValue(height, i+1)))
	case 4: // redeploy over an existing code row
		addr, ok := w.liveCode.sample(w.rng)
		if !ok {
			return
		}
		b.evmPair(codePair(addr, []byte{0x60, 0x80, byte(height), byte(i), 0xFF}))
	default: // overwrite an existing misc row
		c, ok := w.liveMisc.sample(w.rng)
		if !ok {
			return
		}
		b.modulePair(c.module, &proto.KVPair{
			Key:   []byte(c.key),
			Value: []byte{byte(height), byte(i), 0xEE},
		})
	}
}

// emitDelete removes a row that exists.
func (w *agreementWorkload) emitDelete(b *blockBuilder) {
	switch w.rng.Intn(4) {
	case 0: // remove an account row entirely by zeroing every field
		addr, ok := w.liveAccounts.sample(w.rng)
		if !ok {
			return
		}
		// Order is varied because zeroing the fields in either sequence must be equivalent.
		if w.rng.Intn(2) == 0 {
			b.evmPair(nonceDeletePair(addr))
			b.evmPair(codeHashDeletePair(addr))
		} else {
			b.evmPair(codeHashDeletePair(addr))
			b.evmPair(nonceDeletePair(addr))
		}
		w.setNonce(addr, 0)
		w.setCodeHash(addr, false)
	case 1: // delete an existing storage slot, half the time via an all-zero write
		c, ok := w.liveStorage.sample(w.rng)
		if !ok {
			return
		}
		if w.rng.Intn(2) == 0 {
			b.evmPair(storageDeletePair(c.addr, c.slot))
		} else {
			b.evmPair(&proto.KVPair{
				Key:   keys.BuildEVMKey(keys.EVMKeyStorage, ktype.StorageKey(c.addr, c.slot)),
				Value: make([]byte, 32),
			})
		}
		w.liveStorage.remove(c)
	case 2: // delete an existing code row
		addr, ok := w.liveCode.sample(w.rng)
		if !ok {
			return
		}
		b.evmPair(codeDeletePair(addr))
		w.liveCode.remove(addr)
	default: // delete an existing misc row
		c, ok := w.liveMisc.sample(w.rng)
		if !ok {
			return
		}
		b.modulePair(c.module, &proto.KVPair{Key: []byte(c.key), Delete: true})
		w.liveMisc.remove(c)
	}
}

// emitAbsentDelete deletes a key that is not there.
//
// This is legal and must be a strict no-op on every hash, so it is budgeted per block rather than left
// to arise by accident.
func (w *agreementWorkload) emitAbsentDelete(b *blockBuilder) {
	switch w.rng.Intn(4) {
	case 0:
		if addr, ok := w.freshAddr(); ok {
			b.evmPair(nonceDeletePair(addr))
		}
	case 1:
		if c, ok := w.freshStorage(); ok {
			b.evmPair(storageDeletePair(c.addr, c.slot))
		}
	case 2:
		if addr, ok := w.freshAddr(); ok && !w.liveCode.has(addr) {
			b.evmPair(codeDeletePair(addr))
		}
	default:
		if c, ok := w.freshMisc(); ok {
			b.modulePair(c.module, &proto.KVPair{Key: []byte(c.key), Delete: true})
		}
	}
}

// storageValue builds a storage value that is never all-zero, since an all-zero value is a delete and
// would silently turn a write into a removal.
func (w *agreementWorkload) storageValue(height int64, i int) []byte {
	return []byte{0x01, byte(height), byte(i)}
}

// setNonce records an account's nonce and keeps the live set in step: a row lives while any field is
// non-zero.
func (w *agreementWorkload) setNonce(addr ktype.Address, nonce uint64) {
	w.accountNonce[addr] = nonce
	w.refreshAccountLiveness(addr)
}

func (w *agreementWorkload) setCodeHash(addr ktype.Address, present bool) {
	w.accountCodeHash[addr] = present
	w.refreshAccountLiveness(addr)
}

func (w *agreementWorkload) refreshAccountLiveness(addr ktype.Address) {
	if w.accountNonce[addr] != 0 || w.accountCodeHash[addr] {
		w.liveAccounts.add(addr)
		return
	}
	w.liveAccounts.remove(addr)
}

// =============================================================================
// Configuration
// =============================================================================

// agreementConfig returns a store config suited to exporting at every block.
//
// SnapshotInterval is 1 so that exporting at N resolves to a snapshot at exactly N and replays no WAL
// blocks; with the default interval every export would replay 1..N and the suite would be quadratic.
// SnapshotKeepRecent is generous so a snapshot is never pruned out from under an export.
func agreementConfig(t *testing.T) *config.Config {
	t.Helper()
	cfg := config.DefaultTestConfig(t)
	cfg.SnapshotInterval = 1
	cfg.SnapshotKeepRecent = 1000
	return cfg
}

// =============================================================================
// Tests
// =============================================================================

// Blocks carry hundreds of operations so that a single block exercises overwrite, delete, recreate and
// account merge against a state large enough for those to interact, rather than a handful of rows where
// most operations touch a key nothing else touched.
//
// Each block of the import tests exports and imports the whole state, which is O(state) per block — so
// the block count stays modest while the per-block operation count carries the coverage.
const (
	agreementBlocks         = 12
	agreementMinOpsPerBlock = 500
	agreementMaxOpsPerBlock = 2500
)

// TestLtHashAgreementRegularOperations drives randomized blocks and checks, at every block, that the
// store's maintained hashes agree with an independently derived model and that the store holds exactly
// the rows the model expects.
//
// This is the producer row that already had partial coverage: TestLtHashIncrementalEqualsFullScan
// compares the maintained hash to a full rescan. Both describe what is on disk, so neither notices a
// wrong row; the model does.
func TestLtHashAgreementRegularOperations(t *testing.T) {
	seed := agreementSeed(t, "TestLtHashAgreementRegularOperations")
	rng := rand.New(rand.NewSource(seed)) //nolint:gosec // deterministic test data only

	s := setupTestStoreWithConfig(t, agreementConfig(t))
	defer func() { _ = s.Close() }()

	model := newStateModel()
	workload := newAgreementWorkload(rng)

	for height := int64(1); height <= agreementBlocks; height++ {
		changeSets := workload.nextBlock(height)

		// Sometimes split a block across two ApplyChangeSets calls: the store re-reads accounts live
		// for each, so the second must fold onto the first's staged row rather than the committed one.
		if len(changeSets) > 1 && rng.Intn(3) == 0 {
			split := 1 + rng.Intn(len(changeSets)-1)
			require.NoError(t, s.ApplyChangeSets(height, changeSets[:split]))
			model.apply(t, height, changeSets[:split])
			require.NoError(t, s.ApplyChangeSets(height, changeSets[split:]))
			model.apply(t, height, changeSets[split:])
		} else if len(changeSets) > 0 {
			require.NoError(t, s.ApplyChangeSets(height, changeSets))
			model.apply(t, height, changeSets)
		}

		_, err := s.Commit(height)
		require.NoError(t, err, "commit block %d", height)
		require.Equal(t, height, s.Version())

		requireModelAgrees(t, s, model, fmt.Sprintf("block %d", height))
	}

	// The full-scan observer, which cross-checks the maintained hash against what is on disk.
	require.NoError(t, VerifyLtHash(s))
	requireAgreementWorkloadExercised(t, model)
}

// TestLtHashAgreementImport drives randomized blocks and, at every block, exports that block and
// imports it into a fresh store, requiring the imported store to agree with both the source and the
// model.
//
// Importing every block localises a divergence to the block that introduced it. The existing coverage
// exports once, so a divergence at block 2 is only reported at the end.
func TestLtHashAgreementImport(t *testing.T) {
	seed := agreementSeed(t, "TestLtHashAgreementImport")
	rng := rand.New(rand.NewSource(seed)) //nolint:gosec // deterministic test data only

	src := setupTestStoreWithConfig(t, agreementConfig(t))
	defer func() { _ = src.Close() }()

	// One target reused across blocks: each import wipes the previous one, which also exercises the
	// purge of rows the incoming snapshot does not contain.
	dst := setupTestStoreWithConfig(t, agreementConfig(t))
	defer func() { _ = dst.Close() }()

	model := newStateModel()
	workload := newAgreementWorkload(rng)
	comparator := newStoreComparator()
	totalImported := 0

	for height := int64(1); height <= agreementBlocks; height++ {
		changeSets := workload.nextBlock(height)
		if len(changeSets) > 0 {
			require.NoError(t, src.ApplyChangeSets(height, changeSets))
			model.apply(t, height, changeSets)
		}
		_, err := src.Commit(height)
		require.NoError(t, err, "commit block %d", height)

		because := fmt.Sprintf("block %d", height)
		requireModelAgrees(t, src, model, because+" (source)")

		imported := importFromInto(t, src, dst, height)
		totalImported += imported
		comparator.requireAgree(t, src, dst, because+" (imported vs source)")
		requireModelAgrees(t, dst, model, because+" (imported)")
	}

	require.NoError(t, VerifyLtHash(dst))
	require.Positive(t, totalImported, "no rows ever crossed the import boundary; the suite is vacuous")
	t.Logf("imported %d rows across %d blocks; final state holds %d rows",
		totalImported, agreementBlocks, len(model.expect().rows))
	requireAgreementWorkloadExercised(t, model)
}

// TestLtHashAgreementImportThenCommit imports at a block and then keeps committing on the imported
// store alongside the source, requiring the two to stay in lockstep.
//
// This is the case no existing test covers, and it is the one most likely to fail. An imported store's
// view managers hold a baseline sealed before the import, and the first post-import commit derives the
// block's hash by diffing the newly sealed view against that baseline. If the baseline does not account for
// the imported rows, the first commit after an import mixes in new values without mixing out the values
// they replaced — so block N agrees and block N+1 does not.
func TestLtHashAgreementImportThenCommit(t *testing.T) {
	seed := agreementSeed(t, "TestLtHashAgreementImportThenCommit")
	rng := rand.New(rand.NewSource(seed)) //nolint:gosec // deterministic test data only

	src := setupTestStoreWithConfig(t, agreementConfig(t))
	defer func() { _ = src.Close() }()

	model := newStateModel()
	workload := newAgreementWorkload(rng)

	// Build a few blocks of history, then hand it to a fresh store by import.
	const importAt = 6
	for height := int64(1); height <= importAt; height++ {
		changeSets := workload.nextBlock(height)
		if len(changeSets) > 0 {
			require.NoError(t, src.ApplyChangeSets(height, changeSets))
			model.apply(t, height, changeSets)
		}
		_, err := src.Commit(height)
		require.NoError(t, err)
	}
	requireModelAgrees(t, src, model, "source before export")

	dst := setupTestStoreWithConfig(t, agreementConfig(t))
	defer func() { _ = dst.Close() }()

	imported := importFromInto(t, src, dst, importAt)
	require.Positive(t, imported, "nothing was imported; the rest of this test would be vacuous")
	comparator := newStoreComparator()
	comparator.requireAgree(t, src, dst, "immediately after import")
	requireModelAgrees(t, dst, model, "immediately after import")

	// Now drive both forward over the same blocks. The imported store has no WAL history and a
	// baseline sealed before the import; the source has neither peculiarity.
	for height := int64(importAt + 1); height <= agreementBlocks; height++ {
		changeSets := workload.nextBlock(height)
		if len(changeSets) > 0 {
			require.NoError(t, src.ApplyChangeSets(height, changeSets))
			require.NoError(t, dst.ApplyChangeSets(height, changeSets))
			model.apply(t, height, changeSets)
		}
		_, err := src.Commit(height)
		require.NoError(t, err, "source commit %d", height)
		_, err = dst.Commit(height)
		require.NoError(t, err, "imported store commit %d", height)

		because := fmt.Sprintf("block %d after importing at %d", height, importAt)
		requireModelAgrees(t, src, model, because+" (source)")
		requireModelAgrees(t, dst, model, because+" (imported)")
		comparator.requireAgree(t, src, dst, because)
	}

	require.NoError(t, VerifyLtHash(src))
	require.NoError(t, VerifyLtHash(dst))
	requireAgreementWorkloadExercised(t, model)
}

// requireAgreementWorkloadExercised asserts the generated workload actually populated every database.
// Without it a generator that silently stopped producing, say, code rows would leave the suite green
// while covering less than it claims.
func requireAgreementWorkloadExercised(t *testing.T, m *stateModel) {
	t.Helper()
	c := m.coverage
	t.Logf("coverage per database:")
	for _, dir := range dataDBDirs {
		t.Logf("  %-8s creates=%d updates=%d deletes=%d absent-deletes=%d",
			dir, c.creates[dir], c.updates[dir], c.deletes[dir], c.absentDeletes[dir])
	}
	t.Logf("  contract accounts=%d EOA accounts=%d misc modules=%d",
		c.contractAccounts, c.eoaAccounts, len(c.miscModules))
	t.Logf("account write shapes:")
	for _, shape := range accountShapeNames {
		t.Logf("  %-22s %d", shape, c.accountShapes[shape])
	}

	// Single-field writes onto an existing row are the merge: the field not written must survive. Both
	// directions must appear, as must the two-field write that replaces rather than merges.
	for _, shape := range accountShapeNames {
		require.Positivef(t, c.accountShapes[shape],
			"workload never produced an account write of shape %q", shape)
	}

	// Every database must see all four categories. Each is a distinct risk, and a category absent from
	// a database is a hole no amount of volume elsewhere covers.
	for _, dir := range dataDBDirs {
		require.Positivef(t, c.creates[dir], "workload never created a row in the %s database", dir)
		require.Positivef(t, c.updates[dir], "workload never updated an existing row in the %s database", dir)
		require.Positivef(t, c.deletes[dir], "workload never deleted an existing row in the %s database", dir)
		require.Positivef(t, c.absentDeletes[dir],
			"workload never deleted an absent key in the %s database, so the no-op path is untested", dir)
	}
	// Both serialized forms of an account row must be hashed: 49 bytes when the codehash is all-zero,
	// 81 otherwise.
	require.Positive(t, c.contractAccounts, "no contract accounts: the 81-byte account form was never hashed")
	require.Positive(t, c.eoaAccounts, "no EOA accounts: the 49-byte account form was never hashed")
	require.Greater(t, len(c.miscModules), 1, "misc rows came from only one module")
}
