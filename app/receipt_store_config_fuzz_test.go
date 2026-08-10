package app

import (
	"testing"

	"github.com/spf13/cast"

	"github.com/sei-protocol/sei-chain/sei-cosmos/server"
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

	f.Fuzz(func(t *testing.T, raw string) {
		homePath := t.TempDir()

		receiptConfig, err := readReceiptStoreConfig(homePath, mapAppOpts{
			server.FlagMinRetainBlocks: raw,
		})
		if err != nil {
			t.Fatalf("readReceiptStoreConfig(%q): %v", raw, err)
		}

		// The two casts as the two call sites apply them.
		blockRetention := cast.ToUint64(raw)
		receiptRetention := receiptConfig.KeepRecent

		if receiptRetention != cast.ToInt(raw) {
			t.Fatalf("the receipt store's KeepRecent is %d for min-retain-blocks=%q, where "+
				"app/receipt_store_config.go casts with cast.ToInt and would give %d. The receipt side "+
				"of the fan-out changed", receiptRetention, raw, cast.ToInt(raw))
		}

		// Where the casts agree, the fan-out is a plain coupling: one key, two retention policies,
		// same number.
		if uint64(receiptRetention) == blockRetention {
			return
		}

		// Where they disagree, the receipt side never becomes a positive retention window, and that is
		// the safety property. The two disagreement shapes reach it differently. A negative leaves
		// cast.ToInt holding the negative while cast.ToUint64 floors to zero. A value past int64 floors
		// cast.ToInt to zero while cast.ToUint64 keeps it. Either way the receipt store sees zero or
		// below and its KeepRecent>0 guard leaves receipts alone.
		//
		// The block side is deliberately not asserted to be zero, because past int64 it holds a huge
		// number instead, which is harmless for a different reason: it retains more blocks than a chain
		// reaches. Asserting zero there failed on exactly those two seeds, which is how the shape of
		// this invariant got established rather than assumed.
		if receiptRetention > 0 {
			t.Errorf("min-retain-blocks=%q casts to %d for receipt retention and %d for block "+
				"retention. The two disagree and the receipt side is now a positive window, so a value "+
				"the block side handled one way starts pruning EVM receipts on a schedule the operator "+
				"never set", raw, receiptRetention, blockRetention)
		}
	})
}

// TestMinRetainBlocksArchiveModeKeepsBothRetentionsOpen records that the archive path is aligned.
//
// PLT-955 is an archive node pruning state history because its mode's state-store settings are
// discarded. This is the neighbouring question, asked and answered so nobody re-investigates it:
// setArchiveTypeAppConfig sets min-retain-blocks to 0 (app/params/config.go), which is keep-all for
// Tendermint blocks, and 0 through the receipt cast leaves KeepRecent at 0, which is no pruning. So
// the fan-out does not give archive a second history-loss path.
func TestMinRetainBlocksArchiveModeKeepsBothRetentionsOpen(t *testing.T) {
	configtest.Isolate(t)

	receiptConfig, err := readReceiptStoreConfig(t.TempDir(), mapAppOpts{
		server.FlagMinRetainBlocks: 0,
	})
	if err != nil {
		t.Fatalf("readReceiptStoreConfig: %v", err)
	}
	if receiptConfig.KeepRecent != 0 {
		t.Errorf("archive's min-retain-blocks of 0 gave the receipt store KeepRecent=%d, where 0 is "+
			"what leaves receipts unpruned. An archive node would now discard EVM receipts, which is "+
			"the same class of loss as PLT-955 through a different key", receiptConfig.KeepRecent)
	}
}
