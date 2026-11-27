// Package cosmoslogical implements the seidb command that compares the logical
// state of the Cosmos modules across memIAVL, FlatKV, and a node that is
// migrating them from one to the other.
package cosmoslogical

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"slices"
	"sort"

	"github.com/sei-protocol/seilog"
	"github.com/spf13/cobra"

	"github.com/sei-protocol/sei-chain/sei-db/common/keys"
)

const (
	backendMemiavl   = "memiavl"
	backendFlatKV    = "flatkv"
	backendComposite = "composite"

	openModeSnapshot  = "snapshot"
	openModeChangelog = "changelog"
	openModeReplay    = "replay"

	defaultListLimit = 1000
)

// errIntegrity reports a digest that was computed but whose integrity counters are not clean.
var errIntegrity = errors.New("cosmos logical digest integrity check failed")

// progressOut receives narration, so that stdout carries only the report.
var progressOut io.Writer = os.Stderr

func progressf(format string, a ...any) { _, _ = fmt.Fprintf(progressOut, format, a...) }

// CosmosLogicalDigestCmd returns the seidb subcommand that digests the logical rows of every
// Cosmos module, per module, from a memIAVL node, a FlatKV node, or a node in migrate_cosmos.
// Two nodes with equal digests at one height hold the same Cosmos state.
func CosmosLogicalDigestCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cosmos-logical-digest",
		Short: "Digest the logical Cosmos module state of a memiavl, flatkv, or migrate_cosmos node, per module",
		Long: "Computes, per Cosmos module, a count and an order-independent digest of " +
			"XOR(sha256(len(k)||k||len(v)||v)) over k = \"<module>/<key>\" and v = the logical value " +
			"(the FlatKV height stamp is removed). The composite backend reads a migrate_cosmos node " +
			"as its migration router does: a key at or left of the boundary from FlatKV, any other " +
			"key from memiavl first and FlatKV second. The command exits non-zero when an integrity " +
			"counter is not clean.",
		Args:         cobra.NoArgs,
		RunE:         runCosmosLogicalDigest,
		SilenceUsage: true,
	}
	cmd.Flags().String("backend", "", "Backend to read: memiavl | flatkv | composite")
	cmd.Flags().StringP("db-dir", "d", "", "memiavl: the memiavl root dir (contains current/ and snapshot-*). flatkv: the flatkv data dir")
	cmd.Flags().String("flatkv-dir", "", "composite: the flatkv data dir")
	cmd.Flags().String("memiavl-dir", "", "composite: the memiavl root dir")
	cmd.Flags().Int64("height", 0, "Target version. flatkv replays its WAL to it; memiavl snapshot mode reads snapshot-<height> (0 = the current link)")
	cmd.Flags().String("memiavl-open-mode", openModeSnapshot, "memiavl read mode: snapshot (fast, needs a snapshot at --height) | changelog (snapshot at or below --height plus the changelog above it) | replay (slow, read-only DB replayed to --height)")
	cmd.Flags().StringSlice("module", nil, "Digest only these Cosmos modules (default: all)")
	cmd.Flags().Bool("list", false, "List the key/value rows of the single --module instead of only digesting it")
	cmd.Flags().String("key-prefix", "", "List mode: hex prefix that listed keys must start with")
	cmd.Flags().Int("list-limit", defaultListLimit, "List mode: maximum rows to print; <= 0 means no limit")
	cmd.Flags().Bool("json", false, "Write the report as one JSON object on stdout")
	return cmd
}

// options are the parsed flags of one run.
type options struct {
	backend    string
	dbDir      string
	flatKVDir  string
	memiavlDir string
	height     int64
	openMode   string
	modules    []string
	list       bool
	keyPrefix  []byte
	listLimit  int
	asJSON     bool
}

func parseOptions(cmd *cobra.Command) (options, error) {
	var o options
	o.backend, _ = cmd.Flags().GetString("backend")
	o.dbDir, _ = cmd.Flags().GetString("db-dir")
	o.flatKVDir, _ = cmd.Flags().GetString("flatkv-dir")
	o.memiavlDir, _ = cmd.Flags().GetString("memiavl-dir")
	o.height, _ = cmd.Flags().GetInt64("height")
	o.openMode, _ = cmd.Flags().GetString("memiavl-open-mode")
	o.list, _ = cmd.Flags().GetBool("list")
	o.listLimit, _ = cmd.Flags().GetInt("list-limit")
	o.asJSON, _ = cmd.Flags().GetBool("json")
	prefixHex, _ := cmd.Flags().GetString("key-prefix")
	selected, _ := cmd.Flags().GetStringSlice("module")

	switch o.backend {
	case backendMemiavl, backendFlatKV:
		if o.dbDir == "" {
			return o, fmt.Errorf("--backend %s needs --db-dir", o.backend)
		}
	case backendComposite:
		if o.flatKVDir == "" || o.memiavlDir == "" {
			return o, errors.New("--backend composite needs --flatkv-dir and --memiavl-dir")
		}
	default:
		return o, fmt.Errorf("--backend must be %s, %s or %s", backendMemiavl, backendFlatKV, backendComposite)
	}
	if o.height < 0 {
		return o, errors.New("--height must be at least 0")
	}

	all := cosmosModules()
	if len(selected) == 0 {
		o.modules = all
	} else {
		for _, m := range selected {
			if !slices.Contains(all, m) {
				return o, fmt.Errorf("--module %q is not a Cosmos module; want one of %v", m, all)
			}
		}
		o.modules = slices.Clone(selected)
		sort.Strings(o.modules)
		o.modules = slices.Compact(o.modules)
	}

	if prefixHex != "" {
		p, err := hex.DecodeString(prefixHex)
		if err != nil {
			return o, fmt.Errorf("--key-prefix: %w", err)
		}
		o.keyPrefix = p
	}
	if (o.keyPrefix != nil || cmd.Flags().Changed("list-limit")) && !o.list {
		return o, errors.New("--key-prefix and --list-limit need --list")
	}
	if o.list && len(o.modules) != 1 {
		return o, errors.New("--list needs exactly one --module")
	}
	return o, nil
}

// cosmosModules returns the memiavl modules that migrate_cosmos moves to FlatKV, ordered by name.
func cosmosModules() []string {
	modules, err := keys.AllModulesExcept(keys.EVMStoreKey)
	if err != nil {
		panic(err)
	}
	sort.Strings(modules)
	return modules
}

func runCosmosLogicalDigest(cmd *cobra.Command, _ []string) error {
	o, err := parseOptions(cmd)
	if err != nil {
		return err
	}
	if o.asJSON {
		// The storage layer logs to stdout; only an error-level line can still reach the report.
		seilog.SetDefaultLevel(slog.LevelError, true)
	}
	r, err := digest(o)
	if err != nil {
		return err
	}
	if err := writeReport(cmd.OutOrStdout(), r, o.asJSON); err != nil {
		return err
	}
	if !r.Integrity.clean() {
		return errIntegrity
	}
	return nil
}

// digest reads the backend that o names and returns its report.
func digest(o options) (report, error) {
	acc := newAccumulator(o)
	r := report{Backend: o.backend, RequestedHeight: o.height}
	var err error
	switch o.backend {
	case backendMemiavl:
		err = digestMemiavl(o, acc, &r)
	case backendFlatKV:
		err = digestFlatKV(o, acc, &r)
	default:
		err = digestComposite(o, acc, &r)
	}
	if err != nil {
		return report{}, err
	}
	acc.fill(&r)
	return r, nil
}

func digestMemiavl(o options, acc *accumulator, r *report) error {
	mem, err := openMemiavlSource(o.dbDir, o.height, o.openMode)
	if err != nil {
		return err
	}
	defer mem.close()
	describeMemiavl(r, mem)
	return scanMemiavlModules(mem, acc.modules, r, func(module string, key, value []byte) error {
		acc.add(module, key, value)
		return nil
	})
}

func digestFlatKV(o options, acc *accumulator, r *report) error {
	flat, err := openFlatKVSource(o.dbDir, o.height)
	if err != nil {
		return err
	}
	defer flat.close()
	describeFlatKV(r, flat)
	unknown, err := flat.scanCosmosRows(acc.selected, func(module string, key, value []byte) error {
		acc.add(module, key, value)
		return nil
	})
	r.Integrity.FlatKVRowsOfUnknownModules = unknown
	return err
}

// digestComposite reads a node in migrate_cosmos as its migration router does. FlatKV is scanned
// first: a row at or left of the boundary is final, and a row right of it is a write made after
// the migration started, held until memiavl is scanned, since memiavl wins for a key it holds.
func digestComposite(o options, acc *accumulator, r *report) error {
	flat, err := openFlatKVSource(o.flatKVDir, o.height)
	if err != nil {
		return err
	}
	defer flat.close()
	mem, err := openMemiavlSource(o.memiavlDir, o.height, o.openMode)
	if err != nil {
		return err
	}
	defer mem.close()
	describeFlatKV(r, flat)
	describeMemiavl(r, mem)
	if mem.version != flat.store.Version() {
		return fmt.Errorf("memiavl is at version %d and flatkv at %d; pick a --height both can open",
			mem.version, flat.store.Version())
	}

	boundary := flat.state.boundary
	pending := make(map[string][]byte)
	unknown, err := flat.scanCosmosRows(acc.selected, func(module string, key, value []byte) error {
		if boundary.IsMigrated(module, key) {
			acc.add(module, key, value)
			return nil
		}
		pending[physicalKey(module, key)] = bytes.Clone(value)
		return nil
	})
	r.Integrity.FlatKVRowsOfUnknownModules = unknown
	if err != nil {
		return err
	}
	progressf("  flatkv rows right of the boundary held for memiavl=%d\n", len(pending))

	if err := scanMemiavlModules(mem, acc.modules, r, func(module string, key, value []byte) error {
		if boundary.IsMigrated(module, key) {
			r.Integrity.MemiavlRowsLeftOfBoundary++
			return nil
		}
		pk := physicalKey(module, key)
		if _, ok := pending[pk]; ok {
			r.Integrity.KeysInBothStores++
			delete(pending, pk)
		}
		acc.add(module, key, value)
		return nil
	}); err != nil {
		return err
	}
	for pk, value := range pending {
		module, key := splitPhysicalKey(pk)
		acc.add(module, key, value)
	}
	return nil
}

// scanMemiavlModules streams the rows of every module in modules that mem holds, and records in r
// the trees that are absent and the trees that are not expected.
func scanMemiavlModules(mem *memiavlSource, modules []string, r *report, fn rowFunc) error {
	for _, tree := range mem.trees {
		if tree != keys.EVMStoreKey && !slices.Contains(cosmosModules(), tree) {
			r.Integrity.UnexpectedMemiavlTrees = append(r.Integrity.UnexpectedMemiavlTrees, tree)
		}
	}
	for _, module := range modules {
		if !slices.Contains(mem.trees, module) {
			r.AbsentMemiavlTrees = append(r.AbsentMemiavlTrees, module)
			continue
		}
		var rows uint64
		if err := mem.scan(module, func(key, value []byte) error {
			rows++
			if rows%20_000_000 == 0 {
				progressf("  memiavl %s rows read=%d\n", module, rows)
			}
			return fn(module, key, value)
		}); err != nil {
			return fmt.Errorf("scan memiavl %s: %w", module, err)
		}
		progressf("  memiavl %s rows=%d\n", module, rows)
	}
	return nil
}

func describeMemiavl(r *report, mem *memiavlSource) {
	r.MemiavlOpenMode = mem.openMode
	r.MemiavlSource = mem.description
	v := mem.version
	r.MemiavlVersion = &v
}

func describeFlatKV(r *report, flat *flatKVSource) {
	v := flat.store.Version()
	r.FlatKVVersion = &v
	r.MigrationBoundary = flat.state.boundary.String()
	if flat.state.version >= 0 {
		mv := flat.state.version
		r.MigrationVersion = &mv
	}
}

func physicalKey(module string, key []byte) string {
	return module + "/" + string(key)
}

func splitPhysicalKey(pk string) (string, []byte) {
	for i := 0; i < len(pk); i++ {
		if pk[i] == '/' {
			return pk[:i], []byte(pk[i+1:])
		}
	}
	return pk, nil
}

// moduleDigest is an order-independent accumulator over the rows of one module.
type moduleDigest struct {
	acc   [sha256.Size]byte
	count uint64
}

func (d *moduleDigest) addSum(sum [sha256.Size]byte) {
	for i := range sum {
		d.acc[i] ^= sum[i]
	}
	d.count++
}

// entryHash is sha256(len(k)||k||len(v)||v) with k = "<module>/<key>" and big-endian uint32
// lengths, the same unit evm-logical-digest uses.
func entryHash(module string, key, value []byte) [sha256.Size]byte {
	buf := make([]byte, 0, 9+len(module)+len(key)+len(value))
	buf = binary.BigEndian.AppendUint32(buf, uint32(len(module)+1+len(key))) //nolint:gosec
	buf = append(buf, module...)
	buf = append(buf, '/')
	buf = append(buf, key...)
	buf = binary.BigEndian.AppendUint32(buf, uint32(len(value))) //nolint:gosec
	buf = append(buf, value...)
	return sha256.Sum256(buf)
}

// accumulator collects the digests of the selected modules and, in list mode, their rows.
type accumulator struct {
	modules   []string
	digests   map[string]*moduleDigest
	list      bool
	keyPrefix []byte
	listLimit int
	matched   uint64
	entries   []entryReport
}

func newAccumulator(o options) *accumulator {
	a := &accumulator{
		modules:   o.modules,
		digests:   make(map[string]*moduleDigest, len(o.modules)),
		list:      o.list,
		keyPrefix: o.keyPrefix,
		listLimit: o.listLimit,
	}
	for _, m := range o.modules {
		a.digests[m] = &moduleDigest{}
	}
	return a
}

func (a *accumulator) selected(module string) bool {
	_, ok := a.digests[module]
	return ok
}

func (a *accumulator) add(module string, key, value []byte) {
	d, ok := a.digests[module]
	if !ok {
		return
	}
	d.addSum(entryHash(module, key, value))
	if !a.list || !bytes.HasPrefix(key, a.keyPrefix) {
		return
	}
	a.matched++
	if a.listLimit <= 0 || len(a.entries) < a.listLimit {
		a.entries = append(a.entries, entryReport{
			Key:   hex.EncodeToString(key),
			Value: hex.EncodeToString(value),
		})
	}
}

func (a *accumulator) fill(r *report) {
	var final moduleDigest
	for _, m := range a.modules {
		d := a.digests[m]
		r.Modules = append(r.Modules, moduleReport{Module: m, Count: d.count, Digest: fmt.Sprintf("%X", d.acc)})
		for i := range d.acc {
			final.acc[i] ^= d.acc[i]
		}
		final.count += d.count
	}
	r.Final = moduleReport{Module: "final", Count: final.count, Digest: fmt.Sprintf("%X", final.acc)}
	if a.list {
		sort.Slice(a.entries, func(i, j int) bool { return a.entries[i].Key < a.entries[j].Key })
		matched := a.matched
		r.ListMatched = &matched
		r.Entries = a.entries
	}
}

// report is the result of one run.
type report struct {
	Backend           string `json:"backend"`
	RequestedHeight   int64  `json:"requested_height"`
	MemiavlOpenMode   string `json:"memiavl_open_mode,omitempty"`
	MemiavlSource     string `json:"memiavl_source,omitempty"`
	MemiavlVersion    *int64 `json:"memiavl_version,omitempty"`
	FlatKVVersion     *int64 `json:"flatkv_version,omitempty"`
	MigrationBoundary string `json:"migration_boundary,omitempty"`
	MigrationVersion  *int64 `json:"migration_version,omitempty"`
	// AbsentMemiavlTrees names the selected modules that have no memiavl tree. It is not an
	// integrity failure: a tree can be absent from a node that no longer keeps it.
	AbsentMemiavlTrees []string        `json:"absent_memiavl_trees,omitempty"`
	Modules            []moduleReport  `json:"modules"`
	Final              moduleReport    `json:"final"`
	Integrity          integrityReport `json:"integrity"`
	ListMatched        *uint64         `json:"list_matched,omitempty"`
	Entries            []entryReport   `json:"entries,omitempty"`
}

type moduleReport struct {
	Module string `json:"module"`
	Count  uint64 `json:"count"`
	Digest string `json:"digest"`
}

type entryReport struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// integrityReport counts the rows that a correct node does not have. Every counter is 0 on a
// clean node.
type integrityReport struct {
	// UnexpectedMemiavlTrees names memiavl trees that are neither EVM nor a Cosmos module, such as
	// a store an upgrade deleted.
	UnexpectedMemiavlTrees []string `json:"unexpected_memiavl_trees"`
	// MemiavlRowsLeftOfBoundary counts memiavl rows that the migration has already moved, and so
	// must have deleted.
	MemiavlRowsLeftOfBoundary uint64 `json:"memiavl_rows_left_of_boundary"`
	// KeysInBothStores counts keys right of the boundary that both memiavl and FlatKV hold.
	KeysInBothStores uint64 `json:"keys_in_both_stores"`
	// FlatKVRowsOfUnknownModules counts FlatKV rows that name no known module.
	FlatKVRowsOfUnknownModules uint64 `json:"flatkv_rows_of_unknown_modules"`
}

func (r integrityReport) clean() bool {
	return len(r.UnexpectedMemiavlTrees) == 0 && r.MemiavlRowsLeftOfBoundary == 0 &&
		r.KeysInBothStores == 0 && r.FlatKVRowsOfUnknownModules == 0
}

func writeReport(w io.Writer, r report, asJSON bool) error {
	if r.Integrity.UnexpectedMemiavlTrees == nil {
		r.Integrity.UnexpectedMemiavlTrees = []string{}
	}
	if asJSON {
		data, err := json.Marshal(r)
		if err != nil {
			return err
		}
		_, err = fmt.Fprintf(w, "%s\n", data)
		return err
	}
	var b bytes.Buffer
	fmt.Fprintf(&b, "backend=%s requested_height=%d\n", r.Backend, r.RequestedHeight)
	if r.MemiavlVersion != nil {
		fmt.Fprintf(&b, "memiavl: version=%d mode=%s source=%s\n", *r.MemiavlVersion, r.MemiavlOpenMode, r.MemiavlSource)
	}
	if r.FlatKVVersion != nil {
		mv := "none"
		if r.MigrationVersion != nil {
			mv = fmt.Sprint(*r.MigrationVersion)
		}
		fmt.Fprintf(&b, "flatkv: version=%d migration_version=%s boundary=%s\n", *r.FlatKVVersion, mv, r.MigrationBoundary)
	}
	if len(r.AbsentMemiavlTrees) > 0 {
		fmt.Fprintf(&b, "absent memiavl trees: %v\n", r.AbsentMemiavlTrees)
	}
	for _, m := range r.Modules {
		fmt.Fprintf(&b, "module=%-12s count=%d digest=%s\n", m.Module, m.Count, m.Digest)
	}
	fmt.Fprintf(&b, "FINAL count=%d digest=%s\n", r.Final.Count, r.Final.Digest)
	in := r.Integrity
	fmt.Fprintf(&b, "integrity clean=%t unexpected_memiavl_trees=%v memiavl_rows_left_of_boundary=%d keys_in_both_stores=%d flatkv_rows_of_unknown_modules=%d\n",
		in.clean(), in.UnexpectedMemiavlTrees, in.MemiavlRowsLeftOfBoundary, in.KeysInBothStores, in.FlatKVRowsOfUnknownModules)
	if r.ListMatched != nil {
		fmt.Fprintf(&b, "list matched=%d listed=%d\n", *r.ListMatched, len(r.Entries))
		for _, e := range r.Entries {
			fmt.Fprintf(&b, "  key=%s value=%s\n", e.Key, e.Value)
		}
	}
	_, err := w.Write(b.Bytes())
	return err
}
