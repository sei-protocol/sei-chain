package operations

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-db/common/keys"
	"github.com/sei-protocol/sei-chain/sei-db/common/utils"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/memiavl"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/migration"
)

// This file pins the JSON reports of evm-logical-digest to values recorded on disk, so that a change to
// how the scan reads rows has to either reproduce them or be seen changing them. kvrepair-export and the
// runbook digest gates read these reports, and digests taken by two builds are compared with each other.
//
// The pinned fields are every field except source, normalization, and db_dir, which are descriptive
// text. Each of those must still be present. A composite report keeps the memiavl version it read out of
// its normalization text, because nothing else in the report records it.

// evmDigestGoldenRecord regenerates the committed reports instead of checking against them. Off by
// default, and refused outright on CI: see recordEvmDigestGolden.
var evmDigestGoldenRecord = flag.Bool("evm-digest-golden-record", false,
	"rewrite the committed evm-logical-digest golden reports from this build instead of verifying against them")

const evmDigestGoldenFile = "testdata/evm_logical_digest_golden.json"

// Heights of the golden fixture. The memiavl snapshot is at goldenSnapshotHeight, and the blocks above
// it exist only in the changelog.
const (
	goldenSnapshotHeight = 2
	goldenMidHeight      = 3
	goldenTipHeight      = 4
)

const goldenDescriptive = "<descriptive>"

var goldenMemiavlVersionPattern = regexp.MustCompile(`memiavl_version=(\d+)`)

var (
	goldenAccountA     = bytesOfLen(keys.AddressLen, 0x11)
	goldenAccountB     = bytesOfLen(keys.AddressLen, 0x22)
	goldenZeroAccount  = bytesOfLen(keys.AddressLen, 0x33)
	goldenContract     = bytesOfLen(keys.AddressLen, 0x44)
	goldenEmptyCode    = bytesOfLen(keys.AddressLen, 0x55)
	goldenHighAccount  = bytesOfLen(keys.AddressLen, 0xFF)
	goldenContractMisc = append([]byte{0x09}, goldenContract...)
	goldenLowMisc      = append([]byte{0x01}, bytesOfLen(keys.AddressLen, 0x66)...)
)

// TestEvmLogicalDigestGoldenReports runs every backend and open mode over one fixture and requires this
// build to produce the reports committed under testdata.
func TestEvmLogicalDigestGoldenReports(t *testing.T) {
	fx := buildEvmDigestGoldenFixture(t)
	got := runEvmDigestGoldenCases(t, fx)

	if *evmDigestGoldenRecord {
		recordEvmDigestGolden(t, got)
		return
	}

	data, err := os.ReadFile(evmDigestGoldenFile)
	require.NoError(t, err, "read %s; record it with -evm-digest-golden-record", evmDigestGoldenFile)
	var want map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(data, &want))

	for name, report := range got {
		recorded, ok := want[name]
		if !ok {
			t.Errorf("case %s has no recorded report; re-record with -evm-digest-golden-record", name)
			continue
		}
		require.JSONEq(t, string(recorded), string(report), "case %s differs from the recorded report; "+
			"if the change is intended, re-record with -evm-digest-golden-record and review the diff", name)
	}
	for name := range want {
		_, ok := got[name]
		require.True(t, ok, "recorded case %s is no longer produced", name)
	}
}

// recordEvmDigestGolden replaces the committed reports with the ones produced by this build.
//
// It refuses to run on CI. The recorded reports are the only statement of what the output is expected to
// be, so a build that regenerated them as part of an ordinary test run would pass by agreeing with itself.
func recordEvmDigestGolden(t *testing.T, got map[string]json.RawMessage) {
	t.Helper()
	if os.Getenv("CI") != "" {
		t.Fatal("refusing to re-record the evm-logical-digest golden reports on CI: " +
			"the recorded reports are the expected values, so a build that rewrites them verifies nothing")
	}
	data, err := json.MarshalIndent(got, "", "  ")
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll("testdata", 0o750))
	require.NoError(t, os.WriteFile(evmDigestGoldenFile, append(data, '\n'), 0o600))
	t.Logf("recorded %d cases into %s — review the diff before committing", len(got), evmDigestGoldenFile)
}

type evmDigestGoldenFixture struct {
	memiavlDir         string
	flatkvDir          string
	compositeFlatKVDir string
}

// buildEvmDigestGoldenFixture writes the same EVM blocks to a memiavl store and a FlatKV store, and a
// mid-migration FlatKV store whose boundary sits inside the contract's storage. The changelog blocks
// above the memiavl snapshot hold one case for each way a changelog row can relate to a snapshot row.
func buildEvmDigestGoldenFixture(t *testing.T) evmDigestGoldenFixture {
	t.Helper()
	home := t.TempDir()
	mem := newTestMemiavlStore(t, home)
	flat, flatDir := newDiskBackedFlatKVStore(t, 1)
	migrated, migratedDir := newDiskBackedFlatKVStore(t, 1)

	for i, pairs := range goldenEVMBlocks() {
		version := int64(i + 1)
		require.NoError(t, mem.ApplyChangeSets([]*proto.NamedChangeSet{{
			Name: keys.EVMStoreKey, Changeset: proto.ChangeSet{Pairs: pairs},
		}}))
		_, err := mem.Commit(version)
		require.NoError(t, err)
		if version == goldenSnapshotHeight {
			require.NoError(t, mem.GetDB().RewriteSnapshot(context.Background()))
		}
		commitFlatKVForGolden(t, flat, version, []*proto.NamedChangeSet{{
			Name: keys.EVMStoreKey, Changeset: proto.ChangeSet{Pairs: pairs},
		}})
		commitFlatKVForGolden(t, migrated, version, goldenMigratedBlock(version))
	}

	require.NoError(t, mem.Close())
	require.NoError(t, flat.Close())
	require.NoError(t, migrated.Close())
	return evmDigestGoldenFixture{
		memiavlDir:         utils.GetCosmosSCStorePath(home),
		flatkvDir:          flatDir,
		compositeFlatKVDir: migratedDir,
	}
}

func commitFlatKVForGolden(t *testing.T, store *flatkv.CommitStore, version int64, changesets []*proto.NamedChangeSet) {
	t.Helper()
	require.NoError(t, store.ApplyChangeSets(version, changesets))
	_, err := store.Commit(version)
	require.NoError(t, err)
	require.NoError(t, store.FlushSnapshots())
}

func goldenStorageKey(slot byte) []byte {
	return keys.BuildEVMKey(keys.EVMKeyStorage, append(append([]byte{}, goldenContract...), padLeft32(slot)...))
}

func goldenSet(key, value []byte) *proto.KVPair { return &proto.KVPair{Key: key, Value: value} }

func goldenDelete(key []byte) *proto.KVPair { return &proto.KVPair{Key: key, Delete: true} }

// goldenEVMBlocks returns the raw memiavl EVM writes of each block, starting at version 1.
func goldenEVMBlocks() [][]*proto.KVPair {
	return [][]*proto.KVPair{
		{
			goldenSet(keys.BuildEVMKey(keys.EVMKeyNonce, goldenAccountA), nonceBytes(1)),
			goldenSet(keys.BuildEVMKey(keys.EVMKeyBalance, goldenAccountA), padLeft32(100)),
			goldenSet(keys.BuildEVMKey(keys.EVMKeyCodeHash, goldenAccountA), bytesOfLen(32, 0xA1)),
			goldenSet(keys.BuildEVMKey(keys.EVMKeyBalance, goldenAccountB), padLeft32(7)),
			goldenSet(keys.BuildEVMKey(keys.EVMKeyNonce, goldenZeroAccount), nonceBytes(0)),
			goldenSet(keys.BuildEVMKey(keys.EVMKeyCodeHash, goldenZeroAccount), make([]byte, 32)),
			goldenSet(keys.BuildEVMKey(keys.EVMKeyCode, goldenContract), []byte{0x60, 0x01}),
			goldenSet(keys.BuildEVMKey(keys.EVMKeyCodeHash, goldenContract), bytesOfLen(32, 0xC0)),
			goldenSet(goldenStorageKey(1), padLeft32(0x11)),
			goldenSet(goldenStorageKey(2), padLeft32(0x12)),
			goldenSet(goldenStorageKey(3), padLeft32(0x13)),
			goldenSet(goldenStorageKey(4), padLeft32(0x14)),
			goldenSet(goldenStorageKey(5), padLeft32(0x15)),
			goldenSet(goldenContractMisc, []byte{0x01}),
		},
		{
			goldenSet(goldenStorageKey(1), padLeft32(0x21)),
			goldenSet(keys.BuildEVMKey(keys.EVMKeyNonce, goldenAccountA), nonceBytes(2)),
		},
		// Changelog only, above the snapshot.
		{
			goldenSet(goldenStorageKey(6), padLeft32(0x36)), // new key
			goldenSet(goldenStorageKey(2), padLeft32(0x32)), // overwrite of a snapshot key
			goldenDelete(goldenStorageKey(3)),               // delete of a snapshot key
			goldenSet(goldenStorageKey(7), padLeft32(0x37)), // deleted again in the next block
			goldenDelete(goldenStorageKey(4)),               // set again in the next block
			goldenDelete(goldenStorageKey(8)),               // never existed
			goldenSet(goldenStorageKey(9), make([]byte, 32)),
			goldenSet(keys.BuildEVMKey(keys.EVMKeyCode, goldenEmptyCode), []byte{}),
			goldenSet(goldenLowMisc, []byte{0x02}),                                           // below the first snapshot key
			goldenSet(keys.BuildEVMKey(keys.EVMKeyBalance, goldenHighAccount), padLeft32(9)), // above the last snapshot key
		},
		{
			goldenDelete(goldenStorageKey(7)),
			goldenSet(goldenStorageKey(4), padLeft32(0x44)),
			goldenSet(goldenStorageKey(6), padLeft32(0x46)),
		},
	}
}

// goldenMigratedBlock returns the writes of the mid-migration FlatKV store: storage slots at and below
// the boundary are migrated at version 1, and the later blocks are empty.
func goldenMigratedBlock(version int64) []*proto.NamedChangeSet {
	if version != 1 {
		return nil
	}
	boundary := migration.NewMigrationBoundary(keys.EVMStoreKey, goldenStorageKey(3))
	return []*proto.NamedChangeSet{
		{Name: keys.EVMStoreKey, Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{
			goldenSet(goldenStorageKey(1), padLeft32(0xF1)),
			goldenSet(goldenStorageKey(2), padLeft32(0xF2)),
		}}},
		{Name: migration.MigrationStore, Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{
			goldenSet([]byte(migration.MigrationBoundaryKey), boundary.Serialize()),
		}}},
	}
}

type evmDigestGoldenSource struct {
	name  string
	flags map[string]string
	// inspect is false where the inspect path refuses the source.
	inspect bool
	// details is true where the source supports --details on a storage list.
	details bool
}

func goldenMemiavlSource(fx evmDigestGoldenFixture, height int64, openMode, normalization string) map[string]string {
	return map[string]string{
		"backend":               "memiavl",
		"db-dir":                fx.memiavlDir,
		"height":                strconv.FormatInt(height, 10),
		"memiavl-open-mode":     openMode,
		"memiavl-normalization": normalization,
	}
}

func goldenFlatKVSource(fx evmDigestGoldenFixture, height int64) map[string]string {
	return map[string]string{
		"backend": "flatkv",
		"db-dir":  fx.flatkvDir,
		"height":  strconv.FormatInt(height, 10),
	}
}

func goldenCompositeSource(fx evmDigestGoldenFixture, height int64, openMode string) map[string]string {
	return map[string]string{
		"backend":           "composite",
		"flatkv-dir":        fx.compositeFlatKVDir,
		"memiavl-dir":       fx.memiavlDir,
		"height":            strconv.FormatInt(height, 10),
		"memiavl-open-mode": openMode,
	}
}

func evmDigestGoldenSources(fx evmDigestGoldenFixture) []evmDigestGoldenSource {
	return []evmDigestGoldenSource{
		{"memiavl-semantic-snapshot-h2", goldenMemiavlSource(fx, goldenSnapshotHeight, memiavlOpenModeSnapshot, memiavlNormSemantic), true, true},
		{"memiavl-semantic-snapshot-current", goldenMemiavlSource(fx, 0, memiavlOpenModeSnapshot, memiavlNormSemantic), true, false},
		{"memiavl-semantic-replay-h2", goldenMemiavlSource(fx, goldenSnapshotHeight, memiavlOpenModeReplay, memiavlNormSemantic), true, false},
		{"memiavl-semantic-replay-h3", goldenMemiavlSource(fx, goldenMidHeight, memiavlOpenModeReplay, memiavlNormSemantic), true, false},
		{"memiavl-semantic-replay-h4", goldenMemiavlSource(fx, goldenTipHeight, memiavlOpenModeReplay, memiavlNormSemantic), true, false},
		{"memiavl-semantic-replay-tip", goldenMemiavlSource(fx, 0, memiavlOpenModeReplay, memiavlNormSemantic), true, false},
		{"memiavl-translator-snapshot-h2", goldenMemiavlSource(fx, goldenSnapshotHeight, memiavlOpenModeSnapshot, memiavlNormTranslator), true, false},
		{"memiavl-translator-replay-h4", goldenMemiavlSource(fx, goldenTipHeight, memiavlOpenModeReplay, memiavlNormTranslator), false, false},
		{"flatkv-h2", goldenFlatKVSource(fx, goldenSnapshotHeight), true, false},
		{"flatkv-h4", goldenFlatKVSource(fx, goldenTipHeight), true, true},
		{"composite-snapshot-h2", goldenCompositeSource(fx, goldenSnapshotHeight, memiavlOpenModeSnapshot), true, false},
		{"composite-replay-h3", goldenCompositeSource(fx, goldenMidHeight, memiavlOpenModeReplay), true, false},
		{"composite-replay-h4", goldenCompositeSource(fx, goldenTipHeight, memiavlOpenModeReplay), true, false},
	}
}

// evmDigestGoldenInspections returns the inspect flags run against each source, keyed by case suffix.
func evmDigestGoldenInspections() map[string]map[string]string {
	offset := strconv.Itoa(len(keys.EVMStoreKey) + 1)
	list := func(bucket string) map[string]string {
		return map[string]string{"inspect-bucket": bucket, "key-offset": offset, "list": "true", "list-limit": "0"}
	}
	out := map[string]map[string]string{}
	for _, bucket := range flatkvBucketOrder {
		out["list-"+bucket] = list(bucket)
	}
	out["shard-storage"] = map[string]string{
		"inspect-bucket":   flatkvBucketStorage,
		"key-offset":       offset,
		"key-prefix":       "03" + hex.EncodeToString(goldenContract),
		"shard-next-bytes": "32",
	}
	truncated := list(flatkvBucketStorage)
	truncated["list-limit"] = "2"
	out["list-storage-limit2"] = truncated
	account := list(flatkvBucketAccount)
	account["key-prefix"] = "0A11"
	out["list-account-prefix"] = account
	return out
}

func runEvmDigestGoldenCases(t *testing.T, fx evmDigestGoldenFixture) map[string]json.RawMessage {
	t.Helper()
	got := map[string]json.RawMessage{}
	for _, src := range evmDigestGoldenSources(fx) {
		got["digest/"+src.name] = runEvmDigestGoldenCase(t, src.flags, nil)
		if !src.inspect {
			continue
		}
		for suffix, inspect := range evmDigestGoldenInspections() {
			got["inspect/"+src.name+"/"+suffix] = runEvmDigestGoldenCase(t, src.flags, inspect)
		}
		if src.details {
			details := evmDigestGoldenInspections()["list-"+flatkvBucketStorage]
			details["details"] = "true"
			got["inspect/"+src.name+"/list-storage-details"] = runEvmDigestGoldenCase(t, src.flags, details)
		}
	}
	return got
}

// runEvmDigestGoldenCase runs the command with the given flags and returns its canonical JSON report.
func runEvmDigestGoldenCase(t *testing.T, source, inspect map[string]string) json.RawMessage {
	t.Helper()
	cmd := EvmLogicalDigestCmd()
	for _, flags := range []map[string]string{source, inspect} {
		for name, value := range flags {
			require.NoError(t, cmd.Flags().Set(name, value), "flag %s", name)
		}
	}
	_, jsonReport := captureDigestOutput(t, true)
	require.NoError(t, runEvmLogicalDigest(cmd, nil), "flags %v %v", source, inspect)
	return canonicalEvmDigestGoldenReport(t, jsonReport.Bytes())
}

// canonicalEvmDigestGoldenReport replaces the descriptive fields with a placeholder, and sorts account
// list entries, which the scan emits in map order.
func canonicalEvmDigestGoldenReport(t *testing.T, raw []byte) json.RawMessage {
	t.Helper()
	require.Equal(t, 1, bytes.Count(raw, []byte("\n")), "a report is one line: %s", raw)

	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var report map[string]any
	require.NoError(t, dec.Decode(&report))

	if report["backend"] == "composite" {
		normalization, _ := report["normalization"].(string)
		match := goldenMemiavlVersionPattern.FindStringSubmatch(normalization)
		require.Len(t, match, 2, "composite normalization has no memiavl_version: %q", normalization)
		report["memiavl_version_in_normalization"] = match[1]
	}
	for _, field := range []string{"source", "normalization", "db_dir"} {
		value, _ := report[field].(string)
		require.NotEmpty(t, value, "report field %s is empty", field)
		report[field] = goldenDescriptive
	}
	if report["inspect_bucket"] == flatkvBucketAccount {
		sortGoldenEntries(t, report)
	}

	out, err := json.Marshal(report)
	require.NoError(t, err)
	return out
}

func sortGoldenEntries(t *testing.T, report map[string]any) {
	t.Helper()
	entries, _ := report["entries"].([]any)
	keyOf := func(i int) string {
		entry, ok := entries[i].(map[string]any)
		require.True(t, ok, "entry %d is %T", i, entries[i])
		return fmt.Sprint(entry["key"])
	}
	sort.SliceStable(entries, func(i, j int) bool { return keyOf(i) < keyOf(j) })
}

// The fixture must exercise the snapshot boundary it claims to: the blocks above it exist only in the
// changelog, so snapshot mode cannot read them.
func TestEvmDigestGoldenFixtureHasNoSnapshotAboveTheBoundary(t *testing.T) {
	fx := buildEvmDigestGoldenFixture(t)
	for _, height := range []int64{goldenMidHeight, goldenTipHeight} {
		_, err := os.Stat(goldenMemiavlSnapshotDir(fx.memiavlDir, height))
		require.ErrorIs(t, err, os.ErrNotExist, "height %d has a memiavl snapshot", height)
	}
	_, err := os.Stat(goldenMemiavlSnapshotDir(fx.memiavlDir, goldenSnapshotHeight))
	require.NoError(t, err)
}

func goldenMemiavlSnapshotDir(memiavlDir string, height int64) string {
	return filepath.Join(memiavlDir, fmt.Sprintf("%s%020d", memiavl.SnapshotPrefix, height))
}
