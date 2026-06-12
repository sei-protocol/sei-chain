package operations

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"

	"github.com/sei-protocol/sei-chain/sei-db/common/keys"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/vtype"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/migration"
	"github.com/spf13/cobra"
)

// migrationVersionPhysKey is the FlatKV physical key of the migration-version
// marker. FlatKV stores non-EVM module rows as "<module>/<key>"; the
// MigrationManager writes this marker only to the new database (flatkv) and
// never to memiavl. It therefore shows up in the FlatKV legacy bucket but is
// absent from a memiavl-only node, so excluding it lets the FlatKV digest be
// compared apples-to-apples against memiavl-only output.
var migrationVersionPhysKey = []byte(migration.MigrationStore + "/" + migration.MigrationVersionKey)

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
//   - memIAVL: each evm leaf is fed through flatkv.ImportTranslator, which
//     applies the exact same classifyAndPrefix + account-merge logic FlatKV
//     uses, so the emitted physical keys match by construction.
//
// The per-bucket accumulator is an XOR of sha256(len(key)||key||len(val)||val),
// which is order-independent: it does not matter that FlatKV iterates in pebble
// global order while memIAVL is scanned by leaf index, nor that merged accounts
// are flushed out of order at Finalize.
//
// The legacy bucket is intentionally excluded: it is a fallback path for
// non-EVM module-prefixed rows and can carry validator-local dual-write noise.
//
// Usage:
//
//	# FlatKV digest at a height (WAL-replays to it). Prints per-bucket xors,
//	# DIGEST(account+code+storage), DIGEST(account+code+storage+legacy), and a
//	# second DIGEST(...+legacy) tagged [excl migration-version].
//	seidb evm-logical-digest --backend flatkv \
//	    --db-dir /.sei/data/state_commit/flatkv --height 213200000
//
//	# memIAVL digest at the same height (0 = current symlink). The marker is
//	# absent here, so its DIGEST(...+legacy) is what the FlatKV
//	# [excl migration-version] line must equal for a clean migration.
//	seidb evm-logical-digest --backend memiavl \
//	    --db-dir /.sei/data/state_commit/memiavl --height 213200000
//
//	# Compare a migrated FlatKV node against a memiavl-only node at height H:
//	#   FlatKV "DIGEST(...+legacy) [excl migration-version]"  ==  memiavl "DIGEST(...+legacy)"
//	# (the WITH-marker FlatKV line will differ by exactly that one row, count-1.)
//
//	# Inspect one bucket instead of the global digest (e.g. list storage rows
//	# under a key prefix, sharded by the next 2 bytes):
//	seidb evm-logical-digest --backend flatkv -d <dir> --height H \
//	    --inspect-bucket storage --key-prefix 03 --shard-next-bytes 2
//	seidb evm-logical-digest --backend flatkv -d <dir> --height H \
//	    --inspect-bucket account --list --list-limit 50 --details
//
//	# Hunt the single diverging entry between two runs: when two bucket digests
//	# differ by exactly one row, XOR the two xor= accumulators (32-byte hex) and
//	# pass the result; every matching row is printed as FOUND-HASH.
//	seidb evm-logical-digest --backend flatkv -d <dir> --height H \
//	    --find-hash <32-byte-hex>
func EvmLogicalDigestCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "evm-logical-digest",
		Short: "Backend-independent digest of EVM logical state (account/code/storage) for memiavl vs flatkv comparison",
		RunE:  runEvmLogicalDigest,
	}
	cmd.Flags().String("backend", "", "Backend to read: flatkv | memiavl")
	cmd.Flags().StringP("db-dir", "d", "", "For flatkv: the flatkv data dir. For memiavl: the memiavl root dir (contains current/ and snapshot-* )")
	cmd.Flags().Int64("height", 0, "Target version. flatkv WAL-replays to it; memiavl resolves snapshot-<height>/evm (0 = current symlink)")
	cmd.Flags().String("inspect-bucket", "", "Inspect one normalized bucket (account|code|storage|legacy) instead of printing the global digest")
	cmd.Flags().Int("key-offset", 0, "Inspect mode: byte offset into physical key before applying --key-prefix / sharding")
	cmd.Flags().String("key-prefix", "", "Inspect mode: hex prefix, relative to --key-offset, used to filter physical keys")
	cmd.Flags().Int("shard-next-bytes", 0, "Inspect mode: group matching keys by this many bytes after --key-prefix")
	cmd.Flags().Bool("list", false, "Inspect mode: list matching key/logical-value pairs instead of shard digests")
	cmd.Flags().Int("list-limit", 1000, "Inspect mode: maximum pairs to print with --list; <=0 means unlimited")
	cmd.Flags().Bool("details", false, "Inspect list mode: include backend-specific version metadata")
	cmd.Flags().String("find-hash", "", "Optional 32-byte hex per-entry hash to hunt for. When two bucket digests differ by exactly one entry, the XOR of the two accumulators IS that entry's hash; this prints every entry whose sha256(len(key)||key||len(val)||val) matches")
	return cmd
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
	legacy  digestBucket

	// findTarget, when non-nil, is a per-entry hash to hunt for; every
	// matching entry is printed with its bucket, physical key, and values.
	findTarget []byte

	// migrationVersionFound/Hash capture the FlatKV-only
	// "migration/migration-version" marker. It is folded into the legacy
	// bucket like any other row, but tracked separately so print can also
	// report a variant with it XORed back out. A memiavl-only node never
	// owns this key, so that "excl migration-version" variant is what
	// should match memiavl-only output exactly.
	migrationVersionFound bool
	migrationVersionHash  [sha256.Size]byte
}

// consume routes a physical (key, serialized-value) pair into its canonical
// bucket, strips the vtype header, and folds the logical payload into the
// accumulator. The legacy bucket (EVM keys with no canonical prefix: address
// mappings, codesize, etc.) is digested too — its LegacyData value is just as
// height-independent (version+blockHeight header stripped) as the other three.
func (d *evmDigest) consume(physKey, val []byte) error {
	bucket, logical, err := normalizeEVMFlatKVPair(physKey, val)
	if err != nil {
		return err
	}
	sum := entryHash(physKey, logical)
	if d.findTarget != nil && bytes.Equal(sum[:], d.findTarget) {
		fmt.Printf("FOUND-HASH bucket=%s keyhex=%X logicalhex=%X rawhex=%X\n", bucket, physKey, logical, val)
	}
	switch bucket {
	case flatkvBucketAccount:
		d.account.addSum(sum)
	case flatkvBucketCode:
		d.code.addSum(sum)
	case flatkvBucketStorage:
		d.storage.addSum(sum)
	default: // flatkvBucketLegacy
		d.legacy.addSum(sum)
		if bytes.Equal(physKey, migrationVersionPhysKey) {
			d.migrationVersionFound = true
			d.migrationVersionHash = sum
		}
	}
	return nil
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
	default: // flatkvBucketLegacy
		ld, err := vtype.DeserializeLegacyData(val)
		if err != nil {
			return "", nil, fmt.Errorf("deserialize legacy %X: %w", physKey, err)
		}
		return bucket, ld.GetValue(), nil
	}
}

func (d *evmDigest) print(version int64) {
	fmt.Printf("version: %d\n", version)
	fmt.Printf("account  count=%d xor=%X\n", d.account.count, d.account.acc)
	fmt.Printf("code     count=%d xor=%X\n", d.code.count, d.code.acc)
	fmt.Printf("storage  count=%d xor=%X\n", d.storage.count, d.storage.acc)
	fmt.Printf("legacy   count=%d xor=%X\n", d.legacy.count, d.legacy.acc)
	combined := sha256.New()
	_, _ = combined.Write(d.account.acc[:])
	_, _ = combined.Write(d.code.acc[:])
	_, _ = combined.Write(d.storage.acc[:])
	fmt.Printf("DIGEST(account+code+storage) %X count=%d\n",
		combined.Sum(nil), d.account.count+d.code.count+d.storage.count)
	combined4 := sha256.New()
	_, _ = combined4.Write(d.account.acc[:])
	_, _ = combined4.Write(d.code.acc[:])
	_, _ = combined4.Write(d.storage.acc[:])
	_, _ = combined4.Write(d.legacy.acc[:])
	fmt.Printf("DIGEST(account+code+storage+legacy) %X count=%d\n",
		combined4.Sum(nil), d.account.count+d.code.count+d.storage.count+d.legacy.count)

	// Second variant: legacy with the FlatKV-only migration-version marker
	// XORed back out, plus the corresponding combined digest. This is the
	// value to compare against a memiavl-only node, which never owns the
	// migration-version key. When the marker is absent (e.g. memiavl backend,
	// or a not-yet-migrated flatkv node) the excl variant is identical to the
	// line above, which we state explicitly so both runs are directly diffable.
	if d.migrationVersionFound {
		legacyExcl := d.legacy.acc
		for i := 0; i < sha256.Size; i++ {
			legacyExcl[i] ^= d.migrationVersionHash[i]
		}
		fmt.Printf("legacy   count=%d xor=%X [excl migration-version]\n",
			d.legacy.count-1, legacyExcl)
		combinedExcl := sha256.New()
		_, _ = combinedExcl.Write(d.account.acc[:])
		_, _ = combinedExcl.Write(d.code.acc[:])
		_, _ = combinedExcl.Write(d.storage.acc[:])
		_, _ = combinedExcl.Write(legacyExcl[:])
		fmt.Printf("DIGEST(account+code+storage+legacy) %X count=%d [excl migration-version]\n",
			combinedExcl.Sum(nil), d.account.count+d.code.count+d.storage.count+d.legacy.count-1)
	} else {
		fmt.Printf("DIGEST(account+code+storage+legacy) %X count=%d [excl migration-version: marker absent, identical to above]\n",
			combined4.Sum(nil), d.account.count+d.code.count+d.storage.count+d.legacy.count)
	}
}

func runEvmLogicalDigest(cmd *cobra.Command, _ []string) error {
	backend, _ := cmd.Flags().GetString("backend")
	dbDir, _ := cmd.Flags().GetString("db-dir")
	height, _ := cmd.Flags().GetInt64("height")
	if dbDir == "" {
		return errors.New("must provide --db-dir")
	}
	inspectBucket, _ := cmd.Flags().GetString("inspect-bucket")
	if inspectBucket != "" {
		return runEvmLogicalInspect(cmd, backend, dbDir, height, inspectBucket)
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
		return digestMemIAVL(dbDir, height, findTarget)
	default:
		return fmt.Errorf("unknown --backend %q (want flatkv|memiavl)", backend)
	}
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
	var seen, legacy uint64
	for ; iter.Valid(); iter.Next() {
		k := iter.Key()
		if seen < 5 {
			fmt.Printf("  DEBUG key#%d bucket=%s keyhex=%X\n", seen, classifyFlatKVPhysicalKey(k), k)
		}
		seen++
		if classifyFlatKVPhysicalKey(k) == flatkvBucketLegacy {
			legacy++
		}
		if err := d.consume(k, iter.Value()); err != nil {
			return err
		}
		if seen%20000000 == 0 {
			fmt.Printf("  ...flatkv seen=%d legacy=%d acc=%d code=%d storage=%d\n", seen, legacy, d.account.count, d.code.count, d.storage.count)
		}
	}
	if err := iter.Error(); err != nil {
		return fmt.Errorf("iterate: %w", err)
	}
	d.print(version)
	return nil
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
	listed         uint64
}

func runEvmLogicalInspect(cmd *cobra.Command, backend, dbDir string, height int64, inspectBucket string) error {
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
		return inspectMemIAVL(dbDir, height, acc)
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
	if bucket != a.inspectBucket {
		return nil
	}
	if len(physKey) < a.keyOffset {
		return nil
	}
	rel := physKey[a.keyOffset:]
	if !bytes.HasPrefix(rel, a.keyPrefix) {
		return nil
	}
	a.matched++
	if a.list {
		if a.listLimit <= 0 || int(a.listed) < a.listLimit {
			if meta != "" {
				fmt.Printf("key=%X logical=%X %s\n", physKey, logical, meta)
			} else {
				fmt.Printf("key=%X logical=%X\n", physKey, logical)
			}
			a.listed++
		}
		return nil
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
	return nil
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
		fmt.Printf("shard=%s count=%d xor=%X\n", k, d.count, d.acc)
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

func inspectMemIAVL(dbDir string, height int64, acc *inspectAccumulator) error {
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
	kvsPath := filepath.Join(evmSnapshotDir, "kvs")
	f, err := os.Open(filepath.Clean(kvsPath))
	if err != nil {
		return fmt.Errorf("open kvs %s: %w", kvsPath, err)
	}
	defer func() { _ = f.Close() }()
	r := bufio.NewReaderSize(f, 16*1024*1024)

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

	var lenbuf [4]byte
	for {
		if _, err := io.ReadFull(r, lenbuf[:]); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return fmt.Errorf("read key len: %w", err)
		}
		keyLen := binary.LittleEndian.Uint32(lenbuf[:])
		k := make([]byte, keyLen)
		if _, err := io.ReadFull(r, k); err != nil {
			return fmt.Errorf("read key: %w", err)
		}
		if _, err := io.ReadFull(r, lenbuf[:]); err != nil {
			return fmt.Errorf("read val len: %w", err)
		}
		valLen := binary.LittleEndian.Uint32(lenbuf[:])
		v := make([]byte, valLen)
		if _, err := io.ReadFull(r, v); err != nil {
			return fmt.Errorf("read val: %w", err)
		}
		leaves++
		if leaves%20000000 == 0 {
			fmt.Printf("  ...memiavl inspect leaves=%d matched=%d\n", leaves, acc.matched)
		}
		batch = append(batch, &proto.KVPair{Key: k, Value: v})
		if len(batch) >= batchCap {
			if ferr := flush(); ferr != nil {
				return ferr
			}
		}
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
		if kind, _ := keys.ParseEVMKey(k); kind != keys.EVMKeyStorage {
			continue
		}
		value, err := vtype.ParseStorageValue(v)
		if err != nil {
			return fmt.Errorf("parse storage value %X: %w", k, err)
		}
		leafVersion := int64(binary.LittleEndian.Uint32(leafbuf[:4]))
		physKey := make([]byte, len(keys.EVMStoreKey)+1+len(k))
		copy(physKey, keys.EVMStoreKey)
		physKey[len(keys.EVMStoreKey)] = '/'
		copy(physKey[len(keys.EVMStoreKey)+1:], k)
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
		ld, err := vtype.DeserializeLegacyData(val)
		if err != nil {
			return "", fmt.Errorf("deserialize legacy %X: %w", physKey, err)
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
func digestMemIAVL(dbDir string, height int64, findTarget []byte) error {
	evmSnapshotDir, err := resolveMemIAVLEvmSnapshotDir(dbDir, height)
	if err != nil {
		return err
	}

	version, err := readMemIAVLSnapshotVersion(evmSnapshotDir)
	if err != nil {
		return err
	}

	kvsPath := filepath.Join(evmSnapshotDir, "kvs")
	f, err := os.Open(filepath.Clean(kvsPath))
	if err != nil {
		return fmt.Errorf("open kvs %s: %w", kvsPath, err)
	}
	defer func() { _ = f.Close() }()
	r := bufio.NewReaderSize(f, 16*1024*1024)

	// Route EVERY leaf through ImportTranslator and then through the same
	// `consume` used for the FlatKV side. This makes the two backends
	// byte-identical by construction:
	//
	//   - Translate runs the exact classifyAndPrefix + processStorageChanges /
	//     processCodeChanges / processLegacyChanges / mergeAccountUpdates that
	//     CommitStore.ApplyChangeSets uses, so the physical keys AND the
	//     FlatKV-serialized values it emits match what is physically stored in
	//     pebble on the FlatKV node.
	//   - consume then deserializes those values (DeserializeStorageData /
	//     DeserializeCodeData / DeserializeAccountData) and digests only the
	//     height-independent logical payload — the identical code path both
	//     backends take.
	//
	// We deliberately do NOT take the old hybrid shortcut (emitting node.Value()
	// raw with a hand-built key for storage/code): a raw memIAVL leaf value is
	// not guaranteed to be byte-identical to FlatKV's normalized StorageData /
	// CodeData payload, which would make the digest diverge spuriously even when
	// the underlying state is identical.
	//
	// Memory: Translate streams storage/code/legacy pairs out immediately and
	// only buffers account fragments (nonce/codehash) until Finalize, so feeding
	// all leaves through it does not blow up RSS beyond the account buffer.
	translator := flatkv.NewImportTranslator(0)
	d := evmDigest{findTarget: findTarget}
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
			if cerr := d.consume(p.Key, p.Value); cerr != nil {
				return cerr
			}
		}
		batch = batch[:0]
		return nil
	}

	var lenbuf [4]byte
	for {
		// key length
		if _, err := io.ReadFull(r, lenbuf[:]); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return fmt.Errorf("read key len: %w", err)
		}
		keyLen := binary.LittleEndian.Uint32(lenbuf[:])
		k := make([]byte, keyLen)
		if _, err := io.ReadFull(r, k); err != nil {
			return fmt.Errorf("read key: %w", err)
		}
		// value length
		if _, err := io.ReadFull(r, lenbuf[:]); err != nil {
			return fmt.Errorf("read val len: %w", err)
		}
		valLen := binary.LittleEndian.Uint32(lenbuf[:])
		v := make([]byte, valLen)
		if _, err := io.ReadFull(r, v); err != nil {
			return fmt.Errorf("read val: %w", err)
		}

		leaves++
		if leaves%20000000 == 0 {
			fmt.Printf("  ...memiavl leaves=%d acc=%d code=%d storage=%d\n",
				leaves, d.account.count, d.code.count, d.storage.count)
		}

		batch = append(batch, &proto.KVPair{Key: k, Value: v})
		if len(batch) >= batchCap {
			if ferr := flush(); ferr != nil {
				return ferr
			}
		}
	}

	if err := flush(); err != nil {
		return err
	}
	// Flush merged accounts buffered across all batches.
	for _, p := range translator.Finalize() {
		if cerr := d.consume(p.Key, p.Value); cerr != nil {
			return cerr
		}
	}

	fmt.Printf("  memiavl total leaves=%d\n", leaves)
	d.print(version)
	return nil
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
