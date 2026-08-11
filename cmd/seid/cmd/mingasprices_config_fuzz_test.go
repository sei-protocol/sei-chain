package cmd

import (
	"fmt"
	"strings"
	"testing"

	"github.com/spf13/cast"
	"github.com/spf13/viper"

	"github.com/sei-protocol/sei-chain/sei-cosmos/baseapp"
	"github.com/sei-protocol/sei-chain/sei-cosmos/server"
	serverconfig "github.com/sei-protocol/sei-chain/sei-cosmos/server/config"
)

// minimum-gas-prices is the one key in this tree whose accepted syntax is contradicted by its own
// documentation, and this file holds all three parties still.
//
// One reader governs a node. root.go:296 hands cast.ToString of the key to baseapp.SetMinGasPrices,
// which calls sdk.ParseDecCoins and panics on anything it cannot parse (options.go:24-28). That
// panic is the whole boot, and ParseDecCoins separates denominations with a comma.
//
// Two places document a semicolon instead. The start flag's own help text offers
// "0.01photino;0.0001stake" as its example (start.go:208), and Config.GetMinGasPrices splits the
// value on ";" (config.go:323). So the two syntaxes are disjoint rather than merely different: no
// multi-denomination value is accepted by both, and the spelling an operator is shown is the
// spelling that panics.
//
// The reason this has never been reported is that both agree on one denomination, which is the shape
// of the default and of nearly every deployment. It surfaces the first time an operator prices a
// second fee token by following the flag's example.
//
// GetMinGasPrices has no caller outside itself, so it is the documentation of an intent and not a
// second live resolution. That is what keeps this a sharp edge rather than a split: there is one
// answer at runtime, and two artifacts describing a different one.
//
// What this file does not pin is the call site. resolveMinGasPrices below runs the expression
// root.go:296 runs rather than driving root.go:296, because that argument is built inline inside
// newApp's app.New call and reaching it needs a node. So nothing here fails if that line changes its
// cast, changes its key, or stops handing the value to SetMinGasPrices, and the tests would keep
// describing a reader that had moved. The flag disappearing is caught, by the Lookup below; the call
// site changing is not. It is the same gap receipt_store_config_fuzz_test.go names for root.go:297,
// and it is stated here for the same reason: a reader who assumed otherwise would trust a pin that is
// not there.
//
// Recorded rather than repaired, and the two halves of a repair carry very different risk. Both are
// PLT-976 item 1.
//
// Correcting the help text and the getter is prose and dead code. It parses nothing differently, and
// what operators are told today is a value that takes the node down, so there is nothing to preserve
// on that side. That change is written and sits on the fix/min-gas-prices-separator branch, deferred
// on priority rather than on risk.
//
// Teaching ParseDecCoins the semicolon is the other kind of change. It widens the fee-floor grammar
// for every node, and once operators write semicolons narrowing back breaks them, so it needs a
// decision rather than a patch. Aligning the documentation down to the comma the parser already
// accepts is the cheaper direction and does not spend that door.

// resolveMinGasPrices runs the expression root.go:296 runs, through the viper type production hands
// it, and reports the panic rather than the option because the panic is the behavior under test.
func resolveMinGasPrices(raw string) (panicMessage string) {
	defer func() {
		if r := recover(); r != nil {
			panicMessage = fmt.Sprint(r)
		}
	}()
	appOpts := viper.New()
	appOpts.Set(server.FlagMinGasPrices, raw)
	_ = baseapp.SetMinGasPrices(cast.ToString(appOpts.Get(server.FlagMinGasPrices)))
	return ""
}

// FuzzMinGasPricesLiveReaderTakesCommasAndRejectsSemicolons pins which separator boots a node.
func FuzzMinGasPricesLiveReaderTakesCommasAndRejectsSemicolons(f *testing.F) {
	f.Add(serverconfig.DefaultMinGasPrices) // the shipped default, one denomination
	f.Add("")                               // absent, and accepted: ParseDecCoins reads it as no floor
	f.Add("0.01usei")
	f.Add("0.01usei,0.02uatom") // the separator the live reader accepts
	f.Add("0.01usei;0.02uatom") // the separator both documents show, and the live reader panics on
	f.Add("0.01photino;0.0001stake")
	f.Add("abc")
	f.Add("1")
	f.Add("usei")
	f.Add("-1usei")

	f.Fuzz(func(t *testing.T, raw string) {
		panicMessage := resolveMinGasPrices(raw)

		// A semicolon is only reachable as a separator, so any value carrying one has to panic for
		// the disjointness this file records to hold.
		if strings.Contains(raw, ";") && panicMessage == "" {
			t.Errorf("minimum-gas-prices=%q carries a semicolon and booted anyway. ParseDecCoins now "+
				"accepts the separator the flag's help text and Config.GetMinGasPrices already use, so "+
				"the two syntaxes have stopped being disjoint. That is a fine end state and it changes "+
				"what a node accepts, so update this file in the PR that widens the parser", raw)
		}
		// Where the rejection happens, not just that it happens. Every rejection reachable from a
		// string arrives as an error ParseDecCoins returns and SetMinGasPrices wraps, so the wrap is
		// the marker that the refusal is still baseapp's.
		if panicMessage != "" && !strings.Contains(panicMessage, "invalid minimum gas prices") {
			t.Errorf("minimum-gas-prices=%q panicked with %q rather than carrying baseapp's "+
				"\"invalid minimum gas prices\" wrap. Read it one of two ways, and both are worth a look "+
				"rather than a loosened assertion: the rejection moved to another reader, or this value "+
				"found a path that panics inside ParseDecCoins instead of returning an error for "+
				"SetMinGasPrices to wrap. Seeded runs cover neither; a fuzz run reaching here has found "+
				"something", raw, panicMessage)
		}
	})
}

// TestMinGasPricesFlagHelpShowsASeparatorTheLiveReaderPanicsOn reads the help text off the real
// command rather than a copy of it, so correcting either side lands in a diff.
func TestMinGasPricesFlagHelpShowsASeparatorTheLiveReaderPanicsOn(t *testing.T) {
	cmd := shippedStartCmd(t)
	flag := cmd.Flags().Lookup(server.FlagMinGasPrices)
	if flag == nil {
		t.Fatalf("start no longer registers %s, so root.go:296 reads a key with no flag behind it "+
			"and an absent value stops being the empty string this file records", server.FlagMinGasPrices)
	}

	// The example in the help text, held against the reader that would have to accept it.
	const documented = "0.01photino;0.0001stake"
	if !strings.Contains(flag.Usage, documented) {
		t.Fatalf("the %s help text no longer offers %q as its example, so the contradiction this file "+
			"records may be closed. Confirm the new example parses, then delete this test rather than "+
			"loosening it. Usage is now %q", server.FlagMinGasPrices, documented, flag.Usage)
	}
	if panicMessage := resolveMinGasPrices(documented); panicMessage == "" {
		t.Fatalf("the documented example %q now boots, so the help text and the reader agree and this "+
			"recording is stale", documented)
	}

	// The flag's default is what an operator gets by writing nothing, and it is not the package
	// default: an absent key resolves empty, which ParseDecCoins reads as no fee floor at all.
	if flag.DefValue != "" {
		t.Fatalf("the %s flag now defaults to %q rather than empty. An empty default is why a silent "+
			"app.toml yields a node with no fee floor, which is the behavior the [base_config] rows "+
			"record; a real default changes it", server.FlagMinGasPrices, flag.DefValue)
	}
	// And the declared default is not empty, which is what makes the flag's empty default a gap rather
	// than the same value stated twice. Asserted on the declared default directly, since the check
	// above has already established what the flag carries.
	if serverconfig.DefaultMinGasPrices == "" {
		t.Fatalf("DefaultMinGasPrices is now empty too, so the declared default and the resolved one " +
			"agree and there is no longer a gap here to record. That also means nothing in-code states " +
			"a fee floor, so say what a node is expected to accept")
	}
}

// TestMinGasPricesGetterAcceptsOnlyWhatTheLiveReaderRejects pins the inversion itself, which is the
// part a reader would otherwise have to discover twice.
func TestMinGasPricesGetterAcceptsOnlyWhatTheLiveReaderRejects(t *testing.T) {
	const (
		commaSeparated     = "0.01usei,0.02uatom"
		semicolonSeparated = "0.01usei;0.02uatom"
	)

	// The dead getter, reporting how many denominations it read and whether it panicked. Those are
	// kept apart because they are different facts: a getter that returned one coin and a getter that
	// panicked would otherwise be the same answer, and the panic is what this records.
	getterReads := func(raw string) (coins int, panicked bool) {
		defer func() {
			if r := recover(); r != nil {
				panicked = true
			}
		}()
		cfg := serverconfig.Config{BaseConfig: serverconfig.BaseConfig{MinGasPrices: raw}}
		return len(cfg.GetMinGasPrices()), false
	}

	if coins, panicked := getterReads(semicolonSeparated); panicked || coins != 2 {
		t.Errorf("GetMinGasPrices read %q as %d denominations (panicked=%v) rather than 2, so "+
			"config.go:323 has stopped splitting on the separator no other reader accepts",
			semicolonSeparated, coins, panicked)
	}
	if coins, panicked := getterReads(commaSeparated); !panicked {
		t.Errorf("GetMinGasPrices no longer panics on %q and read %d denominations instead. Its split "+
			"leaves the whole value in one token, so the panic is how it refuses the live reader's "+
			"syntax; if it now accepts that syntax the two readers have stopped being disjoint",
			commaSeparated, coins)
	}
	if resolveMinGasPrices(commaSeparated) != "" {
		t.Errorf("the live reader now rejects %q, which was the one multi-denomination spelling that "+
			"booted a node. If the parser changed, say what an operator should write instead",
			commaSeparated)
	}
	if resolveMinGasPrices(semicolonSeparated) == "" {
		t.Errorf("the live reader now accepts %q, so the documented separator boots and this "+
			"inversion is closed", semicolonSeparated)
	}
}
