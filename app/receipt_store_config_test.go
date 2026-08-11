package app

import (
	"fmt"
	"math"
	"testing"

	"github.com/spf13/cast"

	"github.com/sei-protocol/sei-chain/app/params"
	"github.com/sei-protocol/sei-chain/sei-cosmos/server"
	srvconfig "github.com/sei-protocol/sei-chain/sei-cosmos/server/config"
	"github.com/sei-protocol/sei-chain/testutil/configtest"
)

// Tests for min-retain-blocks, the only key in this tree that two live consumers read. One
// operator value becomes both a Tendermint block-retention height and an EVM receipt retention
// window, through different casts.
//
// testutil/configtest/AGENTS.md holds the fan-out's architecture and the two gaps it leaves open,
// including which receipt-store backend survives a saturated retention window by an ordinary bound
// and which survives by an accident nothing here pins.

// minRetainBlocksFanOut is what one operator value becomes on each side.
var minRetainBlocksFanOut = []struct {
	// raw is an any because the input type is part of the case. Both readers cast whatever
	// appOpts.Get returns, and which Go type that is depends on the layer that set the key: the env
	// layer and a quoted app.toml entry hand them a string, an unquoted integer decodes to int64,
	// and an unquoted float literal decodes to float64. Only the float case makes the two casts
	// disagree with the receipt side positive, so a table of strings could not express it.
	raw     any
	receipt int // app/receipt_store_config.go:27, through cast.ToInt. Not read when saturates.
	// block records what cast.ToUint64 produces, held against a number rather than a second call to
	// the same function, since an assertion comparing a call to itself passes for any reader.
	//
	// It and the saturates rows assert spf13/cast and Go's float-to-int conversion rather than
	// anything in this tree, so a cast bump can redden this package for a reason unrelated to
	// configuration resolution, reported as a message about receipt retention.
	block uint64 // cmd/seid/cmd/root.go:297, through cast.ToUint64
	// saturates marks a row whose receipt cast Go leaves implementation-defined, so no literal is a
	// correct prediction and the property is asserted in place of the value. Same reason a
	// NumCPU-derived default is recorded as a DerivedDefault rather than a number.
	saturates bool
}{
	{"0", 0, 0, false},                                       // keep everything, both sides
	{"100000", 100000, 100000, false},                        // the ordinary case: one value, two policies
	{"200000", 200000, 200000, false},                        // a second ordinary value, same class
	{"-5", -5, 0, false},                                     // ToInt keeps the negative, ToUint64 floors
	{"9223372036854775808", 0, 9223372036854775808, false},   // past int64: ToInt floors, ToUint64 keeps
	{"18446744073709551615", 0, 18446744073709551615, false}, // the uint64 ceiling, same shape
	{"not-a-number", 0, 0, false},                            // both floor, so both keep everything
	// A leading sign is where the two string casts could most plausibly part company, because ParseInt
	// takes a sign prefix. cast strips a leading + before ParseUint sees it, so they agree, and the row
	// exists so that stops being something a reader takes on trust.
	{"+5", 5, 5, false},
	// Unquoted in app.toml, so the decode hands both readers the operator's own type.
	{int64(100000), 100000, 100000, false},
	// The one shape where the two casts can disagree with the receipt side positive. Reachable as
	// min-retain-blocks = 1e19, which decodes to float64. Go leaves a float-to-int conversion whose
	// result the target cannot represent implementation-defined, and the two architectures the fleet
	// ships take it differently: amd64 lowers it to a bare CVTTSD2SQ and gets the x86 indefinite value,
	// MinInt64, while arm64 lowers it to FCVTZS and saturates to MaxInt64. So the receipt column is a
	// property here rather than a number. The block column is not affected: uint64 of a float below
	// 2^64 is the exact magnitude on both.
	{float64(1e19), 0, 10000000000000000000, true},
	// Just above 2^63, so the same conversion from a value an operator could plausibly mistype.
	{float64(9.3e18), 0, 9300000000000000000, true},
}

// TestMinRetainBlocksFanOutNeverResolvesReceiptsToAPruningWindow asserts that the receipt side never
// resolves to a window that would prune. It says the config layer resolves to a safe value, not that
// what happens to that value downstream is safe.
//
// The name says resolves rather than prunes because whether a resolved value goes on to expire
// anything is a sei-db property this layer cannot reach.
func TestMinRetainBlocksFanOutNeverResolvesReceiptsToAPruningWindow(t *testing.T) {
	for _, row := range minRetainBlocksFanOut {
		t.Run(fmt.Sprintf("%v(%T)", row.raw, row.raw), func(t *testing.T) {
			receiptConfig, err := readReceiptStoreConfig(t.TempDir(), mapAppOpts{
				server.FlagMinRetainBlocks: row.raw,
			})
			if err != nil {
				t.Fatalf("readReceiptStoreConfig(%v): %v", row.raw, err)
			}

			// Where Go leaves the conversion implementation-defined, the property stands in for the
			// value: the receipt side must be at or below zero, or the saturating positive extreme.
			// Anything else positive is a real retention window and would prune.
			if row.saturates {
				if kr := receiptConfig.KeepRecent; kr > 0 && kr != math.MaxInt64 {
					t.Errorf("min-retain-blocks=%v resolved the receipt store's KeepRecent to %d. Go "+
						"leaves this conversion implementation-defined, so the two outcomes this suite "+
						"accepts are at-or-below zero, which the KeepRecent>0 guard refuses, and MaxInt64, "+
						"which sei-db is relied on to render harmless by overflow and which nothing here "+
						"pins. A positive window that is neither is a real retention schedule an operator "+
						"never set", row.raw, kr)
				}
			} else if receiptConfig.KeepRecent != row.receipt {
				t.Errorf("min-retain-blocks=%v resolves the receipt store's KeepRecent to %d where this "+
					"row predicts %d. The prediction describes app/receipt_store_config.go, so a reader "+
					"you changed deliberately means updating the row and saying what a node now retains "+
					"for receipts; a reader you did not change means the cast moved underneath it",
					row.raw, receiptConfig.KeepRecent, row.receipt)
			}
			// The recording, against the cast the block side applies.
			if got := cast.ToUint64(row.raw); got != row.block {
				t.Errorf("min-retain-blocks=%v casts to %d through cast.ToUint64, recorded as %d. The "+
					"block column is a recording of the cast rather than a pin on root.go:297, so "+
					"update it and say what a node now retains", row.raw, got, row.block)
			}

			// A saturating row is done here, and this sits above the agreement check rather than below
			// it so that check never reads a receipt column holding no prediction. It stays below the
			// block check, because the block column is a real prediction on these rows.
			if row.saturates {
				return
			}

			// Same number on both sides means the fan-out is a plain coupling and there is nothing to
			// check. The sign test is part of that question rather than belt-and-braces: converting a
			// negative receipt value to uint64 wraps it to the top of the range, so a row pairing a
			// negative with a large block value would read as agreement and skip the check below.
			if row.receipt >= 0 && uint64(row.receipt) == row.block {
				return
			}
			// Positive is the whole test, because a positive KeepRecent is the one state that arms
			// receipt expiry on either backend: pebbledb starts a pruner
			// (sei-db/ledger_db/receipt/receipt_store.go:363) and litt sets a TTL
			// (sei-db/ledger_db/receipt/litt_receipt_store.go:138). At or below zero, neither does.
			if row.receipt > 0 {
				t.Errorf("min-retain-blocks=%v gives %d for receipt retention and %d for block "+
					"retention. The two disagree and the receipt side is a positive window that is not "+
					"the saturation case, so a value the block side handled one way starts pruning EVM "+
					"receipts on a schedule the operator never set", row.raw, row.receipt, row.block)
			}
		})
	}
}

// minRetainBlocksKeyName records the operator-facing spelling of the key both retention readers share.
//
// A KeyName rather than a KeySpec, because it resolves into two different structs through two
// different casts and no single row could name a Path for it. The record exists because nothing else
// in the tree pins this constant's value: sei-cosmos/server/config's base_config record belongs to
// GetConfig, which reads the literal independently, so renaming server.FlagMinRetainBlocks moves the
// key for both live readers this file exists to hold still. That rename does fail today, where a test
// asks the start command to set a flag it no longer has, but it fails as "no such flag" rather than as
// an operator-facing key having moved, and it fails in another package.
//
// Spelled through the reader's own constant, which is the position the record exists for: a rename
// lands in this golden as a diff rather than only as a set failure somewhere else.
var minRetainBlocksKeyName = []configtest.KeyName{configtest.KeyName(server.FlagMinRetainBlocks)}

// TestMinRetainBlocksKeyNameMatchesTheRecordedName pins the spelling of the key above.
func TestMinRetainBlocksKeyNameMatchesTheRecordedName(t *testing.T) {
	configtest.CheckKeyNames(t, "min_retain_blocks", nil, minRetainBlocksKeyName...)
}

// TestMinRetainBlocksFullNodeModeCapsReceiptRetention records the case where the fan-out bites, which
// is the default one.
//
// seid init defaults --mode to full, and setFullnodeTypeAppConfig sets min-retain-blocks to 100000 for
// Tendermint block pruning. The same key reaches the receipt store as KeepRecent, so a positive value
// arms receipt pruning: the default pebbledb backend starts a pruner from it
// (sei-db/ledger_db/receipt/receipt_store.go:176) and the litt backend applies a TTL and a read floor
// from it. So a default full node caps EVM receipt retention at a block count nobody set for that
// purpose, and the key's own documentation describes only block retention.
//
// An operator has no setting that keeps both. Block pruning at 100000 caps receipts, and keeping every
// receipt means min-retain-blocks of 0, which also stops pruning blocks. That is the coupling recorded
// here, and it is tracked as PLT-976.
//
// The value is read out of SetAppConfigByMode rather than written here, for the reason the archive test
// gives: a transcribed 100000 would hold whatever the mode did, so moving the mode off 100000 would
// change receipt retention on every full node with nothing reporting it.
func TestMinRetainBlocksFullNodeModeCapsReceiptRetention(t *testing.T) {
	configtest.Isolate(t)

	full := srvconfig.DefaultConfig()
	params.SetAppConfigByMode(full, params.NodeModeFull)

	if full.MinRetainBlocks != 100000 {
		t.Fatalf("full mode now sets min-retain-blocks to %d rather than 100000. That value is also the "+
			"receipt store's KeepRecent, so this moves how much EVM receipt history every default full "+
			"node keeps, not only how many Tendermint blocks it retains. If the change is deliberate, "+
			"say what receipt history a full node is now expected to serve", full.MinRetainBlocks)
	}

	receiptConfig, err := readReceiptStoreConfig(t.TempDir(), mapAppOpts{
		server.FlagMinRetainBlocks: full.MinRetainBlocks,
	})
	if err != nil {
		t.Fatalf("readReceiptStoreConfig: %v", err)
	}
	// The state that arms pruning, stated on its own so the name of this test stays true, and
	// established before the comparison below so that comparison needs no unguarded conversion.
	if receiptConfig.KeepRecent <= 0 {
		t.Errorf("full mode now leaves the receipt store's KeepRecent at %d, which is the no-pruning "+
			"state. That is a better end state for receipt history and it changes what a full node "+
			"keeps, so record what block retention it now gets instead", receiptConfig.KeepRecent)
	}

	// The mode's value reaching the reader intact. Compared as uint64 rather than converting the
	// reader's int down, because the sign is already established above and widening cannot overflow
	// where narrowing could.
	if uint64(receiptConfig.KeepRecent) != full.MinRetainBlocks {
		t.Errorf("full mode's min-retain-blocks of %d reached the receipt store as KeepRecent=%d. The "+
			"fan-out is what this file records, so the two stopping agreeing is the thing to explain",
			full.MinRetainBlocks, receiptConfig.KeepRecent)
	}
}

// TestMinRetainBlocksArchiveModeKeepsBothRetentionsOpen records that the archive path is aligned.
//
// PLT-955 is an archive node pruning state history because its mode's state-store settings are
// discarded. This is the neighbouring question, asked and answered so nobody re-investigates it: the
// archive mode leaves min-retain-blocks at 0, which is keep-all for Tendermint blocks, and 0 through
// the receipt cast leaves KeepRecent at 0, which is no pruning. So the fan-out does not give archive
// a second history-loss path.
//
// Two things are checked and they catch different changes. The first assertion reads archive's value
// out of SetAppConfigByMode, so a mode that stops keeping every block fails there; that is the one
// that would have caught the change this test exists to notice, and it is why the value is sourced
// rather than transcribed. The second hands that value to the reader, which with the first assertion
// standing is always 0, so what it adds is narrower: it fails if readReceiptStoreConfig starts
// substituting a non-zero default for a zero key instead of passing it through.
func TestMinRetainBlocksArchiveModeKeepsBothRetentionsOpen(t *testing.T) {
	configtest.Isolate(t)

	archive := srvconfig.DefaultConfig()
	params.SetAppConfigByMode(archive, params.NodeModeArchive)

	// Stated as its own assertion so a mode that stops keeping every block says so here, rather than
	// through the receipt-side comparison below.
	if archive.MinRetainBlocks != 0 {
		t.Fatalf("archive mode now sets min-retain-blocks to %d rather than 0. Tendermint prunes "+
			"blocks on that schedule and the receipt store takes the same key, so an archive node "+
			"keeps neither all blocks nor all receipts. If the mode changed deliberately, say what an "+
			"archive node is now expected to retain", archive.MinRetainBlocks)
	}

	receiptConfig, err := readReceiptStoreConfig(t.TempDir(), mapAppOpts{
		server.FlagMinRetainBlocks: archive.MinRetainBlocks,
	})
	if err != nil {
		t.Fatalf("readReceiptStoreConfig: %v", err)
	}
	if receiptConfig.KeepRecent != 0 {
		t.Errorf("archive's min-retain-blocks of %d gave the receipt store KeepRecent=%d, where 0 is "+
			"what leaves receipts unpruned. An archive node would now discard EVM receipts, which is "+
			"the same class of loss as PLT-955 through a different key",
			archive.MinRetainBlocks, receiptConfig.KeepRecent)
	}
}
