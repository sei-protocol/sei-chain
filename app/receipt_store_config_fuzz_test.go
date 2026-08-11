package app

import (
	"testing"

	"github.com/spf13/cast"

	"github.com/sei-protocol/sei-chain/app/params"
	"github.com/sei-protocol/sei-chain/sei-cosmos/server"
	srvconfig "github.com/sei-protocol/sei-chain/sei-cosmos/server/config"
	"github.com/sei-protocol/sei-chain/testutil/configtest"
)

// FuzzMinRetainBlocksFansOutToTwoRetentionPolicies pins the one configuration key in this tree that
// two live consumers read.
//
// Every other key read twice has a dead second reader: sei-cosmos/server/config.GetConfig parses
// [state-commit] and [state-store] into a Config nobody hands to the store, so a disagreement there
// cannot reach a node. min-retain-blocks is different. Both of its readers run.
//
//	cmd/seid/cmd/root.go passes cast.ToUint64 of it to baseapp.SetMinRetainBlocks, which becomes the
//	Tendermint block-retention height.
//	app/receipt_store_config.go passes cast.ToInt of it to the receipt store's KeepRecent, which
//	becomes EVM receipt retention.
//
// So one number an operator sets for block retention silently also sets receipt retention, and the two
// go through different casts. That combination is what this holds still.
//
// What each half of that is worth is not the same, and the difference is the thing to carry off this
// file. The receipt half is pinned: this target calls readReceiptStoreConfig, so changing its cast
// fails here. The block half is not, and cannot be from a test: root.go builds that argument inline
// inside newApp's app.New call, which needs a node. So the block column of the table below is a
// recorded literal, and nothing in this suite fails if root.go:297 changes its cast. That gap is
// stated rather than papered over, because a reader who assumed otherwise would trust a pin that is
// not there.
//
// For every value an operator would sensibly write the two casts agree, and the fan-out is then just a
// coupling. Where they disagree, they disagree in value and agree in effect, and only because both
// out-of-range paths land on keep-everything: the receipt store applies a TTL only when KeepRecent is
// above zero (litt_receipt_store.go:138), and baseapp disables pruning entirely when minRetainBlocks
// is zero (abci.go:758). Both guards are load-bearing for that agreement, and neither names this key.
//
// Recorded rather than repaired. Making the two casts one would change what a node retains for any
// operator currently relying on the out-of-range behaviour, which is the kind of change this suite
// pins instead of making.
func FuzzMinRetainBlocksFansOutToTwoRetentionPolicies(f *testing.F) {
	// Values an operator writes, then the two shapes where the casts part company.
	f.Add("0")
	f.Add("100000")
	f.Add("200000")
	f.Add("-5")                   // ToInt keeps it, ToUint64 floors to 0
	f.Add("9223372036854775808")  // past int64, so ToInt floors to 0
	f.Add("18446744073709551615") // past int64, so ToInt floors to 0
	f.Add("not-a-number")         // both floor to 0

	// One directory for the whole target rather than one per iteration. It is not decoration: the
	// reader resolves DBDirectory through GetReceiptStorePath, which returns a legacy path when
	// <home>/data/receipt.db exists (path.go:69-75), so a home that is guaranteed empty is what keeps
	// that branch off ambient filesystem state. A literal would read whatever the machine has.
	homePath := f.TempDir()

	f.Fuzz(func(t *testing.T, raw string) {
		receiptConfig, err := readReceiptStoreConfig(homePath, mapAppOpts{
			server.FlagMinRetainBlocks: raw,
		})
		if err != nil {
			t.Fatalf("readReceiptStoreConfig(%q): %v", raw, err)
		}

		// The receipt half, against the cast its reader applies. Both sides go through cast.ToInt, so
		// what this catches is the wiring: a changed cast, or a reader that stops reading this key. It
		// cannot catch cast.ToInt itself resolving a value differently, and it is not asked to. The
		// value-level pin is the literal table below, where the receipt column is a number. What
		// fuzzing adds over that table is the wiring check across inputs no table would list.
		if receiptConfig.KeepRecent != cast.ToInt(raw) {
			t.Fatalf("the receipt store's KeepRecent is %d for min-retain-blocks=%q, where "+
				"app/receipt_store_config.go casts with cast.ToInt and would give %d. The receipt side "+
				"of the fan-out changed", receiptConfig.KeepRecent, raw, cast.ToInt(raw))
		}
	})
}

// minRetainBlocksFanOut is what one operator value becomes on each side.
//
// Both columns are literals. The receipt column is checked against the reader, so it is a prediction.
// The block column is a recording of what cast.ToUint64 does, held against a number rather than
// against a second call to the same function, because an assertion comparing a call to itself passes
// for any reader and would say nothing.
var minRetainBlocksFanOut = []struct {
	raw     string
	receipt int    // app/receipt_store_config.go:27, through cast.ToInt
	block   uint64 // cmd/seid/cmd/root.go:297, through cast.ToUint64
}{
	{"0", 0, 0},                // keep everything, both sides
	{"100000", 100000, 100000}, // the ordinary case: one value, two policies
	{"-5", -5, 0},              // ToInt keeps the negative, ToUint64 floors
	{"9223372036854775808", 0, 9223372036854775808},   // past int64: ToInt floors, ToUint64 keeps
	{"18446744073709551615", 0, 18446744073709551615}, // the uint64 ceiling, same shape
	{"not-a-number", 0, 0},                            // both floor, so both keep everything
}

// TestMinRetainBlocksFanOutNeverPrunesReceiptsWhereTheCastsDisagree holds the safety property that
// makes the fan-out survivable.
//
// Where the two casts land on the same number the fan-out is a plain coupling. Where they disagree,
// the receipt side must not be a positive retention window, because a positive KeepRecent is the one
// state that starts expiring receipts (litt_receipt_store.go:138). A row that broke that would mean a
// value an operator set for block retention silently pruning EVM receipts.
func TestMinRetainBlocksFanOutNeverPrunesReceiptsWhereTheCastsDisagree(t *testing.T) {
	for _, row := range minRetainBlocksFanOut {
		t.Run(row.raw, func(t *testing.T) {
			receiptConfig, err := readReceiptStoreConfig(t.TempDir(), mapAppOpts{
				server.FlagMinRetainBlocks: row.raw,
			})
			if err != nil {
				t.Fatalf("readReceiptStoreConfig(%q): %v", row.raw, err)
			}

			// The prediction, against the reader.
			if receiptConfig.KeepRecent != row.receipt {
				t.Errorf("min-retain-blocks=%q resolves the receipt store's KeepRecent to %d, recorded "+
					"as %d", row.raw, receiptConfig.KeepRecent, row.receipt)
			}
			// The recording, against the cast the block side applies.
			if got := cast.ToUint64(row.raw); got != row.block {
				t.Errorf("min-retain-blocks=%q casts to %d through cast.ToUint64, recorded as %d. The "+
					"block column is a recording of the cast rather than a pin on root.go:297, so "+
					"update it and say what a node now retains", row.raw, got, row.block)
			}

			// Same number on both sides means the fan-out is a plain coupling and there is nothing to
			// check. The sign test is part of that question rather than belt-and-braces: converting a
			// negative receipt value to uint64 wraps it to the top of the range, so a row pairing a
			// negative with a large block value would read as agreement and skip the check below.
			if row.receipt >= 0 && uint64(row.receipt) == row.block {
				return
			}
			if row.receipt > 0 {
				t.Errorf("min-retain-blocks=%q gives %d for receipt retention and %d for block "+
					"retention. The two disagree and the receipt side is a positive window, so a value "+
					"the block side handled one way starts pruning EVM receipts on a schedule the "+
					"operator never set", row.raw, row.receipt, row.block)
			}
		})
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
