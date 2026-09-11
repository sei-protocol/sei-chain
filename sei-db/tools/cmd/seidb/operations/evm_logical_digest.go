package operations

import (
	"bufio"
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
	"path/filepath"
	"sort"

	"github.com/sei-protocol/seilog"

	"github.com/sei-protocol/sei-chain/sei-db/common/keys"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/ktype"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/vtype"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/memiavl"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/migration"
	"github.com/sei-protocol/sei-chain/sei-db/wal"
	"github.com/spf13/cobra"
)

// migrationVersionPhysKey is the FlatKV physical key of the migration-version
// marker. FlatKV stores non-EVM module rows as "<module>/<key>"; the
// MigrationManager writes this marker only to the new database (flatkv) and
// never to memiavl. It therefore shows up in the FlatKV misc bucket but is
// absent from a memiavl-only node, so excluding it lets the FlatKV digest be
// compared apples-to-apples against memiavl-only output.
var migrationVersionPhysKey = []byte(migration.MigrationStore + "/" + migration.MigrationVersionKey)

// migrationBoundaryPhysKey is the FlatKV physical key of the in-progress
// migration cursor. Like migrationVersionPhysKey it is a FlatKV-only
// MigrationStore row that a memiavl-only node never owns, but it is present only
// while a migration is in flight (the MigrationManager deletes it on completion,
// atomically writing MigrationVersionKey instead). Excluding it lets an
// in-progress node's FlatKV/composite digest compare apples-to-apples against a
// completed or memiavl-only node, which carry no boundary row.
var migrationBoundaryPhysKey = []byte(migration.MigrationStore + "/" + migration.MigrationBoundaryKey)

// memiavl-open-mode and memiavl-normalization flag values, named so they are not
// repeated as bare string literals (goconst).
const (
	memiavlOpenModeSnapshot = "snapshot"
	memiavlOpenModeReplay   = "replay"
	memiavlNormSemantic     = "semantic"
	memiavlNormIndependent  = "independent"
	memiavlNormTranslator   = "translator"
)

// EvmLogicalDigestCmd computes a backend-independent digest of the EVM logical
// state (account / code / storage canonical buckets) so a memIAVL node and a
// FlatKV node can be compared at the same chain height.
//
// Why "logical" and not a raw physical digest: every FlatKV value embeds a
// per-key blockHeight stamp (the height the key was last written / migrated).
// A freshly migrated FlatKV node stamps migration-time heights, which differ
// from the memIAVL leaf versions, so a byte-for-byte physical digest would
// diverge even when the underlying EVM state is identical. This tool strips
// the serialization-version + blockHeight header on both sides and digests
// only the logical payload (storage word / bytecode / balance+nonce+codehash).
//
// Both sides are normalized to FlatKV physical keys:
//   - FlatKV: keys come straight from RawGlobalIterator.
//   - memIAVL semantic mode (default): raw EVM leaves are independently decoded
//     into the same logical account / code / storage / misc buckets.
//   - memIAVL translator mode: each EVM leaf is fed through
//     flatkv.ImportTranslator, which applies the same classifyAndPrefix +
//     account-merge logic FlatKV uses.
//
// The per-bucket accumulator is an XOR of sha256(len(key)||key||len(val)||val),
// which is order-independent: it does not matter that FlatKV iterates in pebble
// global order while memIAVL is scanned by leaf index, nor that merged accounts
// are flushed out of order at Finalize.
//
// Performance / mode selection (memiavl side only; --memiavl-open-mode):
//   - snapshot (default, FAST): sequentially scans the completed snapshot kvs
//     file at snapshot-<height>/evm. Requires an on-disk memiavl snapshot AT
//     that exact height (or --height 0 for the current symlink). This is the
//     preferred mode whenever the target height lines up with an existing
//     snapshot boundary.
//   - replay (SLOW): opens a read-only DB, replays the changelog up to
//     --height, then walks the in-memory/mmap tree. Roughly an order of
//     magnitude slower than snapshot (changelog replay + per-leaf tree walk
//     instead of a sequential file read). Use it only when no snapshot exists
//     at the target height — e.g. nodes whose snapshot rewrite lags the tip, so
//     an arbitrary comparison height has no snapshot-<height> on disk. On a live
//     node this mode can read a changelog record the node is midway through
//     writing; it then reports that and asks for a rerun rather than repairing
//     the changelog under its writer.
//
// The flatkv side is always a pebble WAL-replay-to-height and is fast
// regardless. So when comparing across nodes, pick a height that is an existing
// memiavl snapshot on every node and use snapshot mode; fall back to replay only
// when no such common height is reachable within each backend's retained window.
//
// The primary comparison is account+code+storage. The misc bucket is printed
// separately, plus marker-adjusted comparison lines, because FlatKV can contain
// FlatKV-only MigrationStore rows that a memiavl-only truth node never owns: the
// migration-version marker (present once a migration completes) and the
// migration-boundary cursor (present only while a migration is in flight). Both
// are XORed out of the misc bucket for the final comparison so that memiavl,
// mid-migration, and completed nodes all agree.
//
// Usage:
//
//	# FlatKV digest at a height (WAL-replays to it). Prints per-bucket
//	# bucket_digest values and one FINAL_DIGEST line for backend comparison.
//	# FlatKV's internal migration-version marker is omitted from that comparison.
//	seidb evm-logical-digest --backend flatkv \
//	    --db-dir /.sei/data/state_commit/flatkv --height 213200000
//
//	# memIAVL digest at the same height (0 = current symlink), using the
//	# default semantic + snapshot mode: independently decodes raw EVM keys
//	# without flatkv.ImportTranslator, reading the completed snapshot kvs file
//	# at snapshot-<height>/evm (or current/evm). This is the fast path and
//	# requires a snapshot at that exact height.
//	seidb evm-logical-digest --backend memiavl \
//	    --db-dir /.sei/data/state_commit/memiavl --height 213200000
//
//	# Same, but for a height with no on-disk snapshot (e.g. snapshot rewrite
//	# lags the tip): replay the changelog to the height first. Slower — prefer
//	# snapshot mode whenever the height matches an existing snapshot boundary.
//	seidb evm-logical-digest --backend memiavl --memiavl-open-mode replay \
//	    --db-dir /.sei/data/state_commit/memiavl --height 213205000
//
//	# Mid-migration node: digest the full EVM logical view as the union of
//	# flatkv (migrated rows) and memiavl (rows not yet past the boundary), to
//	# compare a migrating node against a memiavl-only node at the same height.
//	# Use --memiavl-open-mode replay when memiavl's retained snapshot height is
//	# outside flatkv's retained snapshot window (the common case on a live
//	# migrating node, where the two backends keep snapshots at different heights).
//	seidb evm-logical-digest --backend composite --memiavl-open-mode replay \
//	    --flatkv-dir /.sei/data/state_commit/flatkv \
//	    --memiavl-dir /.sei/data/state_commit/memiavl --height 213200000
//
//	# Translator-based memIAVL digest. This proves FlatKV state matches the
//	# current migration mapping and is useful when debugging ImportTranslator.
//	seidb evm-logical-digest --backend memiavl \
//	    --db-dir /.sei/data/state_commit/memiavl --height 213200000 \
//	    --memiavl-normalization translator
//
//	# Compare a migrated FlatKV node against a memiavl-only node at height H:
//	#   FlatKV FINAL_DIGEST account+code+storage+misc digest=... == memiavl FINAL_DIGEST account+code+storage+misc digest=...
//
//	# Inspect one bucket instead of the global digest (e.g. list storage rows
//	# under a key prefix, sharded by the next 2 bytes):
//	seidb evm-logical-digest --backend flatkv -d <dir> --height H \
//	    --inspect-bucket storage --key-prefix 03 --shard-next-bytes 2
//	seidb evm-logical-digest --backend flatkv -d <dir> --height H \
//	    --inspect-bucket account --list --list-limit 50 --details
//
//	# Hunt the single diverging entry between two runs: when two bucket_digest
//	# values differ by exactly one row, XOR those two 32-byte hex values and
//	# pass the result; every matching row is printed as FOUND-HASH.
//	seidb evm-logical-digest --backend flatkv -d <dir> --height H \
//	    --find-hash <32-byte-hex>
func EvmLogicalDigestCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "evm-logical-digest",
		Short: "Backend-independent digest of EVM logical state (account/code/storage) for memiavl vs flatkv comparison",
		RunE:  runEvmLogicalDigest,
	}
	cmd.Flags().String("backend", "", "Backend to read: flatkv | memiavl | composite")
	cmd.Flags().StringP("db-dir", "d", "", "For flatkv: the flatkv data dir. For memiavl: the memiavl root dir (contains current/ and snapshot-* )")
	cmd.Flags().String("flatkv-dir", "", "Composite mode: flatkv data dir")
	cmd.Flags().String("memiavl-dir", "", "Composite mode: memiavl root dir (contains current/ and snapshot-* )")
	cmd.Flags().Int64("height", 0, "Target version. flatkv WAL-replays to it; memiavl resolves snapshot-<height>/evm (0 = current symlink)")
	cmd.Flags().String("memiavl-open-mode", memiavlOpenModeSnapshot, "memiavl read mode: snapshot (FAST: sequential scan of the completed snapshot kvs file; requires an on-disk snapshot at --height, or --height 0 for current) | replay (SLOW, ~10x: replays changelog to --height then walks the mmap tree; use only when no snapshot exists at the target height). Prefer snapshot when --height matches an existing snapshot boundary")
	cmd.Flags().String("memiavl-normalization", memiavlNormSemantic, "memiavl digest/inspect normalization: semantic/independent (raw EVM key/value decoder) | translator (current migration mapping)")
	cmd.Flags().String("inspect-bucket", "", "Inspect one normalized bucket (account|code|storage|misc) instead of printing the global digest")
	cmd.Flags().Int("key-offset", 0, "Inspect mode: byte offset into physical key before applying --key-prefix / sharding")
	cmd.Flags().String("key-prefix", "", "Inspect mode: hex prefix, relative to --key-offset, used to filter physical keys")
	cmd.Flags().Int("shard-next-bytes", 0, "Inspect mode: group matching keys by this many bytes after --key-prefix")
	cmd.Flags().Bool("list", false, "Inspect mode: list matching key/logical-value pairs instead of shard bucket_digest values")
	cmd.Flags().Int("list-limit", 1000, "Inspect mode: maximum pairs to print with --list; <=0 means unlimited")
	cmd.Flags().Bool("details", false, "Inspect list mode: include backend-specific version metadata")
	cmd.Flags().String("find-hash", "", "Optional 32-byte hex per-entry hash to hunt for. When two bucket_digest values differ by exactly one entry, their XOR IS that entry's hash; this prints every entry whose sha256(len(key)||key||len(val)||val) matches")
	cmd.Flags().Bool("json", false, "Emit the report as one JSON object on stdout and send the narration to stderr, for callers that check digests on a schedule instead of reading them. Storage-layer logging is quieted to error level so it leaves stdout; set SEI_LOG_OUTPUT=stderr to move an error-level line off stdout too. Not valid with --inspect-bucket, which produces no report")
	return cmd
}

// digestSink is where this command's output goes. prose takes the narration —
// the start banner, scan progress, and the human-readable report. jsonReport is
// nil in the default text mode and, in JSON mode, takes the encoded report while
// prose moves to stderr, so stdout carries only the object.
//
// Narration moves rather than being discarded, so a scan that runs for minutes
// still reports progress to whoever is watching.
type digestSink struct {
	prose      io.Writer
	jsonReport io.Writer
}

// sink is a package variable because the text-or-JSON choice is made once per
// process, in runEvmLogicalDigest, while the lines it governs are emitted from a
// dozen call sites, several of which hold neither the print context nor the
// accumulator. One switch at the single decision point is checkable; a flag
// threaded through every scan helper is a parameter each new helper can forget.
var sink = digestSink{prose: os.Stdout}

// sayf writes one narration line.
//
// Narration is advisory, so a failed write is dropped rather than returned. The
// write that realistically fails is a closed pipe, from an operator ending a
// `| head`, and aborting a scan that is still producing a correct digest over
// that would be the worse outcome. The report itself does report write errors:
// see emit.
func (s digestSink) sayf(format string, a ...any) {
	_, _ = fmt.Fprintf(s.prose, format, a...)
}

// say writes one narration line from already-formatted parts, or a blank line
// when called with none.
func (s digestSink) say(a ...any) {
	_, _ = fmt.Fprintln(s.prose, a...)
}

// digestBucket is an order-independent accumulator over (key, logical-value)
// pairs for one canonical EVM bucket.
type digestBucket struct {
	acc   [sha256.Size]byte
	count uint64
}

// entryHash is the per-entry digest unit shared by all buckets:
// sha256(len(key)||key||len(val)||val), lengths big-endian uint32.
func entryHash(physKey, logicalVal []byte) (sum [sha256.Size]byte) {
	h := sha256.New()
	var lenbuf [4]byte
	binary.BigEndian.PutUint32(lenbuf[:], uint32(len(physKey))) //nolint:gosec
	_, _ = h.Write(lenbuf[:])
	_, _ = h.Write(physKey)
	binary.BigEndian.PutUint32(lenbuf[:], uint32(len(logicalVal))) //nolint:gosec
	_, _ = h.Write(lenbuf[:])
	_, _ = h.Write(logicalVal)
	copy(sum[:], h.Sum(nil))
	return sum
}

func (b *digestBucket) add(physKey, logicalVal []byte) {
	b.addSum(entryHash(physKey, logicalVal))
}

func (b *digestBucket) addSum(sum [sha256.Size]byte) {
	for i := 0; i < sha256.Size; i++ {
		b.acc[i] ^= sum[i]
	}
	b.count++
}

type evmDigest struct {
	account digestBucket
	code    digestBucket
	storage digestBucket
	misc    digestBucket
	census  evmZeroCensus

	// findTarget, when non-nil, is a per-entry hash to hunt for; every
	// matching entry is printed with its bucket, physical key, and values.
	findTarget []byte

	// migrationVersionFound/Hash capture the FlatKV-only
	// "migration/migration-version" marker. It is folded into the misc
	// bucket like any other row, but tracked separately so print can also
	// report a variant with it XORed back out. A memiavl-only node never
	// owns this key, so that "excl migration-version" variant is what
	// should match memiavl-only output exactly.
	migrationVersionFound bool
	migrationVersionHash  [sha256.Size]byte

	// migrationBoundaryFound/Hash capture the FlatKV-only
	// "migration/migration-boundary" cursor, present only while a migration is
	// in flight. It is folded into the misc bucket like any other row but
	// tracked separately so print can XOR it back out — mirroring the
	// migration-version handling — so an in-progress node matches a completed
	// or memiavl-only node.
	migrationBoundaryFound bool
	migrationBoundaryHash  [sha256.Size]byte
}

type evmZeroCensus struct {
	ZeroAccounts uint64 `json:"zero_accounts"`
	// ZeroCodeHashRows counts code-hash rows that memiavl holds with an all-zero value.
	// A row missing altogether is absent on both backends and is counted separately, so
	// this is the only population whose presence FlatKV normalization can change.
	ZeroCodeHashRows                uint64 `json:"zero_codehash_rows"`
	LiveAccountsWithZeroCodeHashRow uint64 `json:"live_accounts_with_zero_codehash_row"`
	LiveAccountsWithoutCodeHashRow  uint64 `json:"live_accounts_without_codehash_row"`
	EmptyCodeValues                 uint64 `json:"empty_code_values"`
	ZeroStorageSlots                uint64 `json:"zero_storage_slots"`
}

type digestPrintContext struct {
	backend         string
	mode            string
	dbDir           string
	source          string
	normalization   string
	requestedHeight int64
	version         int64
}

// consume routes a physical (key, serialized-value) pair into its canonical
// bucket, strips the vtype header, and folds the logical payload into the
// accumulator. The misc bucket (EVM keys with no canonical prefix: address
// mappings, codesize, etc.) is digested too — its MiscData value is just as
// height-independent (version+blockHeight header stripped) as the other three.
func (d *evmDigest) consume(physKey, val []byte) error {
	bucket, logical, err := normalizeEVMFlatKVPair(physKey, val)
	if err != nil {
		return err
	}
	d.addLogical(bucket, physKey, logical, val)
	return nil
}

func (d *evmDigest) addLogical(bucket string, physKey, logical, rawVal []byte) {
	sum := entryHash(physKey, logical)
	if d.findTarget != nil && bytes.Equal(sum[:], d.findTarget) {
		sink.sayf("FOUND-HASH bucket=%s keyhex=%X logicalhex=%X rawhex=%X\n", bucket, physKey, logical, rawVal)
	}
	switch bucket {
	case flatkvBucketAccount:
		d.account.addSum(sum)
	case flatkvBucketCode:
		d.code.addSum(sum)
	case flatkvBucketStorage:
		d.storage.addSum(sum)
	default: // flatkvBucketMisc
		d.misc.addSum(sum)
		if bytes.Equal(physKey, migrationVersionPhysKey) {
			d.migrationVersionFound = true
			d.migrationVersionHash = sum
		}
		if bytes.Equal(physKey, migrationBoundaryPhysKey) {
			d.migrationBoundaryFound = true
			d.migrationBoundaryHash = sum
		}
	}
}

func normalizeEVMFlatKVPair(physKey, val []byte) (string, []byte, error) {
	switch bucket := classifyFlatKVPhysicalKey(physKey); bucket {
	case flatkvBucketAccount:
		ad, err := vtype.DeserializeAccountData(val)
		if err != nil {
			return "", nil, fmt.Errorf("deserialize account %X: %w", physKey, err)
		}
		// Logical account payload, height-independent: balance(32)||nonce(8)||codeHash(32).
		logical := make([]byte, 0, 72)
		logical = append(logical, ad.GetBalance()[:]...)
		var nonce [8]byte
		binary.BigEndian.PutUint64(nonce[:], ad.GetNonce())
		logical = append(logical, nonce[:]...)
		logical = append(logical, ad.GetCodeHash()[:]...)
		return bucket, logical, nil
	case flatkvBucketCode:
		cd, err := vtype.DeserializeCodeData(val)
		if err != nil {
			return "", nil, fmt.Errorf("deserialize code %X: %w", physKey, err)
		}
		return bucket, cd.GetBytecode(), nil
	case flatkvBucketStorage:
		sd, err := vtype.DeserializeStorageData(val)
		if err != nil {
			return "", nil, fmt.Errorf("deserialize storage %X: %w", physKey, err)
		}
		value := sd.GetValue()
		return bucket, value[:], nil
	default: // flatkvBucketMisc
		ld, err := vtype.DeserializeMiscData(val)
		if err != nil {
			return "", nil, fmt.Errorf("deserialize misc %X: %w", physKey, err)
		}
		return bucket, ld.GetValue(), nil
	}
}

// miscForCompare returns the misc bucket accumulator and count with the
// FlatKV-only MigrationStore marker rows XORed back out: the migration-version
// marker (present on a completed node) and the migration-boundary cursor
// (present on an in-progress node). A memiavl-only node owns neither, so after
// this adjustment memiavl, mid-migration, and completed nodes all produce the
// same misc digest for identical EVM state.
func (d *evmDigest) miscForCompare() (acc [sha256.Size]byte, count uint64) {
	acc = d.misc.acc
	count = d.misc.count
	if d.migrationVersionFound {
		for i := 0; i < sha256.Size; i++ {
			acc[i] ^= d.migrationVersionHash[i]
		}
		count--
	}
	if d.migrationBoundaryFound {
		for i := 0; i < sha256.Size; i++ {
			acc[i] ^= d.migrationBoundaryHash[i]
		}
		count--
	}
	return acc, count
}

// evmDigestBucketJSON is one bucket's entry count and accumulator.
//
// Digest is uppercase hex, matching the text report's %X so a value read out of
// JSON compares equal to the same value read out of the prose without either
// caller having to normalize case.
type evmDigestBucketJSON struct {
	Count  uint64 `json:"count"`
	Digest string `json:"digest"`
}

// evmDigestJSON is the machine-readable form of a digest report: the same
// numbers the text report prints, named so a caller does not parse prose.
type evmDigestJSON struct {
	Backend         string `json:"backend"`
	Mode            string `json:"mode,omitempty"`
	DBDir           string `json:"db_dir"`
	Source          string `json:"source"`
	Normalization   string `json:"normalization"`
	RequestedHeight int64  `json:"requested_height"`
	Version         int64  `json:"version"`

	// MarkerAdjustments names the FlatKV-only MigrationStore rows XORed back
	// out of the misc bucket. It is what distinguishes a mid-migration reading
	// from a completed one, so a caller can tell them apart without inferring
	// it from the counts.
	MarkerAdjustments []string `json:"marker_adjustments"`

	ZeroCensus evmZeroCensus `json:"zero_census"`

	Account evmDigestBucketJSON `json:"account"`
	Code    evmDigestBucketJSON `json:"code"`
	Storage evmDigestBucketJSON `json:"storage"`
	Misc    evmDigestBucketJSON `json:"misc"`

	// Final is the digest the two backends are compared on: sha256 over the
	// four bucket accumulators, with Count their sum.
	Final evmDigestBucketJSON `json:"final"`
}

// report reduces the accumulators to the values both output forms render, so the
// text report and the JSON object cannot drift apart.
func (d *evmDigest) report(ctx digestPrintContext) evmDigestJSON {
	miscAcc, miscCount := d.miscForCompare()

	markers := make([]string, 0, 2)
	if d.migrationVersionFound {
		markers = append(markers, migration.MigrationStore+"/"+migration.MigrationVersionKey)
	}
	if d.migrationBoundaryFound {
		markers = append(markers, migration.MigrationStore+"/"+migration.MigrationBoundaryKey)
	}

	combined := sha256.New()
	_, _ = combined.Write(d.account.acc[:])
	_, _ = combined.Write(d.code.acc[:])
	_, _ = combined.Write(d.storage.acc[:])
	_, _ = combined.Write(miscAcc[:])

	return evmDigestJSON{
		Backend:           ctx.backend,
		Mode:              ctx.mode,
		DBDir:             ctx.dbDir,
		Source:            ctx.source,
		Normalization:     ctx.normalization,
		RequestedHeight:   ctx.requestedHeight,
		Version:           ctx.version,
		MarkerAdjustments: markers,
		ZeroCensus:        d.census,
		Account:           evmDigestBucketJSON{Count: d.account.count, Digest: fmt.Sprintf("%X", d.account.acc)},
		Code:              evmDigestBucketJSON{Count: d.code.count, Digest: fmt.Sprintf("%X", d.code.acc)},
		Storage:           evmDigestBucketJSON{Count: d.storage.count, Digest: fmt.Sprintf("%X", d.storage.acc)},
		Misc:              evmDigestBucketJSON{Count: miscCount, Digest: fmt.Sprintf("%X", miscAcc)},
		Final: evmDigestBucketJSON{
			Count:  d.account.count + d.code.count + d.storage.count + miscCount,
			Digest: fmt.Sprintf("%X", combined.Sum(nil)),
		},
	}
}

// emit renders the finished digest in whichever form the sink is configured for.
func (d *evmDigest) emit(ctx digestPrintContext) error {
	if sink.jsonReport != nil {
		return encodeDigestJSON(sink.jsonReport, d.report(ctx))
	}
	d.print(ctx)
	return nil
}

// encodeDigestJSON writes the report as one line, so a caller can read a run's
// result with a single line-oriented read rather than framing the stream.
func encodeDigestJSON(w io.Writer, r evmDigestJSON) error {
	enc := json.NewEncoder(w)
	return enc.Encode(r)
}

func (d *evmDigest) print(ctx digestPrintContext) {
	r := d.report(ctx)

	sink.say("EVM logical digest report")
	printDigestContext(ctx)
	sink.say()

	for _, marker := range r.MarkerAdjustments {
		sink.sayf("flatkv_marker_adjustment: omitted %s from misc bucket in final result\n", marker)
		sink.say()
	}

	if r.ZeroCensus != (evmZeroCensus{}) {
		sink.say("Zero-value memiavl census")
		sink.sayf("zero_accounts=%d zero_codehash_rows=%d empty_code_values=%d zero_storage_slots=%d\n",
			r.ZeroCensus.ZeroAccounts,
			r.ZeroCensus.ZeroCodeHashRows,
			r.ZeroCensus.EmptyCodeValues,
			r.ZeroCensus.ZeroStorageSlots)
		sink.sayf("live_accounts_with_zero_codehash_row=%d live_accounts_without_codehash_row=%d\n",
			r.ZeroCensus.LiveAccountsWithZeroCodeHashRow,
			r.ZeroCensus.LiveAccountsWithoutCodeHashRow)
		sink.say()
	}

	sink.say("Bucket digests (final digest inputs)")
	sink.sayf("account  count=%d bucket_digest=%s\n", r.Account.Count, r.Account.Digest)
	sink.sayf("code     count=%d bucket_digest=%s\n", r.Code.Count, r.Code.Digest)
	sink.sayf("storage  count=%d bucket_digest=%s\n", r.Storage.Count, r.Storage.Digest)
	sink.sayf("misc   count=%d bucket_digest=%s\n", r.Misc.Count, r.Misc.Digest)

	sink.say()
	sink.sayf("FINAL_DIGEST account+code+storage+misc count=%d digest=%s\n", r.Final.Count, r.Final.Digest)
}

func printDigestStart(ctx digestPrintContext) {
	sink.say("EVM logical digest start")
	printDigestContext(ctx)
	sink.say()
}

func printDigestContext(ctx digestPrintContext) {
	sink.sayf("backend: %s\n", ctx.backend)
	if ctx.mode != "" {
		sink.sayf("mode: %s\n", ctx.mode)
	}
	sink.sayf("db_dir: %s\n", ctx.dbDir)
	sink.sayf("source: %s\n", ctx.source)
	sink.sayf("requested_height: %d\n", ctx.requestedHeight)
	sink.sayf("version: %d\n", ctx.version)
	sink.sayf("normalization: %s\n", ctx.normalization)
}

// enterJSONMode redirects this command's narration to stderr so stdout carries
// only the encoded report, and raises the storage layer's log level, which
// writes to stdout and would otherwise put several lines in front of the object.
//
// Raising the level rather than redirecting is what is available: seilog fixes
// its destination when the process starts and exposes no runtime setter. That
// clears every line a healthy run emits, all of them informational, but an
// error-level line would still reach stdout. SEI_LOG_OUTPUT=stderr, which the
// flag help names, closes that remainder.
func enterJSONMode() {
	sink.prose = os.Stderr
	sink.jsonReport = os.Stdout
	seilog.SetDefaultLevel(slog.LevelError, true)
}

func runEvmLogicalDigest(cmd *cobra.Command, _ []string) error {
	asJSON, _ := cmd.Flags().GetBool("json")
	backend, _ := cmd.Flags().GetString("backend")
	dbDir, _ := cmd.Flags().GetString("db-dir")
	flatKVDir, _ := cmd.Flags().GetString("flatkv-dir")
	memIAVLDir, _ := cmd.Flags().GetString("memiavl-dir")
	height, _ := cmd.Flags().GetInt64("height")
	if dbDir == "" && backend != "composite" {
		return errors.New("must provide --db-dir")
	}
	inspectBucket, _ := cmd.Flags().GetString("inspect-bucket")
	memiavlNormalization, _ := cmd.Flags().GetString("memiavl-normalization")
	memiavlOpenMode, _ := cmd.Flags().GetString("memiavl-open-mode")
	if inspectBucket != "" {
		if memiavlOpenMode != memiavlOpenModeSnapshot {
			return fmt.Errorf("--inspect-bucket does not support --memiavl-open-mode=%q yet", memiavlOpenMode)
		}
		// Refusing beats emitting nothing on stdout. --json names one shape, the
		// digest report, and inspect mode does not produce that report at all.
		if asJSON {
			return errors.New("--json applies to the digest report and --inspect-bucket does not produce one; drop one of the two")
		}
		return runEvmLogicalInspect(cmd, backend, dbDir, height, inspectBucket, memiavlNormalization)
	}
	if asJSON {
		enterJSONMode()
	}

	findHashHex, _ := cmd.Flags().GetString("find-hash")
	var findTarget []byte
	if findHashHex != "" {
		var err error
		findTarget, err = hex.DecodeString(findHashHex)
		if err != nil {
			return fmt.Errorf("decode --find-hash: %w", err)
		}
		if len(findTarget) != sha256.Size {
			return fmt.Errorf("--find-hash must be %d bytes, got %d", sha256.Size, len(findTarget))
		}
	}

	switch backend {
	case "flatkv":
		return digestFlatKV(dbDir, height, findTarget)
	case "memiavl":
		return digestMemIAVL(dbDir, height, findTarget, memiavlNormalization, memiavlOpenMode)
	case "composite":
		if flatKVDir == "" || memIAVLDir == "" {
			return errors.New("--backend composite requires --flatkv-dir and --memiavl-dir")
		}
		return digestCompositeMigrateEVM(flatKVDir, memIAVLDir, height, findTarget, memiavlOpenMode)
	default:
		return fmt.Errorf("unknown --backend %q (want flatkv|memiavl|composite)", backend)
	}
}

func digestCompositeMigrateEVM(flatKVDir, memIAVLDir string, height int64, findTarget []byte, memiavlOpenMode string) error {
	opened, err := openFlatKVReadOnly(flatKVDir, height)
	if err != nil {
		return fmt.Errorf("open flatkv read-only: %w", err)
	}
	defer func() { _ = opened.Close() }()

	boundary, versionKnown, migrationVersion, err := readFlatKVMigrationState(opened)
	if err != nil {
		return err
	}

	ctx := digestPrintContext{
		backend:         "composite",
		mode:            "migrate_evm",
		dbDir:           fmt.Sprintf("flatkv=%s memiavl=%s", flatKVDir, memIAVLDir),
		requestedHeight: height,
		version:         opened.Version(),
	}
	var memReplayDB *memiavl.DB
	var memEvmSnapshotDir string
	var memVersion int64
	switch memiavlOpenMode {
	case "", memiavlOpenModeSnapshot:
		memEvmSnapshotDir, err = resolveMemIAVLEvmSnapshotDir(memIAVLDir, height)
		if err != nil {
			return err
		}
		memVersion, err = readMemIAVLSnapshotVersion(memEvmSnapshotDir)
		if err != nil {
			return err
		}
		ctx.source = fmt.Sprintf("flatkv clone version=%d + memiavl snapshot=%s", opened.Version(), memEvmSnapshotDir)
		ctx.normalization = fmt.Sprintf("flatkv rows plus memiavl rows not migrated by boundary=%s version_known=%t migration_version=%d memiavl_version=%d", boundary.String(), versionKnown, migrationVersion, memVersion)
	case memiavlOpenModeReplay:
		memReplayDB, err = openMemiAVLReplayReadOnly(memIAVLDir, height)
		if err != nil {
			return err
		}
		defer func() { _ = memReplayDB.Close() }()
		memVersion = memReplayDB.Version()
		ctx.source = fmt.Sprintf("flatkv clone version=%d + memiavl read-only replay dir=%s", opened.Version(), memIAVLDir)
		ctx.normalization = fmt.Sprintf("flatkv rows plus replayed memiavl rows not migrated by boundary=%s version_known=%t migration_version=%d memiavl_version=%d", boundary.String(), versionKnown, migrationVersion, memVersion)
	default:
		return fmt.Errorf("unknown --memiavl-open-mode %q (want snapshot|replay)", memiavlOpenMode)
	}
	printDigestStart(ctx)
	sink.say("Scan progress: composite migrate_evm logical view -> flatkv rows plus memiavl rows to the right of boundary")

	d := evmDigest{findTarget: findTarget}
	accounts := make(map[string]*semanticAccountDigestState)

	if err := consumeCompositeFlatKV(opened, &d, accounts); err != nil {
		return err
	}
	if boundary.Status() != migration.MigrationComplete {
		if memReplayDB != nil {
			if err := consumeCompositeMemiavl(func(fn func(rawKey, rawVal []byte) error) error {
				return scanMemiavlReplayEVMLeaves(memReplayDB, fn)
			}, "memiavl-replay", boundary, &d, accounts); err != nil {
				return err
			}
		} else {
			if err := consumeCompositeMemiavl(func(fn func(rawKey, rawVal []byte) error) error {
				return scanMemiavlSnapshotEVMLeaves(memEvmSnapshotDir, fn)
			}, "memiavl", boundary, &d, accounts); err != nil {
				return err
			}
		}
	}
	finalizeSemanticAccounts(accounts, d.addLogical, nil)
	return d.emit(ctx)
}

func readFlatKVMigrationState(store *openedFlatKV) (migration.MigrationBoundary, bool, uint64, error) {
	if data, ok := store.Get(migration.MigrationStore, []byte(migration.MigrationVersionKey)); ok {
		if len(data) != 8 {
			return migration.MigrationBoundary{}, false, 0, fmt.Errorf("flatkv migration version length=%d, want 8", len(data))
		}
		return migration.MigrationBoundaryComplete, true, binary.BigEndian.Uint64(data), nil
	}
	if data, ok := store.Get(migration.MigrationStore, []byte(migration.MigrationBoundaryKey)); ok {
		b, err := migration.DeserializeMigrationBoundary(data)
		return b, false, 0, err
	}
	return migration.MigrationBoundaryNotStarted, false, 0, nil
}

// shouldIncludeFlatKVEVMLogicalDigestKey reports whether a raw FlatKV physical
// row belongs in the EVM logical digest. FlatKV's RawGlobalIterator yields every
// module's rows, but only the EVM module participates in the comparison against a
// memiavl node (which walks the EVM tree alone). The FlatKV-only migration
// markers are let through here and then XORed back out in legacyForCompare; all
// other non-EVM module rows (e.g. Cosmos state migrated into FlatKV) are excluded
// so the legacy bucket and FINAL_DIGEST stay comparable across backends.
func shouldIncludeFlatKVEVMLogicalDigestKey(physKey []byte) bool {
	if bytes.Equal(physKey, migrationVersionPhysKey) || bytes.Equal(physKey, migrationBoundaryPhysKey) {
		return true
	}
	moduleName, _, err := ktype.StripModulePrefix(physKey)
	return err == nil && moduleName == keys.EVMStoreKey
}

func consumeCompositeFlatKV(opened *openedFlatKV, d *evmDigest, accounts map[string]*semanticAccountDigestState) error {
	iter, err := opened.RawGlobalIterator()
	if err != nil {
		return fmt.Errorf("raw global iterator: %w", err)
	}
	defer func() { _ = iter.Close() }()
	var seen uint64
	for ; iter.Valid(); iter.Next() {
		seen++
		k := iter.Key()
		if !shouldIncludeFlatKVEVMLogicalDigestKey(k) {
			continue
		}
		if classifyFlatKVPhysicalKey(k) == flatkvBucketAccount {
			if err := mergeCompositeFlatKVAccount(accounts, k, iter.Value()); err != nil {
				return err
			}
		} else if err := d.consume(k, iter.Value()); err != nil {
			return err
		}
		if seen%20000000 == 0 {
			sink.sayf("  progress backend=composite source=flatkv input_physical_rows=%d account_buffered=%d code=%d storage=%d misc=%d\n", seen, len(accounts), d.code.count, d.storage.count, d.misc.count)
		}
	}
	if err := iter.Error(); err != nil {
		return fmt.Errorf("iterate flatkv: %w", err)
	}
	sink.sayf("  composite flatkv rows=%d\n", seen)
	return nil
}

func mergeCompositeFlatKVAccount(accounts map[string]*semanticAccountDigestState, physKey, val []byte) error {
	kind, addr, err := ktype.StripEVMPhysicalKey(physKey)
	if err != nil {
		return err
	}
	if kind != ktype.EVMKeyAccount {
		return fmt.Errorf("flatkv account key %X kind=%d, want account", physKey, kind)
	}
	ad, err := vtype.DeserializeAccountData(val)
	if err != nil {
		return err
	}
	acct := getSemanticAccount(accounts, addr)
	bal := ad.GetBalance()
	copy(acct.balance[:], bal[:])
	acct.nonce = ad.GetNonce()
	copy(acct.codeHash[:], ad.GetCodeHash()[:])
	return nil
}

// consumeCompositeMemiavl folds the memiavl EVM leaves NOT yet migrated past the
// boundary into d (the unmigrated tail of the composite mid-migration view).
// srcLabel selects the snapshot vs replay wording so both modes emit the same
// progress output as before.
func consumeCompositeMemiavl(scan evmLeafSource, srcLabel string, boundary migration.MigrationBoundary, d *evmDigest, accounts map[string]*semanticAccountDigestState) error {
	var leaves, consumed uint64
	if err := scan(func(k, v []byte) error {
		leaves++
		if !boundary.IsMigrated(keys.EVMStoreKey, k) {
			if err := d.consumeSemanticMemiavlLeaf(accounts, k, v); err != nil {
				return err
			}
			consumed++
		}
		if leaves%20000000 == 0 {
			sink.sayf("  progress backend=composite source=%s input_leaves=%d consumed_unmigrated=%d account_buffered=%d code=%d storage=%d misc=%d\n",
				srcLabel, leaves, consumed, len(accounts), d.code.count, d.storage.count, d.misc.count)
		}
		return nil
	}); err != nil {
		return err
	}
	sink.sayf("  composite %s leaves=%d consumed_unmigrated=%d\n", srcLabel, leaves, consumed)
	return nil
}

func digestFlatKV(dbDir string, height int64, findTarget []byte) error {
	opened, err := openFlatKVReadOnly(dbDir, height)
	if err != nil {
		return fmt.Errorf("open flatkv read-only: %w", err)
	}
	defer func() { _ = opened.Close() }()

	version := opened.Version()
	iter, err := opened.RawGlobalIterator()
	if err != nil {
		return fmt.Errorf("raw global iterator: %w", err)
	}
	defer func() { _ = iter.Close() }()

	d := evmDigest{findTarget: findTarget}
	ctx := digestPrintContext{
		backend:         "flatkv",
		mode:            "native",
		dbDir:           dbDir,
		source:          "isolated FlatKV clone opened from snapshot + changelog WAL replay",
		normalization:   "native FlatKV physical keyspace; values reduced to height-independent logical payload",
		requestedHeight: height,
		version:         version,
	}
	printDigestStart(ctx)
	sink.say("Scan progress: flatkv input_physical_rows -> normalized logical bucket counts")
	var seen uint64
	for ; iter.Valid(); iter.Next() {
		k := iter.Key()
		seen++
		if !shouldIncludeFlatKVEVMLogicalDigestKey(k) {
			continue
		}
		if err := d.consume(k, iter.Value()); err != nil {
			return err
		}
		if seen%20000000 == 0 {
			sink.sayf("  progress backend=flatkv input_physical_rows=%d digested account=%d code=%d storage=%d misc=%d\n",
				seen, d.account.count, d.code.count, d.storage.count, d.misc.count)
		}
	}
	if err := iter.Error(); err != nil {
		return fmt.Errorf("iterate: %w", err)
	}
	return d.emit(ctx)
}

type inspectAccumulator struct {
	inspectBucket  string
	keyOffset      int
	keyPrefix      []byte
	shardNextBytes int
	list           bool
	listLimit      int
	details        bool
	shards         map[string]*digestBucket
	matched        uint64
	listed         int
}

func runEvmLogicalInspect(cmd *cobra.Command, backend, dbDir string, height int64, inspectBucket string, memiavlNormalization string) error {
	if !isFlatKVBucket(inspectBucket) {
		return fmt.Errorf("unknown --inspect-bucket %q", inspectBucket)
	}
	keyOffset, _ := cmd.Flags().GetInt("key-offset")
	keyPrefixHex, _ := cmd.Flags().GetString("key-prefix")
	keyPrefix, err := hex.DecodeString(keyPrefixHex)
	if err != nil {
		return fmt.Errorf("decode --key-prefix: %w", err)
	}
	shardNextBytes, _ := cmd.Flags().GetInt("shard-next-bytes")
	list, _ := cmd.Flags().GetBool("list")
	listLimit, _ := cmd.Flags().GetInt("list-limit")
	details, _ := cmd.Flags().GetBool("details")
	if keyOffset < 0 {
		return errors.New("--key-offset must be non-negative")
	}
	if shardNextBytes < 0 {
		return errors.New("--shard-next-bytes must be non-negative")
	}
	acc := &inspectAccumulator{
		inspectBucket:  inspectBucket,
		keyOffset:      keyOffset,
		keyPrefix:      keyPrefix,
		shardNextBytes: shardNextBytes,
		list:           list,
		listLimit:      listLimit,
		details:        details,
		shards:         make(map[string]*digestBucket),
	}

	switch backend {
	case "flatkv":
		return inspectFlatKV(dbDir, height, acc)
	case "memiavl":
		return inspectMemIAVL(dbDir, height, acc, memiavlNormalization)
	default:
		return fmt.Errorf("unknown --backend %q (want flatkv|memiavl)", backend)
	}
}

func (a *inspectAccumulator) consume(physKey, val []byte) error {
	return a.consumeWithMeta(physKey, val, "")
}

func (a *inspectAccumulator) consumeWithMeta(physKey, val []byte, meta string) error {
	bucket, logical, err := normalizeEVMFlatKVPair(physKey, val)
	if err != nil {
		return err
	}
	a.consumeLogical(bucket, physKey, logical, meta)
	return nil
}

func (a *inspectAccumulator) consumeLogical(bucket string, physKey, logical []byte, meta string) {
	if bucket != a.inspectBucket {
		return
	}
	if len(physKey) < a.keyOffset {
		return
	}
	rel := physKey[a.keyOffset:]
	if !bytes.HasPrefix(rel, a.keyPrefix) {
		return
	}
	a.matched++
	if a.list {
		if a.listLimit <= 0 || a.listed < a.listLimit {
			if meta != "" {
				fmt.Printf("key=%X logical=%X %s\n", physKey, logical, meta)
			} else {
				fmt.Printf("key=%X logical=%X\n", physKey, logical)
			}
			a.listed++
		}
		return
	}
	shardEnd := len(a.keyPrefix) + a.shardNextBytes
	if shardEnd > len(rel) {
		shardEnd = len(rel)
	}
	shard := hex.EncodeToString(rel[:shardEnd])
	d := a.shards[shard]
	if d == nil {
		d = &digestBucket{}
		a.shards[shard] = d
	}
	d.add(physKey, logical)
}

func (a *inspectAccumulator) print(version int64) {
	fmt.Printf("version: %d\n", version)
	fmt.Printf("inspect bucket=%s key_offset=%d key_prefix=%X matched=%d\n",
		a.inspectBucket, a.keyOffset, a.keyPrefix, a.matched)
	if a.list {
		fmt.Printf("listed=%d list_limit=%d\n", a.listed, a.listLimit)
		return
	}
	keys := make([]string, 0, len(a.shards))
	for k := range a.shards {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		d := a.shards[k]
		fmt.Printf("shard=%s count=%d bucket_digest=%X\n", k, d.count, d.acc)
	}
}

func inspectFlatKV(dbDir string, height int64, acc *inspectAccumulator) error {
	opened, err := openFlatKVReadOnly(dbDir, height)
	if err != nil {
		return fmt.Errorf("open flatkv read-only: %w", err)
	}
	defer func() { _ = opened.Close() }()

	iter, err := opened.RawGlobalIterator()
	if err != nil {
		return fmt.Errorf("raw global iterator: %w", err)
	}
	defer func() { _ = iter.Close() }()

	var seen uint64
	for ; iter.Valid(); iter.Next() {
		seen++
		if !shouldIncludeFlatKVEVMLogicalDigestKey(iter.Key()) {
			continue
		}
		meta := ""
		if acc.details && acc.list {
			var derr error
			meta, derr = flatKVValueMeta(iter.Key(), iter.Value())
			if derr != nil {
				return derr
			}
		}
		if err := acc.consumeWithMeta(iter.Key(), iter.Value(), meta); err != nil {
			return err
		}
		if seen%20000000 == 0 {
			fmt.Printf("  ...flatkv inspect seen=%d matched=%d\n", seen, acc.matched)
		}
	}
	if err := iter.Error(); err != nil {
		return fmt.Errorf("iterate: %w", err)
	}
	acc.print(opened.Version())
	return nil
}

func inspectMemIAVL(dbDir string, height int64, acc *inspectAccumulator, normalization string) error {
	switch normalization {
	case "", memiavlNormSemantic, memiavlNormIndependent:
		return inspectMemIAVLSemantic(dbDir, height, acc)
	case memiavlNormTranslator:
		return inspectMemIAVLTranslator(dbDir, height, acc)
	default:
		return fmt.Errorf("unknown --memiavl-normalization %q (want semantic|independent|translator)", normalization)
	}
}

func inspectMemIAVLTranslator(dbDir string, height int64, acc *inspectAccumulator) error {
	if acc.details && acc.list && acc.inspectBucket == flatkvBucketStorage {
		return inspectMemIAVLStorageDetails(dbDir, height, acc)
	}

	evmSnapshotDir, err := resolveMemIAVLEvmSnapshotDir(dbDir, height)
	if err != nil {
		return err
	}
	version, err := readMemIAVLSnapshotVersion(evmSnapshotDir)
	if err != nil {
		return err
	}

	translator := flatkv.NewImportTranslator(0)
	var leaves uint64
	const batchCap = 8192
	batch := make([]*proto.KVPair, 0, batchCap)
	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		cs := &proto.NamedChangeSet{Name: keys.EVMStoreKey, Changeset: proto.ChangeSet{Pairs: batch}}
		pairs, terr := translator.Translate(cs)
		if terr != nil {
			return fmt.Errorf("translate batch: %w", terr)
		}
		for _, p := range pairs {
			if cerr := acc.consume(p.Key, p.Value); cerr != nil {
				return cerr
			}
		}
		batch = batch[:0]
		return nil
	}

	if err := scanMemiavlSnapshotEVMLeaves(evmSnapshotDir, func(k, v []byte) error {
		leaves++
		if leaves%20000000 == 0 {
			fmt.Printf("  ...memiavl inspect leaves=%d matched=%d\n", leaves, acc.matched)
		}
		batch = append(batch, &proto.KVPair{Key: k, Value: v})
		if len(batch) >= batchCap {
			return flush()
		}
		return nil
	}); err != nil {
		return err
	}
	if err := flush(); err != nil {
		return err
	}
	for _, p := range translator.Finalize() {
		if err := acc.consume(p.Key, p.Value); err != nil {
			return err
		}
	}
	fmt.Printf("  memiavl inspect total leaves=%d\n", leaves)
	acc.print(version)
	return nil
}

func inspectMemIAVLSemantic(dbDir string, height int64, acc *inspectAccumulator) error {
	if acc.details && acc.list && acc.inspectBucket == flatkvBucketStorage {
		return inspectMemIAVLStorageDetails(dbDir, height, acc)
	}

	evmSnapshotDir, err := resolveMemIAVLEvmSnapshotDir(dbDir, height)
	if err != nil {
		return err
	}
	version, err := readMemIAVLSnapshotVersion(evmSnapshotDir)
	if err != nil {
		return err
	}

	var accounts map[string]*semanticAccountDigestState
	if acc.inspectBucket == flatkvBucketAccount {
		accounts = make(map[string]*semanticAccountDigestState)
	}
	consume := func(bucket string, physKey, logical, _ []byte) {
		acc.consumeLogical(bucket, physKey, logical, "")
	}
	var leaves uint64
	if err := scanMemiavlSnapshotEVMLeaves(evmSnapshotDir, func(k, v []byte) error {
		leaves++
		if leaves%20000000 == 0 {
			fmt.Printf("  ...memiavl inspect mode=semantic leaves=%d matched=%d\n", leaves, acc.matched)
		}
		return consumeSemanticMemiavlLeaf(accounts, k, v, consume, nil, "inspect")
	}); err != nil {
		return err
	}
	finalizeSemanticAccounts(accounts, consume, nil)
	fmt.Printf("  memiavl inspect total leaves=%d\n", leaves)
	acc.print(version)
	return nil
}

func inspectMemIAVLStorageDetails(dbDir string, height int64, acc *inspectAccumulator) error {
	evmSnapshotDir, err := resolveMemIAVLEvmSnapshotDir(dbDir, height)
	if err != nil {
		return err
	}
	version, err := readMemIAVLSnapshotVersion(evmSnapshotDir)
	if err != nil {
		return err
	}

	kvsPath := filepath.Join(evmSnapshotDir, "kvs")
	kvsFile, err := os.Open(filepath.Clean(kvsPath))
	if err != nil {
		return fmt.Errorf("open kvs %s: %w", kvsPath, err)
	}
	defer func() { _ = kvsFile.Close() }()
	kvsReader := bufio.NewReaderSize(kvsFile, 16*1024*1024)

	leavesPath := filepath.Join(evmSnapshotDir, "leaves")
	leavesFile, err := os.Open(filepath.Clean(leavesPath))
	if err != nil {
		return fmt.Errorf("open leaves %s: %w", leavesPath, err)
	}
	defer func() { _ = leavesFile.Close() }()
	leavesReader := bufio.NewReaderSize(leavesFile, 1024*1024)

	rawOffset := acc.keyOffset - len(keys.EVMStoreKey) - 1
	if rawOffset < 0 {
		return fmt.Errorf("--details storage memiavl requires --key-offset >= %d", len(keys.EVMStoreKey)+1)
	}

	var lenbuf [4]byte
	var leafbuf [48]byte
	var leaves uint64
	for {
		if _, err := io.ReadFull(kvsReader, lenbuf[:]); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return fmt.Errorf("read key len: %w", err)
		}
		keyLen := binary.LittleEndian.Uint32(lenbuf[:])
		k := make([]byte, keyLen)
		if _, err := io.ReadFull(kvsReader, k); err != nil {
			return fmt.Errorf("read key: %w", err)
		}
		if _, err := io.ReadFull(kvsReader, lenbuf[:]); err != nil {
			return fmt.Errorf("read val len: %w", err)
		}
		valLen := binary.LittleEndian.Uint32(lenbuf[:])
		v := make([]byte, valLen)
		if _, err := io.ReadFull(kvsReader, v); err != nil {
			return fmt.Errorf("read val: %w", err)
		}
		if _, err := io.ReadFull(leavesReader, leafbuf[:]); err != nil {
			return fmt.Errorf("read leaf metadata: %w", err)
		}

		leaves++
		if leaves%20000000 == 0 {
			fmt.Printf("  ...memiavl inspect leaves=%d matched=%d\n", leaves, acc.matched)
		}
		if len(k) < rawOffset || !bytes.HasPrefix(k[rawOffset:], acc.keyPrefix) {
			continue
		}
		kind, keyBytes := keys.ParseEVMKey(k)
		if kind != keys.EVMKeyStorage {
			continue
		}
		if isAllZero(v) {
			continue
		}
		value, err := vtype.ParseStorageValue(v)
		if err != nil {
			return fmt.Errorf("parse storage value %X: %w", k, err)
		}
		leafVersion := int64(binary.LittleEndian.Uint32(leafbuf[:4]))
		physKey := ktype.EVMPhysicalKey(keys.EVMKeyStorage, keyBytes)
		storageData := vtype.NewStorageData().SetBlockHeight(leafVersion).SetValue(value)
		if err := acc.consumeWithMeta(physKey, storageData.Serialize(), fmt.Sprintf("leaf_version=%d", leafVersion)); err != nil {
			return err
		}
	}

	fmt.Printf("  memiavl inspect total leaves=%d\n", leaves)
	acc.print(version)
	return nil
}

func flatKVValueMeta(physKey, val []byte) (string, error) {
	switch bucket := classifyFlatKVPhysicalKey(physKey); bucket {
	case flatkvBucketAccount:
		ad, err := vtype.DeserializeAccountData(val)
		if err != nil {
			return "", fmt.Errorf("deserialize account %X: %w", physKey, err)
		}
		return fmt.Sprintf("block_height=%d", ad.GetBlockHeight()), nil
	case flatkvBucketCode:
		cd, err := vtype.DeserializeCodeData(val)
		if err != nil {
			return "", fmt.Errorf("deserialize code %X: %w", physKey, err)
		}
		return fmt.Sprintf("block_height=%d", cd.GetBlockHeight()), nil
	case flatkvBucketStorage:
		sd, err := vtype.DeserializeStorageData(val)
		if err != nil {
			return "", fmt.Errorf("deserialize storage %X: %w", physKey, err)
		}
		return fmt.Sprintf("block_height=%d", sd.GetBlockHeight()), nil
	default:
		ld, err := vtype.DeserializeMiscData(val)
		if err != nil {
			return "", fmt.Errorf("deserialize misc %X: %w", physKey, err)
		}
		return fmt.Sprintf("block_height=%d", ld.GetBlockHeight()), nil
	}
}

// digestMemIAVL streams the snapshot's `kvs` file sequentially instead of
// walking the tree. Rationale (the "iterate by index, not by tree" advice taken
// to its limit):
//
//   - memIAVL writes every leaf's (key,value) into the `kvs` file in leaf order
//     (post-order == ascending key order) and writes nothing else there: branch
//     nodes carry only a keyLeaf pointer, never a kv payload. So the `kvs` file
//     is exactly the leaf set, already laid out contiguously.
//   - Walking the tree (or even ScanNodes) goes through the mmap, which
//     OpenSnapshot deliberately tags MADV_RANDOM — that disables kernel
//     readahead, so a full scan degenerates into one 4 KiB page fault per access.
//     Under disk contention (e.g. a concurrent snapshot write) that collapses to
//     tens of thousands of rows/sec.
//   - Reading the `kvs` file with a plain buffered os.File restores normal
//     sequential readahead, so the scan runs at raw sequential-disk bandwidth
//     with zero seeks. The digest is XOR-order-independent, so leaf order is
//     irrelevant to correctness.
//
// kvs record layout (little-endian), repeated leafCount times until EOF:
//
//	keyLen uint32 | key [keyLen] | valLen uint32 | value [valLen]
func digestMemIAVL(dbDir string, height int64, findTarget []byte, normalization string, openMode string) error {
	if openMode == memiavlOpenModeReplay {
		return digestMemIAVLReplay(dbDir, height, findTarget, normalization)
	}
	if openMode != "" && openMode != memiavlOpenModeSnapshot {
		return fmt.Errorf("unknown --memiavl-open-mode %q (want snapshot|replay)", openMode)
	}
	switch normalization {
	case "", memiavlNormSemantic, memiavlNormIndependent:
		return digestMemIAVLSemantic(dbDir, height, findTarget)
	case memiavlNormTranslator:
		return digestMemIAVLTranslator(dbDir, height, findTarget)
	default:
		return fmt.Errorf("unknown --memiavl-normalization %q (want semantic|independent|translator)", normalization)
	}
}

// openMemiAVLReplayReadOnly opens dbDir for changelog replay without repairing
// its changelog. The repair is a truncation, and a record a running node is
// midway through writing is indistinguishable from a corrupt one, so repairing
// here can discard a block that node has committed.
func openMemiAVLReplayReadOnly(dbDir string, height int64) (*memiavl.DB, error) {
	db, err := memiavl.OpenDB(height, memiavl.Options{
		Dir:               dbDir,
		ReadOnly:          true,
		ZeroCopy:          true,
		NoChangelogRepair: true,
	})
	if err != nil {
		if errors.Is(err, wal.ErrCorrupt) {
			return nil, fmt.Errorf("the changelog under %s ends mid-record, which is what a node "+
				"writing a block looks like; %s was left as it was found, so rerun this command, and if "+
				"it keeps failing the changelog is corrupt and the node needs attention: %w",
				dbDir, dbDir, err)
		}
		return nil, fmt.Errorf("open memiavl read-only replay: %w", err)
	}
	return db, nil
}

func digestMemIAVLReplay(dbDir string, height int64, findTarget []byte, normalization string) error {
	db, err := openMemiAVLReplayReadOnly(dbDir, height)
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()

	switch normalization {
	case "", memiavlNormSemantic, memiavlNormIndependent:
		return digestMemIAVLReplaySemantic(dbDir, height, db, findTarget)
	case memiavlNormTranslator:
		return digestMemIAVLReplayTranslator(dbDir, height, db, findTarget)
	default:
		return fmt.Errorf("unknown --memiavl-normalization %q (want semantic|independent|translator)", normalization)
	}
}

// evmLeafSource streams raw memiavl EVM (key,val) leaves to fn, stopping on the
// first error. It abstracts the two ways the tool reads memiavl EVM leaves — a
// snapshot kvs file scan and a replayed read-only tree walk — so the semantic
// and translator digest cores below are shared between snapshot and replay modes.
type evmLeafSource func(fn func(rawKey, rawVal []byte) error) error

// scanMemiavlSnapshotEVMLeaves streams every leaf from a memiavl EVM snapshot
// kvs file (length-prefixed keyLen|key|valLen|val, little-endian).
func scanMemiavlSnapshotEVMLeaves(evmSnapshotDir string, fn func(rawKey, rawVal []byte) error) error {
	kvsPath := filepath.Join(evmSnapshotDir, "kvs")
	f, err := os.Open(filepath.Clean(kvsPath))
	if err != nil {
		return fmt.Errorf("open kvs %s: %w", kvsPath, err)
	}
	defer func() { _ = f.Close() }()
	r := bufio.NewReaderSize(f, 16*1024*1024)
	var lenbuf [4]byte
	for {
		if _, err := io.ReadFull(r, lenbuf[:]); err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return fmt.Errorf("read key len: %w", err)
		}
		k := make([]byte, binary.LittleEndian.Uint32(lenbuf[:]))
		if _, err := io.ReadFull(r, k); err != nil {
			return fmt.Errorf("read key: %w", err)
		}
		if _, err := io.ReadFull(r, lenbuf[:]); err != nil {
			return fmt.Errorf("read val len: %w", err)
		}
		v := make([]byte, binary.LittleEndian.Uint32(lenbuf[:]))
		if _, err := io.ReadFull(r, v); err != nil {
			return fmt.Errorf("read val: %w", err)
		}
		if err := fn(k, v); err != nil {
			return err
		}
	}
}

// scanMemiavlReplayEVMLeaves streams every leaf from the EVM tree of a read-only
// replayed memiavl DB.
func scanMemiavlReplayEVMLeaves(db *memiavl.DB, fn func(rawKey, rawVal []byte) error) error {
	tree := db.TreeByName(keys.EVMStoreKey)
	if tree == nil {
		return fmt.Errorf("memiavl tree %q not found", keys.EVMStoreKey)
	}
	iter := tree.Iterator(nil, nil, true)
	defer func() { _ = iter.Close() }()
	for ; iter.Valid(); iter.Next() {
		if err := fn(iter.Key(), iter.Value()); err != nil {
			return err
		}
	}
	if err := iter.Error(); err != nil {
		return fmt.Errorf("iterate replayed memiavl: %w", err)
	}
	return nil
}

// runMemiavlSemanticDigest digests memiavl EVM leaves with the independent
// semantic decoder (no flatkv.ImportTranslator), buffering account fragments and
// merging them at finalize. modeLabel/totalLabel select the snapshot vs replay
// wording so each mode emits the same progress output as before.
func runMemiavlSemanticDigest(ctx digestPrintContext, modeLabel, totalLabel string, findTarget []byte, scan evmLeafSource) error {
	d := evmDigest{findTarget: findTarget}
	accounts := make(map[string]*semanticAccountDigestState)
	var leaves uint64
	if err := scan(func(k, v []byte) error {
		leaves++
		if err := d.consumeSemanticMemiavlLeaf(accounts, k, v); err != nil {
			return err
		}
		if leaves%20000000 == 0 {
			sink.sayf("  progress backend=memiavl mode=%s input_leaves=%d digested account=deferred_until_finalize account_buffered=%d code=%d storage=%d misc=%d\n",
				modeLabel, leaves, len(accounts), d.code.count, d.storage.count, d.misc.count)
		}
		return nil
	}); err != nil {
		return err
	}
	d.finalizeSemanticAccounts(accounts)
	sink.sayf("  finalize backend=memiavl mode=%s account=%d code=%d storage=%d misc=%d\n",
		modeLabel, d.account.count, d.code.count, d.storage.count, d.misc.count)
	sink.sayf("  %s=%d\n", totalLabel, leaves)
	return d.emit(ctx)
}

// runMemiavlTranslatorDigest digests memiavl EVM leaves by routing every leaf
// through flatkv.ImportTranslator (the exact classifyAndPrefix + merge path
// CommitStore.ApplyChangeSets uses) and then through the shared consume, making
// the memiavl and flatkv digests byte-identical by construction. Translate
// streams storage/code/misc out immediately and only buffers account fragments
// until Finalize, so RSS stays bounded.
func runMemiavlTranslatorDigest(ctx digestPrintContext, modeLabel, totalLabel string, findTarget []byte, scan evmLeafSource) error {
	translator := flatkv.NewImportTranslator(0)
	d := evmDigest{findTarget: findTarget}
	const batchCap = 8192
	batch := make([]*proto.KVPair, 0, batchCap)
	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		cs := &proto.NamedChangeSet{Name: keys.EVMStoreKey, Changeset: proto.ChangeSet{Pairs: batch}}
		pairs, terr := translator.Translate(cs)
		if terr != nil {
			return fmt.Errorf("translate batch: %w", terr)
		}
		for _, p := range pairs {
			if cerr := d.consume(p.Key, p.Value); cerr != nil {
				return cerr
			}
		}
		batch = batch[:0]
		return nil
	}
	var leaves uint64
	if err := scan(func(k, v []byte) error {
		leaves++
		batch = append(batch, &proto.KVPair{Key: k, Value: v})
		if len(batch) >= batchCap {
			if err := flush(); err != nil {
				return err
			}
		}
		if leaves%20000000 == 0 {
			if err := flush(); err != nil {
				return err
			}
			sink.sayf("  progress backend=memiavl mode=%s input_leaves=%d digested account=deferred_until_finalize code=%d storage=%d misc=%d\n",
				modeLabel, leaves, d.code.count, d.storage.count, d.misc.count)
		}
		return nil
	}); err != nil {
		return err
	}
	if err := flush(); err != nil {
		return err
	}
	for _, p := range translator.Finalize() {
		if err := d.consume(p.Key, p.Value); err != nil {
			return err
		}
	}
	sink.sayf("  finalize backend=memiavl mode=%s account=%d code=%d storage=%d misc=%d\n",
		modeLabel, d.account.count, d.code.count, d.storage.count, d.misc.count)
	sink.sayf("  %s=%d\n", totalLabel, leaves)
	return d.emit(ctx)
}

func digestMemIAVLReplaySemantic(dbDir string, height int64, db *memiavl.DB, findTarget []byte) error {
	ctx := digestPrintContext{
		backend:         "memiavl",
		mode:            "semantic-replay",
		dbDir:           dbDir,
		source:          "read-only memiavl DB opened from snapshot + changelog replay",
		normalization:   "independent semantic decoder for replayed memiavl EVM keys; does not call flatkv.ImportTranslator",
		requestedHeight: height,
		version:         db.Version(),
	}
	printDigestStart(ctx)
	sink.say("Scan progress: replayed memiavl iterator -> independently decoded EVM logical bucket counts")
	sink.say("Note: semantic replay mode walks the in-memory/mmap tree, not the snapshot kvs file.")
	return runMemiavlSemanticDigest(ctx, "semantic-replay", "memiavl-replay total leaves", findTarget,
		func(fn func(rawKey, rawVal []byte) error) error { return scanMemiavlReplayEVMLeaves(db, fn) })
}

func digestMemIAVLReplayTranslator(dbDir string, height int64, db *memiavl.DB, findTarget []byte) error {
	ctx := digestPrintContext{
		backend:         "memiavl",
		mode:            "translator-replay",
		dbDir:           dbDir,
		source:          "read-only memiavl DB opened from snapshot + changelog replay",
		normalization:   "replayed memiavl leaves translated with flatkv.ImportTranslator, then reduced to logical payload",
		requestedHeight: height,
		version:         db.Version(),
	}
	printDigestStart(ctx)
	sink.say("Scan progress: replayed memiavl iterator -> translated flatkv logical bucket counts")
	sink.say("Note: account rows are merged by the translator at finalize, so progress shows account=deferred_until_finalize.")
	return runMemiavlTranslatorDigest(ctx, "translator-replay", "memiavl-replay total leaves", findTarget,
		func(fn func(rawKey, rawVal []byte) error) error { return scanMemiavlReplayEVMLeaves(db, fn) })
}

func digestMemIAVLTranslator(dbDir string, height int64, findTarget []byte) error {
	evmSnapshotDir, err := resolveMemIAVLEvmSnapshotDir(dbDir, height)
	if err != nil {
		return err
	}
	version, err := readMemIAVLSnapshotVersion(evmSnapshotDir)
	if err != nil {
		return err
	}
	ctx := digestPrintContext{
		backend:         "memiavl",
		mode:            memiavlNormTranslator,
		dbDir:           dbDir,
		source:          evmSnapshotDir + " (snapshot/current only; no memiavl WAL replay)",
		normalization:   "memiavl leaves translated with flatkv.ImportTranslator, then reduced to logical payload",
		requestedHeight: height,
		version:         version,
	}
	printDigestStart(ctx)
	sink.say("Scan progress: memiavl input_leaves -> translated flatkv logical bucket counts")
	sink.say("Note: account rows are merged by the translator at finalize, so progress shows account=deferred_until_finalize.")
	return runMemiavlTranslatorDigest(ctx, memiavlNormTranslator, "memiavl total leaves", findTarget,
		func(fn func(rawKey, rawVal []byte) error) error {
			return scanMemiavlSnapshotEVMLeaves(evmSnapshotDir, fn)
		})
}

type semanticAccountDigestState struct {
	balance  [32]byte
	nonce    uint64
	codeHash [32]byte
	// codeHashRow records that a code-hash row was read for this address. An all-zero
	// codeHash is otherwise indistinguishable from one that was never written.
	codeHashRow bool
}

func (s *semanticAccountDigestState) isZeroAccount() bool {
	if s == nil {
		return true
	}
	return !s.isLiveAccount() && s.hasZeroCodeHash()
}

func (s *semanticAccountDigestState) isLiveAccount() bool {
	if s == nil {
		return false
	}
	if s.nonce != 0 {
		return true
	}
	for _, b := range s.balance {
		if b != 0 {
			return true
		}
	}
	return false
}

func (s *semanticAccountDigestState) hasZeroCodeHash() bool {
	if s == nil {
		return true
	}
	for _, b := range s.codeHash {
		if b != 0 {
			return false
		}
	}
	return true
}

func (s *semanticAccountDigestState) logicalPayload() []byte {
	logical := make([]byte, 72)
	copy(logical[:32], s.balance[:])
	binary.BigEndian.PutUint64(logical[32:40], s.nonce)
	copy(logical[40:], s.codeHash[:])
	return logical
}

func digestMemIAVLSemantic(dbDir string, height int64, findTarget []byte) error {
	evmSnapshotDir, err := resolveMemIAVLEvmSnapshotDir(dbDir, height)
	if err != nil {
		return err
	}
	version, err := readMemIAVLSnapshotVersion(evmSnapshotDir)
	if err != nil {
		return err
	}
	ctx := digestPrintContext{
		backend:         "memiavl",
		mode:            memiavlNormSemantic,
		dbDir:           dbDir,
		source:          evmSnapshotDir + " (snapshot/current only; no memiavl WAL replay)",
		normalization:   "independent semantic decoder for raw memiavl EVM keys; does not call flatkv.ImportTranslator",
		requestedHeight: height,
		version:         version,
	}
	printDigestStart(ctx)
	sink.say("Scan progress: memiavl input_leaves -> independently decoded EVM logical bucket counts")
	sink.say("Note: semantic mode does not call flatkv.ImportTranslator; account rows are merged locally at finalize.")
	return runMemiavlSemanticDigest(ctx, memiavlNormSemantic, "memiavl total leaves", findTarget,
		func(fn func(rawKey, rawVal []byte) error) error {
			return scanMemiavlSnapshotEVMLeaves(evmSnapshotDir, fn)
		})
}

func (d *evmDigest) finalizeSemanticAccounts(accounts map[string]*semanticAccountDigestState) {
	finalizeSemanticAccounts(accounts, d.addLogical, &d.census)
}

func (d *evmDigest) consumeSemanticMemiavlLeaf(accounts map[string]*semanticAccountDigestState, rawKey, rawVal []byte) error {
	return consumeSemanticMemiavlLeaf(accounts, rawKey, rawVal, d.addLogical, &d.census, "digest")
}

type semanticLogicalConsumer func(bucket string, physKey, logical, rawVal []byte)

func finalizeSemanticAccounts(accounts map[string]*semanticAccountDigestState, consume semanticLogicalConsumer, census *evmZeroCensus) {
	for addr, account := range accounts {
		if account.isZeroAccount() {
			if census != nil {
				census.ZeroAccounts++
			}
			continue
		}
		if account.hasZeroCodeHash() && account.isLiveAccount() && census != nil {
			if account.codeHashRow {
				census.LiveAccountsWithZeroCodeHashRow++
			} else {
				census.LiveAccountsWithoutCodeHashRow++
			}
		}
		physKey := ktype.EVMPhysicalKey(keys.EVMKeyNonce, []byte(addr))
		consume(flatkvBucketAccount, physKey, account.logicalPayload(), nil)
	}
}

func consumeSemanticMemiavlLeaf(accounts map[string]*semanticAccountDigestState, rawKey, rawVal []byte, consume semanticLogicalConsumer, census *evmZeroCensus, caller string) error {
	kind, keyBytes := keys.ParseEVMKey(rawKey)
	switch kind {
	case keys.EVMKeyEmpty:
		return fmt.Errorf("semantic memiavl %s: empty EVM key", caller)
	case keys.EVMKeyNonce:
		if len(rawVal) != 8 {
			return fmt.Errorf("semantic memiavl %s: nonce %X has length %d, want 8", caller, rawKey, len(rawVal))
		}
		if accounts == nil {
			return nil
		}
		account := getSemanticAccount(accounts, keyBytes)
		account.nonce = binary.BigEndian.Uint64(rawVal)
	case keys.EVMKeyCodeHash:
		if len(rawVal) != 32 {
			return fmt.Errorf("semantic memiavl %s: codehash %X has length %d, want 32", caller, rawKey, len(rawVal))
		}
		if isAllZero(rawVal) && census != nil {
			census.ZeroCodeHashRows++
		}
		if accounts == nil {
			return nil
		}
		account := getSemanticAccount(accounts, keyBytes)
		copy(account.codeHash[:], rawVal)
		account.codeHashRow = true
	case keys.EVMKeyBalance:
		if len(rawVal) != 32 {
			return fmt.Errorf("semantic memiavl %s: balance %X has length %d, want 32",
				caller, rawKey, len(rawVal))
		}
		if accounts == nil {
			return nil
		}
		account := getSemanticAccount(accounts, keyBytes)
		copy(account.balance[:], rawVal)
	case keys.EVMKeyCode:
		if len(rawVal) == 0 {
			if census != nil {
				census.EmptyCodeValues++
			}
			return nil
		}
		physKey := ktype.EVMPhysicalKey(keys.EVMKeyCode, keyBytes)
		consume(flatkvBucketCode, physKey, rawVal, rawVal)
	case keys.EVMKeyStorage:
		if len(rawVal) != 32 {
			return fmt.Errorf("semantic memiavl %s: storage %X has length %d, want 32", caller, rawKey, len(rawVal))
		}
		if isAllZero(rawVal) {
			if census != nil {
				census.ZeroStorageSlots++
			}
			return nil
		}
		physKey := ktype.EVMPhysicalKey(keys.EVMKeyStorage, keyBytes)
		consume(flatkvBucketStorage, physKey, rawVal, rawVal)
	case keys.EVMKeyMisc:
		physKey := ktype.ModulePhysicalKey(keys.EVMStoreKey, rawKey)
		consume(flatkvBucketMisc, physKey, rawVal, rawVal)
	default:
		return fmt.Errorf("semantic memiavl %s: unsupported EVM key kind %d for key %X", caller, kind, rawKey)
	}
	return nil
}

func getSemanticAccount(accounts map[string]*semanticAccountDigestState, addr []byte) *semanticAccountDigestState {
	key := string(addr)
	account, ok := accounts[key]
	if !ok {
		account = &semanticAccountDigestState{}
		accounts[key] = account
	}
	return account
}

func isAllZero(bz []byte) bool {
	for _, b := range bz {
		if b != 0 {
			return false
		}
	}
	return true
}

// readMemIAVLSnapshotVersion reads the version field from the snapshot metadata
// file (magic uint32 | format uint32 | version uint32, all little-endian).
func readMemIAVLSnapshotVersion(snapshotDir string) (int64, error) {
	bz, err := os.ReadFile(filepath.Join(filepath.Clean(snapshotDir), "metadata"))
	if err != nil {
		return 0, fmt.Errorf("read metadata: %w", err)
	}
	if len(bz) < 12 {
		return 0, fmt.Errorf("metadata too short: %d bytes", len(bz))
	}
	return int64(binary.LittleEndian.Uint32(bz[8:])), nil
}

func resolveMemIAVLEvmSnapshotDir(dbDir string, height int64) (string, error) {
	var snapshotName string
	if height == 0 {
		snapshotName = "current"
	} else {
		snapshotName = fmt.Sprintf("snapshot-%020d", height)
	}
	return filepath.Join(dbDir, snapshotName, keys.EVMStoreKey), nil
}
