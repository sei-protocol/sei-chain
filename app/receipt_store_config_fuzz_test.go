package app

import (
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/spf13/cast"

	"github.com/sei-protocol/sei-chain/app/params"
	"github.com/sei-protocol/sei-chain/sei-cosmos/server"
	srvconfig "github.com/sei-protocol/sei-chain/sei-cosmos/server/config"
	"github.com/sei-protocol/sei-chain/testutil/configtest"
)

// This file pins the one configuration key in this tree that two live consumers read.
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
// file. The receipt half is pinned against the reader: the table drives readReceiptStoreConfig, so
// changing its cast fails here. The block half is not, and cannot be from a test: root.go builds that
// argument inline inside newApp's app.New call, which needs a node. So the block column is a recorded
// literal, and nothing in this suite fails if root.go:297 changes its cast. That gap is stated rather
// than papered over, because a reader who assumed otherwise would trust a pin that is not there.
//
// For every value an operator would sensibly write the two casts agree, and the fan-out is then just a
// coupling. Where they disagree, receipts still survive, but not by one mechanism and not always by
// the one worth relying on. There are two routes and the difference between them matters.
//
// The first is the guard. Where cast.ToInt lands on zero or below, the receipt store never enters its
// TTL branch, because that branch is entered only when KeepRecent is above zero
// (litt_receipt_store.go:138). Every disagreement reachable as a string takes this route: a negative
// is kept by ToInt and floored by ToUint64, and a decimal past int64 is floored by ToInt and kept by
// ToUint64.
//
// The second is an overflow, and it is the one to be uneasy about. A value that reaches Get as a
// float64 at or above 2^63 makes cast.ToInt saturate to MaxInt64 rather than floor, so KeepRecent is
// positive and the TTL branch is entered. Receipts survive anyway because MaxInt64 multiplied by
// littTTLPerBlock overflows to a negative Duration, and a TTL at or below zero is treated as no
// expiry (gc_manager.go:273). That is an accident of the multiplier's value, not a guard, and a
// different multiplier could wrap the product to a small positive TTL that prunes on a schedule
// nobody chose. This file records the accident rather than resting on it.
//
// Recorded rather than repaired. Making the two casts one would change what a node retains for any
// operator currently relying on the out-of-range behaviour, which is the kind of change this suite
// pins instead of making.
// minRetainBlocksFanOut is what one operator value becomes on each side.
//
// Both columns are literals. The receipt column is checked against the reader, so it is a prediction.
// The block column is a recording of what cast.ToUint64 does, held against a number rather than
// against a second call to the same function, because an assertion comparing a call to itself passes
// for any reader and would say nothing.
//
// raw is an any because the type is part of the input, not a detail of how this table is written. Both
// readers cast whatever appOpts.Get returns, and which Go type that is depends on the layer that set
// the key: the env layer and a quoted app.toml entry hand them a string, an unquoted integer decodes
// to int64, and an unquoted float literal decodes to float64. A table of strings alone cannot express
// the float case, and the float case is the only one where the two casts disagree with the receipt
// side positive.
// littTTLPerBlockForTest mirrors sei-db's per-block TTL multiplier (litt_receipt_store.go). It is
// duplicated rather than imported because it is unexported there; if the two drift, the saturation row
// below stops describing what a node does and the assertion says so.
const littTTLPerBlockForTest = 2 * time.Second

var minRetainBlocksFanOut = []struct {
	raw     any
	receipt int    // app/receipt_store_config.go:27, through cast.ToInt
	block   uint64 // cmd/seid/cmd/root.go:297, through cast.ToUint64
}{
	{"0", 0, 0},                // keep everything, both sides
	{"100000", 100000, 100000}, // the ordinary case: one value, two policies
	{"200000", 200000, 200000}, // a second ordinary value, same class
	{"-5", -5, 0},              // ToInt keeps the negative, ToUint64 floors
	{"9223372036854775808", 0, 9223372036854775808},   // past int64: ToInt floors, ToUint64 keeps
	{"18446744073709551615", 0, 18446744073709551615}, // the uint64 ceiling, same shape
	{"not-a-number", 0, 0},                            // both floor, so both keep everything
	// A leading sign is where the two string casts could most plausibly part company, because ParseInt
	// takes a sign prefix. cast strips a leading + before ParseUint sees it, so they agree, and the row
	// exists so that stops being something a reader takes on trust.
	{"+5", 5, 5},
	// Unquoted in app.toml, so the decode hands both readers the operator's own type.
	{int64(100000), 100000, 100000},
	// The one shape where the casts disagree with the receipt side positive. Reachable as
	// min-retain-blocks = 1e19, which decodes to float64: ToInt saturates to MaxInt64 where the string
	// path would have floored to zero, and ToUint64 keeps the magnitude.
	{float64(1e19), math.MaxInt64, 10000000000000000000},
	// Just above 2^63, so the same saturation from a value an operator could plausibly mistype.
	{float64(9.3e18), math.MaxInt64, 9300000000000000000},
}

// TestMinRetainBlocksFanOutNeverPrunesReceiptsWhereTheCastsDisagree holds the safety property that
// makes the fan-out survivable.
//
// Where the two casts land on the same number the fan-out is a plain coupling. Where they disagree,
// the receipt side must not be a positive retention window, because a positive KeepRecent is the one
// state that starts expiring receipts (litt_receipt_store.go:138). A row that broke that would mean a
// value an operator set for block retention silently pruning EVM receipts.
//
// One class of row is exempt from that and the exemption is the finding, not a loophole. A float at or
// above 2^63 makes cast.ToInt saturate to MaxInt64, so the receipt side is positive and the TTL branch
// is entered. Receipts survive because the TTL multiply overflows negative and a non-positive TTL is
// treated as no expiry, which is an accident rather than a guard. Those rows are held to saturating
// exactly, since a value that stopped saturating and stayed positive but small would prune.
func TestMinRetainBlocksFanOutNeverPrunesReceiptsWhereTheCastsDisagree(t *testing.T) {
	for _, row := range minRetainBlocksFanOut {
		t.Run(fmt.Sprintf("%v(%T)", row.raw, row.raw), func(t *testing.T) {
			receiptConfig, err := readReceiptStoreConfig(t.TempDir(), mapAppOpts{
				server.FlagMinRetainBlocks: row.raw,
			})
			if err != nil {
				t.Fatalf("readReceiptStoreConfig(%v): %v", row.raw, err)
			}

			// The prediction, against the reader.
			if receiptConfig.KeepRecent != row.receipt {
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

			// Same number on both sides means the fan-out is a plain coupling and there is nothing to
			// check. The sign test is part of that question rather than belt-and-braces: converting a
			// negative receipt value to uint64 wraps it to the top of the range, so a row pairing a
			// negative with a large block value would read as agreement and skip the check below.
			if row.receipt >= 0 && uint64(row.receipt) == row.block {
				return
			}
			// Saturation is the one positive receipt window that survives, and only because the TTL
			// multiply overflows past it. Held to the exact boundary: anything positive and smaller
			// would produce a real TTL and prune.
			if row.receipt == math.MaxInt64 {
				if ttl := time.Duration(row.receipt) * littTTLPerBlockForTest; ttl > 0 {
					t.Errorf("min-retain-blocks=%v saturates KeepRecent to MaxInt64 and the TTL multiply "+
						"now yields %v, which is positive. Receipts were surviving this row only because "+
						"that product overflowed to a non-positive Duration that gc_manager treats as no "+
						"expiry. With a positive TTL an operator's block-retention value starts expiring "+
						"receipts", row.raw, ttl)
				}
				return
			}
			if row.receipt > 0 {
				t.Errorf("min-retain-blocks=%v gives %d for receipt retention and %d for block "+
					"retention. The two disagree and the receipt side is a positive window that is not "+
					"the saturation case, so a value the block side handled one way starts pruning EVM "+
					"receipts on a schedule the operator never set", row.raw, row.receipt, row.block)
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
