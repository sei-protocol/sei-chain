package operations

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/spf13/cobra"

	"github.com/sei-protocol/sei-chain/sei-db/common/keys"
	"github.com/sei-protocol/sei-chain/sei-db/common/kvrepair"
)

// evmPhysicalKeyPrefix is the module prefix of every EVM row in an inspect
// report.
var evmPhysicalKeyPrefix = []byte(keys.EVMStoreKey + "/")

// accountLogicalLen is the length of an account's logical value in an inspect
// report: balance(32) || nonce(8) || codeHash(32).
const accountLogicalLen = 72

// KVRepairExportCmd writes a kvrepair file from two evm-logical-digest inspect
// list reports taken at the same height: one from a memiavl reserve and one
// from a production node. Each entry sets a key to the reserve value and
// states the production value as its old value.
//
// Usage:
//
//	seidb evm-logical-digest --backend memiavl -d <reserve memiavl dir> \
//	    --memiavl-open-mode replay --height H --inspect-bucket storage \
//	    --key-offset 4 --key-prefix 03AB --list --list-limit 0 --json > reserve.json
//	seidb evm-logical-digest --backend composite --memiavl-open-mode replay \
//	    --flatkv-dir <flatkv dir> --memiavl-dir <memiavl dir> --height H \
//	    --inspect-bucket storage --key-offset 4 --key-prefix 03AB \
//	    --list --list-limit 0 --json > prod.json
//	seidb kvrepair-export --reserve reserve.json --prod prod.json \
//	    --chain-id pacific-1 --name pacific-1-evm-<H+1> --target-repair-height <H+1> \
//	    -o app/upgrades/kvrepair/repairs/pacific-1-evm-<H+1>.json
func KVRepairExportCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "kvrepair-export",
		Short: "Write a kvrepair file from a reserve and a production evm-logical-digest inspect list",
		Args:  cobra.NoArgs,
		RunE:  runKVRepairExport,
	}
	cmd.Flags().String("reserve", "", "Inspect list report (--list --json) from a memiavl reserve")
	cmd.Flags().String("prod", "", "Inspect list report (--list --json) from a production node, at the same height, bucket, offset, and prefix")
	cmd.Flags().String("chain-id", "", "Chain ID the repair runs on")
	cmd.Flags().String("name", "", "Repair name, unique across the repair files")
	cmd.Flags().Int64("target-repair-height", 0, "Height whose BeginBlock applies the repair; must be above the report height")
	cmd.Flags().String("source", "", "Free text for reviewers; defaults to the two report sources")
	cmd.Flags().StringP("output", "o", "", "Output file; stdout when empty")
	return cmd
}

func runKVRepairExport(cmd *cobra.Command, _ []string) error {
	reservePath, _ := cmd.Flags().GetString("reserve")
	prodPath, _ := cmd.Flags().GetString("prod")
	chainID, _ := cmd.Flags().GetString("chain-id")
	name, _ := cmd.Flags().GetString("name")
	targetRepairHeight, _ := cmd.Flags().GetInt64("target-repair-height")
	source, _ := cmd.Flags().GetString("source")
	output, _ := cmd.Flags().GetString("output")
	if reservePath == "" || prodPath == "" || chainID == "" || name == "" || targetRepairHeight == 0 {
		return errors.New("--reserve, --prod, --chain-id, --name, and --target-repair-height are required")
	}

	reserve, err := readInspectReport(reservePath)
	if err != nil {
		return fmt.Errorf("reserve report: %w", err)
	}
	prod, err := readInspectReport(prodPath)
	if err != nil {
		return fmt.Errorf("production report: %w", err)
	}
	if source == "" {
		source = fmt.Sprintf("reserve %s %s and production %s %s at %d",
			reserve.Backend, reserve.DBDir, prod.Backend, prod.DBDir, reserve.Version)
	}
	r, err := exportKVRepair(reserve, prod, chainID, name, targetRepairHeight, source)
	if err != nil {
		return err
	}

	var encoded bytes.Buffer
	if err := kvrepair.Encode(&encoded, r); err != nil {
		return err
	}
	if output == "" {
		_, err = cmd.OutOrStdout().Write(encoded.Bytes())
		return err
	}
	if err := os.WriteFile(filepath.Clean(output), encoded.Bytes(), 0o600); err != nil {
		return err
	}
	_, err = fmt.Fprintf(cmd.ErrOrStderr(), "wrote %d entries for %s bucket keys under prefix %q at state height %d to %s\n",
		len(r.Entries), reserve.InspectBucket, reserve.KeyPrefix, r.StateHeight, output)
	return err
}

func readInspectReport(path string) (evmInspectJSON, error) {
	data, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return evmInspectJSON{}, err
	}
	var report evmInspectJSON
	if err := json.Unmarshal(data, &report); err != nil {
		return evmInspectJSON{}, fmt.Errorf("%s: %w", path, err)
	}
	return report, nil
}

// exportKVRepair builds the repair that makes production match the reserve
// for every key the two reports list, and checks that a node would load it.
func exportKVRepair(reserve, prod evmInspectJSON, chainID, name string, targetRepairHeight int64, source string) (kvrepair.Repair, error) {
	if err := checkInspectPair(reserve, prod); err != nil {
		return kvrepair.Repair{}, err
	}
	if targetRepairHeight <= reserve.Version {
		return kvrepair.Repair{}, fmt.Errorf("--target-repair-height %d is not above the report height %d", targetRepairHeight, reserve.Version)
	}
	diffs, err := inspectDiffs(reserve, prod)
	if err != nil {
		return kvrepair.Repair{}, err
	}
	if len(diffs) == 0 {
		return kvrepair.Repair{}, errors.New("the reports list no differences")
	}
	entries, err := buildRepairEntries(diffs)
	if err != nil {
		return kvrepair.Repair{}, err
	}
	r := kvrepair.Repair{
		Name:               name,
		ChainID:            chainID,
		TargetRepairHeight: targetRepairHeight,
		StateHeight:        reserve.Version,
		Source:             source,
		Entries:            entries,
	}
	if err := checkRepairLoads(r); err != nil {
		return kvrepair.Repair{}, err
	}
	return r, nil
}

// checkInspectPair returns an error unless reserve and prod are complete
// inspect lists of the same keys at the same height, from a semantic memiavl
// reserve and a FlatKV or composite production node.
func checkInspectPair(reserve, prod evmInspectJSON) error {
	switch {
	case reserve.Backend != "memiavl":
		return fmt.Errorf("reserve report backend is %q, want memiavl", reserve.Backend)
	case reserve.Mode != memiavlNormSemantic && reserve.Mode != memiavlModeSemanticReplay:
		return fmt.Errorf("reserve report mode is %q, want %s or %s; translator normalization shares the migration mapping",
			reserve.Mode, memiavlNormSemantic, memiavlModeSemanticReplay)
	case prod.Backend != "composite" && prod.Backend != "flatkv":
		return fmt.Errorf("production report backend is %q, want composite or flatkv", prod.Backend)
	case reserve.Version != prod.Version:
		return fmt.Errorf("reserve report is at height %d and production report at %d", reserve.Version, prod.Version)
	case reserve.InspectBucket != prod.InspectBucket:
		return fmt.Errorf("reserve report bucket is %q and production report bucket %q", reserve.InspectBucket, prod.InspectBucket)
	case !isFlatKVBucket(reserve.InspectBucket):
		return fmt.Errorf("unknown inspect bucket %q", reserve.InspectBucket)
	case reserve.KeyOffset != prod.KeyOffset || reserve.KeyPrefix != prod.KeyPrefix:
		return fmt.Errorf("reserve report filters offset %d prefix %q and production report offset %d prefix %q",
			reserve.KeyOffset, reserve.KeyPrefix, prod.KeyOffset, prod.KeyPrefix)
	}
	for _, side := range []struct {
		name   string
		report evmInspectJSON
	}{{"reserve", reserve}, {"production", prod}} {
		if !side.report.List {
			return fmt.Errorf("%s report is not an inspect list; run it with --list", side.name)
		}
		if side.report.Listed < 0 || uint64(side.report.Listed) != side.report.Matched {
			return fmt.Errorf("%s report lists %d of %d matched keys; run it with --list-limit 0",
				side.name, side.report.Listed, side.report.Matched)
		}
	}
	return nil
}

// evmDiff is one physical key whose logical value differs between the
// reserve and production reports. A nil value means the report has no row.
type evmDiff struct {
	bucket  string
	physKey []byte
	reserve []byte
	prod    []byte
}

// inspectDiffs returns every key that only one report lists, or that the two
// reports list with different logical values, sorted by key. It leaves out
// the FlatKV migration markers.
func inspectDiffs(reserve, prod evmInspectJSON) ([]evmDiff, error) {
	reserveRows, err := inspectRows(reserve.Entries)
	if err != nil {
		return nil, fmt.Errorf("reserve report: %w", err)
	}
	prodRows, err := inspectRows(prod.Entries)
	if err != nil {
		return nil, fmt.Errorf("production report: %w", err)
	}
	var diffs []evmDiff
	for key, reserveValue := range reserveRows {
		prodValue, ok := prodRows[key]
		if !ok || !bytes.Equal(reserveValue, prodValue) {
			diffs = append(diffs, evmDiff{bucket: reserve.InspectBucket, physKey: []byte(key), reserve: reserveValue, prod: prodValue})
		}
	}
	for key, prodValue := range prodRows {
		if _, ok := reserveRows[key]; !ok {
			diffs = append(diffs, evmDiff{bucket: reserve.InspectBucket, physKey: []byte(key), prod: prodValue})
		}
	}
	sort.Slice(diffs, func(i, j int) bool { return bytes.Compare(diffs[i].physKey, diffs[j].physKey) < 0 })
	return diffs, nil
}

// inspectRows decodes the listed rows into logical values keyed by physical
// key. A listed row with an empty logical value decodes to an empty, non-nil
// value.
func inspectRows(entries []evmInspectEntryJSON) (map[string][]byte, error) {
	rows := make(map[string][]byte, len(entries))
	for _, e := range entries {
		physKey, err := hex.DecodeString(e.Key)
		if err != nil {
			return nil, fmt.Errorf("key %q: %w", e.Key, err)
		}
		if bytes.Equal(physKey, migrationVersionPhysKey) || bytes.Equal(physKey, migrationBoundaryPhysKey) {
			continue
		}
		if !bytes.HasPrefix(physKey, evmPhysicalKeyPrefix) {
			return nil, fmt.Errorf("key %X is not an EVM row", physKey)
		}
		logical, err := hex.DecodeString(e.Logical)
		if err != nil {
			return nil, fmt.Errorf("key %X logical value %q: %w", physKey, e.Logical, err)
		}
		if _, ok := rows[string(physKey)]; ok {
			return nil, fmt.Errorf("key %X is listed twice", physKey)
		}
		rows[string(physKey)] = append([]byte{}, logical...)
	}
	return rows, nil
}

// buildRepairEntries maps each difference to evm store entries. An entry sets
// the reserve value, or deletes the key when the reserve value reads as
// absent, and states the production value as its old value.
func buildRepairEntries(diffs []evmDiff) ([]kvrepair.Entry, error) {
	var entries []kvrepair.Entry
	for _, d := range diffs {
		storeKey := d.physKey[len(evmPhysicalKeyPrefix):]
		if d.bucket == flatkvBucketAccount {
			fields, err := accountFieldDiffs(storeKey, d.reserve, d.prod)
			if err != nil {
				return nil, err
			}
			for _, f := range fields {
				entries = append(entries, repairEntry(f.key, f.reserve, f.prod))
			}
			continue
		}
		if err := checkStoreKeyBucket(d.bucket, storeKey); err != nil {
			return nil, err
		}
		entries = append(entries, repairEntry(storeKey, d.reserve, d.prod))
	}
	return entries, nil
}

func checkStoreKeyBucket(bucket string, storeKey []byte) error {
	kind, _ := keys.ParseEVMKey(storeKey)
	want := map[string]keys.EVMKeyKind{
		flatkvBucketStorage: keys.EVMKeyStorage,
		flatkvBucketCode:    keys.EVMKeyCode,
		flatkvBucketMisc:    keys.EVMKeyMisc,
	}[bucket]
	if kind != want {
		return fmt.Errorf("evm key %X does not belong to the %s bucket", storeKey, bucket)
	}
	return nil
}

func repairEntry(storeKey, reserve, prod []byte) kvrepair.Entry {
	e := kvrepair.Entry{Store: keys.EVMStoreKey, Key: append(kvrepair.HexBytes{}, storeKey...)}
	if !kvrepair.ReadsAsAbsent(keys.EVMStoreKey, storeKey, reserve) {
		value := kvrepair.HexBytes(reserve)
		e.New = &value
	}
	if kvrepair.ReadsAsAbsent(keys.EVMStoreKey, storeKey, prod) {
		e.OldAbsent = true
	} else {
		value := kvrepair.HexBytes(prod)
		e.Old = &value
	}
	return e
}

type accountFieldDiff struct {
	key     []byte
	reserve []byte
	prod    []byte
}

// accountFieldDiffs splits an account difference into its nonce and code hash
// keys, and returns the fields whose values differ. A missing account reads as
// all zeros. When only one report lists the account and every field is zero,
// it returns both fields, so that the repair deletes the row. It returns an
// error when the balances differ, because the evm store has no balance key.
func accountFieldDiffs(nonceKey, reserve, prod []byte) ([]accountFieldDiff, error) {
	kind, addr := keys.ParseEVMKey(nonceKey)
	if kind != keys.EVMKeyNonce {
		return nil, fmt.Errorf("evm key %X does not belong to the account bucket", nonceKey)
	}
	reserveFields, err := splitAccountLogical(nonceKey, reserve)
	if err != nil {
		return nil, err
	}
	prodFields, err := splitAccountLogical(nonceKey, prod)
	if err != nil {
		return nil, err
	}
	if !bytes.Equal(reserveFields[0], prodFields[0]) {
		return nil, fmt.Errorf("account %X balance differs, and the evm store has no balance key", nonceKey)
	}
	fieldKeys := [2][]byte{
		keys.BuildEVMKey(keys.EVMKeyNonce, addr),
		keys.BuildEVMKey(keys.EVMKeyCodeHash, addr),
	}
	var all, changed []accountFieldDiff
	for i, key := range fieldKeys {
		f := accountFieldDiff{key: key, reserve: reserveFields[i+1], prod: prodFields[i+1]}
		all = append(all, f)
		if !bytes.Equal(f.reserve, f.prod) {
			changed = append(changed, f)
		}
	}
	if len(changed) == 0 {
		return all, nil
	}
	return changed, nil
}

// splitAccountLogical returns the balance, nonce, and code hash in an account
// logical value, or three zero values when logical is nil.
func splitAccountLogical(nonceKey, logical []byte) ([3][]byte, error) {
	if logical == nil {
		logical = make([]byte, accountLogicalLen)
	}
	if len(logical) != accountLogicalLen {
		return [3][]byte{}, fmt.Errorf("account %X logical value has %d bytes, want %d", nonceKey, len(logical), accountLogicalLen)
	}
	return [3][]byte{logical[:32], logical[32:40], logical[40:]}, nil
}

// checkRepairLoads encodes r and returns an error unless a node would parse
// and validate the result.
func checkRepairLoads(r kvrepair.Repair) error {
	var encoded bytes.Buffer
	if err := kvrepair.Encode(&encoded, r); err != nil {
		return err
	}
	parsed, err := kvrepair.Parse(encoded.Bytes())
	if err != nil {
		return fmt.Errorf("the repair file does not parse: %w", err)
	}
	if err := parsed.Validate(func(store string) bool { return store == keys.EVMStoreKey }); err != nil {
		return fmt.Errorf("the repair file does not validate: %w", err)
	}
	return nil
}
