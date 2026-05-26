package operations

// seidb iavl-account — dump a single EVM account from memiavl as JSON.
//
// Sibling of `seidb flatkv-account`. Emits the exact same JSON schema so
// the two dumps can be diffed directly. Use when you suspect FlatKV has
// drifted from memiavl (the ground truth) and want to know which specific
// keys disagree for a given account.

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-db/common/keys"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/memiavl"
	"github.com/spf13/cobra"
)

func IAVLAccountCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "iavl-account",
		Short: "Dump one EVM account (balance/nonce/code/storage) from memIAVL as JSON (paired with flatkv-account)",
		Long: `Dump the full state of a single EVM account from the memIAVL "evm" tree
at a given height. Output JSON schema is identical to ` + "`seidb flatkv-account`" + `
so the two can be diffed directly.`,
		Run: executeIAVLAccount,
	}

	cmd.PersistentFlags().StringP("db-dir", "d", "", "memIAVL data directory (e.g. /root/.sei/data/committer.db)")
	cmd.PersistentFlags().Int64("height", 0, "Block height (0 = latest)")
	cmd.PersistentFlags().StringP("address", "a", "", "EVM account address (0x-prefixed, 20 bytes)")
	cmd.PersistentFlags().StringP("output", "o", "", "Output file (default: stdout)")
	cmd.PersistentFlags().Bool("pretty", true, "Indent JSON output for human reading")
	return cmd
}

func executeIAVLAccount(cmd *cobra.Command, _ []string) {
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

	// ReadOnly skips acquiring memiavl's LOCK file, so this command can
	// safely run against a live seid node's committer.db without
	// fighting the main process for the flock. Without this, opening a
	// live node's memiavl dir fails with "fail to lock db".
	db, err := memiavl.OpenDB(height, memiavl.Options{
		Dir:             dbDir,
		ZeroCopy:        true,
		CreateIfMissing: false,
		ReadOnly:        true,
	})
	if err != nil {
		panic(fmt.Errorf("open memiavl: %w", err))
	}
	defer func() { _ = db.Close() }()

	tree := db.TreeByName(keys.EVMStoreKey)
	if tree == nil {
		panic(fmt.Errorf("memiavl has no %q tree at this node; is this a valid seid state dir?",
			keys.EVMStoreKey))
	}

	dump := dumpIAVLAccount(tree, addrBytes)
	if err := writeAccountDump(dump, outputFile, pretty); err != nil {
		panic(err)
	}
}

func dumpIAVLAccount(tree *memiavl.Tree, addr []byte) *AccountDump {
	dump := &AccountDump{
		Backend:  "memiavl",
		Height:   tree.Version(),
		RootHash: fmt.Sprintf("%X", tree.RootHash()),
		Address:  fmt.Sprintf("0x%s", hex.EncodeToString(addr)),
		Storage:  map[string]string{},
	}

	// Nonce: the EVM keeper stores raw uint64-BE under key 0x0a||addr.
	if v := tree.Get(keys.BuildEVMKey(keys.EVMKeyNonce, addr)); v != nil {
		if len(v) == 8 {
			dump.Nonce = binary.BigEndian.Uint64(v)
		}
	}

	// CodeHash: 32 bytes under 0x08||addr.
	if v := tree.Get(keys.BuildEVMKey(keys.EVMKeyCodeHash, addr)); v != nil {
		dump.CodeHash = fmt.Sprintf("0x%s", hex.EncodeToString(v))
	}

	// Code: raw bytecode under 0x07||addr.
	if v := tree.Get(keys.BuildEVMKey(keys.EVMKeyCode, addr)); v != nil {
		dump.CodeHex = fmt.Sprintf("0x%s", hex.EncodeToString(v))
		dump.CodeSize = len(v)
	}

	// Storage: iterate over the narrow range [0x03||addr, 0x03||(addr+1)).
	// memiavl's Iterator is half-open, and since storage keys are
	// 0x03||addr||slot (53 bytes), the open-ended upper bound is
	// 0x03||(addr+1). This is orders of magnitude cheaper than FlatKV's
	// global sweep because memiavl supports bounded iteration natively.
	start := keys.BuildEVMKey(keys.EVMKeyStorage, addr)
	var upper []byte
	if upperAddr := incrementBE(addr); upperAddr != nil {
		upper = append([]byte{0x03}, upperAddr...)
	}
	// upper == nil means addr was 0xFF...FF; the memiavl iterator
	// treats a nil upper bound as "no upper bound", which is correct.

	it := tree.Iterator(start, upper, true)
	defer it.Close()
	for ; it.Valid(); it.Next() {
		k := it.Key()
		// Only count keys matching exactly 0x03||addr||slot.
		if len(k) != 1+20+32 {
			continue
		}
		if k[0] != 0x03 || !bytes.Equal(k[1:21], addr) {
			continue
		}
		slot := k[21:]
		dump.Storage[fmt.Sprintf("0x%s", hex.EncodeToString(slot))] =
			fmt.Sprintf("0x%s", hex.EncodeToString(it.Value()))
	}
	dump.StorageCount = len(dump.Storage)
	return dump
}

// incrementBE returns b+1 treated as a big-endian unsigned integer, or
// nil if b is all-0xFF (i.e. the increment would overflow). Used to
// build the open-ended upper bound for per-address storage iteration.
func incrementBE(b []byte) []byte {
	out := append([]byte(nil), b...)
	for i := len(out) - 1; i >= 0; i-- {
		if out[i] != 0xFF {
			out[i]++
			for j := i + 1; j < len(out); j++ {
				out[j] = 0
			}
			return out
		}
	}
	return nil
}
