package operations

// seidb flatkv-account — dump a single EVM account from FlatKV as JSON.
//
// This is the operator-facing "show me one account" tool. It mirrors the
// CLI flag style of `dump-iavl` / `dump-flatkv` (-d / --height / -o) and
// adds --address so the operator can zero in on a specific contract or
// externally-owned account without having to wade through a multi-GB raw
// hex dump.
//
// Output is JSON with a schema intentionally identical to
// `seidb iavl-account` (see flatkv_iavl_account.go) so the two tools can
// be diffed directly:
//
//   diff <(seidb flatkv-account -d $FLATKV_DIR --address 0x... --height H) \
//        <(seidb iavl-account   -d $MEMIAVL_DIR --address 0x... --height H)

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/sei-protocol/sei-chain/sei-db/common/keys"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/ktype"
	"github.com/spf13/cobra"
)

// AccountDump is the JSON document written by both flatkv-account and
// iavl-account. Field order is stable so operators can `diff` two outputs
// byte-for-byte.
type AccountDump struct {
	Backend      string            `json:"backend"`
	Height       int64             `json:"height"`
	RootHash     string            `json:"rootHash"`
	Address      string            `json:"address"`
	Nonce        uint64            `json:"nonce"`
	CodeHash     string            `json:"codeHash,omitempty"`
	CodeHex      string            `json:"codeHex,omitempty"`
	CodeSize     int               `json:"codeSize"`
	Storage      map[string]string `json:"storage"`
	StorageCount int               `json:"storageCount"`
}

func FlatKVAccountCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "flatkv-account",
		Short: "Dump one EVM account (balance/nonce/code/storage) from FlatKV as JSON",
		Long: `Dump the full state of a single EVM account from FlatKV at a given height.

The output JSON schema matches ` + "`seidb iavl-account`" + ` so the two can be
diffed directly to find FlatKV<->memiavl inconsistencies for one account.

Storage is collected by iterating the FlatKV storage DB and filtering keys
by address prefix, so cost is O(total-storage-keys) regardless of how many
slots the account has. For production-size chains this takes a few seconds.`,
		Run: executeFlatKVAccount,
	}

	cmd.PersistentFlags().StringP("db-dir", "d", "", "FlatKV data directory (e.g. /root/.sei/data/committer.db/flatkv)")
	cmd.PersistentFlags().Int64("height", 0, "Block height (0 = latest)")
	cmd.PersistentFlags().StringP("address", "a", "", "EVM account address (0x-prefixed, 20 bytes)")
	cmd.PersistentFlags().StringP("output", "o", "", "Output file (default: stdout)")
	cmd.PersistentFlags().Bool("pretty", true, "Indent JSON output for human reading")
	return cmd
}

func executeFlatKVAccount(cmd *cobra.Command, _ []string) {
	dbDir, _ := cmd.Flags().GetString("db-dir")
	addrHex, _ := cmd.Flags().GetString("address")
	height, _ := cmd.Flags().GetInt64("height")
	outputFile, _ := cmd.Flags().GetString("output")
	pretty, _ := cmd.Flags().GetBool("pretty")

	if dbDir == "" {
		panic("must provide --db-dir")
	}
	addrBytes, err := parseEVMAddress(addrHex)
	if err != nil {
		panic(fmt.Errorf("invalid --address: %w", err))
	}

	store, err := openFlatKVReadOnly(dbDir, height)
	if err != nil {
		panic(err)
	}
	defer func() { _ = store.Close() }()

	dump := dumpFlatKVAccount(store.CommitStore, addrBytes, store.Version())
	if err := writeAccountDump(dump, outputFile, pretty); err != nil {
		panic(err)
	}
}

// dumpFlatKVAccount is also exercised from higher-level tests that already
// hold a flatkv.Store interface (not a concrete *CommitStore), so it is
// typed against the interface.
func dumpFlatKVAccount(store flatkv.Store, addr []byte, height int64) *AccountDump {
	dump := &AccountDump{
		Backend:  "flatkv",
		Height:   height,
		RootHash: fmt.Sprintf("%X", store.RootHash()),
		Address:  fmt.Sprintf("0x%s", hex.EncodeToString(addr)),
		Storage:  map[string]string{},
	}

	// Nonce: stored as 8 big-endian bytes by flatkv.Get for EVMKeyNonce.
	if v, ok := store.Get(keys.EVMStoreKey, keys.BuildEVMKey(keys.EVMKeyNonce, addr)); ok && len(v) == 8 {
		dump.Nonce = decodeNonceBE(v)
	}

	// CodeHash: 32 bytes (zero-hash is returned as not-found by flatkv).
	if v, ok := store.Get(keys.EVMStoreKey, keys.BuildEVMKey(keys.EVMKeyCodeHash, addr)); ok {
		dump.CodeHash = fmt.Sprintf("0x%s", hex.EncodeToString(v))
	}

	// Code bytecode.
	if v, ok := store.Get(keys.EVMStoreKey, keys.BuildEVMKey(keys.EVMKeyCode, addr)); ok {
		dump.CodeHex = fmt.Sprintf("0x%s", hex.EncodeToString(v))
		dump.CodeSize = len(v)
	}

	// Storage: iterate the global raw view to discover which slots this
	// address has written, then call Get() for each slot to fetch the
	// decoded 32-byte value. This two-pass approach avoids reimplementing
	// the vtype deserialization here — the raw iterator yields the
	// on-disk `version+blockHeight+value` envelope, not the slot value
	// itself, so we MUST go through Get() (which routes via
	// DeserializeStorageData) to obtain what the RPC layer would see.
	//
	// Physical storage key layout (set by flatkv on commit):
	//   "evm/" + 0x03 + addr(20) + slot(32)
	iter := store.RawGlobalIterator()
	defer func() { _ = iter.Close() }()

	if iter.First() {
		for iter.Valid() {
			if slot, ok := matchStoragePhysKey(iter.Key(), addr); ok {
				// Decode via the canonical Get() path so the value
				// matches what eth_getStorageAt would return.
				logicalKey := keys.BuildEVMKey(keys.EVMKeyStorage, append(append([]byte(nil), addr...), slot...))
				if v, found := store.Get(keys.EVMStoreKey, logicalKey); found {
					dump.Storage[fmt.Sprintf("0x%s", hex.EncodeToString(slot))] =
						fmt.Sprintf("0x%s", hex.EncodeToString(v))
				}
			}
			iter.Next()
		}
	}
	if err := iter.Error(); err != nil {
		panic(fmt.Errorf("flatkv storage iteration: %w", err))
	}
	dump.StorageCount = len(dump.Storage)
	return dump
}

// matchStoragePhysKey returns the 32-byte slot if the given raw physical
// key is an EVM storage entry for addr. The FlatKV "global raw" iterator
// emits keys in the "module/" + inner layout produced by
// ktype.ModulePhysicalKey + ktype.EVMPhysicalKey, so we reuse
// ktype.StripModulePrefix to get the inner bytes.
func matchStoragePhysKey(physKey, addr []byte) ([]byte, bool) {
	moduleName, inner, err := ktype.StripModulePrefix(physKey)
	if err != nil || moduleName != keys.EVMStoreKey {
		return nil, false
	}
	if len(inner) != 1+20+32 {
		return nil, false
	}
	if inner[0] != 0x03 {
		return nil, false
	}
	if !bytes.Equal(inner[1:21], addr) {
		return nil, false
	}
	return inner[21:], true
}

// =============================================================================
// shared helpers also used by iavl-account
// =============================================================================

// parseEVMAddress accepts "0xHEX..." or bare hex, returns 20 bytes.
func parseEVMAddress(s string) ([]byte, error) {
	if s == "" {
		return nil, fmt.Errorf("address is required")
	}
	s = strings.TrimPrefix(strings.TrimSpace(s), "0x")
	s = strings.TrimPrefix(s, "0X")
	if len(s) != 40 {
		return nil, fmt.Errorf("expected 20-byte hex (40 chars), got %d chars", len(s))
	}
	b, err := hex.DecodeString(s)
	if err != nil {
		return nil, fmt.Errorf("decode hex: %w", err)
	}
	return b, nil
}

// decodeNonceBE decodes an 8-byte big-endian nonce written by flatkv /
// produced by the EVM keeper. Returns 0 on malformed input so the dump
// never panics on best-effort reads.
func decodeNonceBE(b []byte) uint64 {
	if len(b) != 8 {
		return 0
	}
	var n uint64
	for _, x := range b {
		n = (n << 8) | uint64(x)
	}
	return n
}

// writeAccountDump emits the dump as JSON to stdout or the given file.
// Keys inside `Storage` are sorted by slot hex so two backends produce
// byte-identical output when they agree.
func writeAccountDump(dump *AccountDump, outputFile string, pretty bool) error {
	// Sort storage keys so emission is deterministic.
	sorted := make(map[string]string, len(dump.Storage))
	keysSorted := make([]string, 0, len(dump.Storage))
	for k := range dump.Storage {
		keysSorted = append(keysSorted, k)
	}
	sort.Strings(keysSorted)
	for _, k := range keysSorted {
		sorted[k] = dump.Storage[k]
	}
	dump.Storage = sorted

	var (
		buf []byte
		err error
	)
	if pretty {
		buf, err = json.MarshalIndent(dump, "", "  ")
	} else {
		buf, err = json.Marshal(dump)
	}
	if err != nil {
		return fmt.Errorf("marshal JSON: %w", err)
	}
	buf = append(buf, '\n')

	if outputFile == "" {
		_, err := os.Stdout.Write(buf)
		return err
	}
	return os.WriteFile(outputFile, buf, 0o644)
}
