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

// Tests for minimum-gas-prices, the one key in this tree whose accepted syntax is contradicted by
// its own documentation. One reader governs a node, and two artifacts show a separator it panics on.
//
// testutil/configtest/AGENTS.md holds the contradiction, why the two halves of a repair carry
// different risk, and the call-site gap this file cannot close.

// resolveMinGasPrices runs the expression root.go:296 runs, through the viper type production hands
// it, and reports whether it panicked rather than the option, because the panic is the behavior
// under test.
//
// Whether it panicked and what it said are returned separately for the same reason getterReads
// below keeps them apart: collapsed into one string, a boot and a panic carrying an empty message
// are the same answer, and the fuzz target's only test for "booted" is that string being empty.
func resolveMinGasPrices(raw string) (panicked bool, panicMessage string) {
	defer func() {
		if r := recover(); r != nil {
			panicked, panicMessage = true, fmt.Sprint(r)
		}
	}()
	appOpts := viper.New()
	appOpts.Set(server.FlagMinGasPrices, raw)
	_ = baseapp.SetMinGasPrices(cast.ToString(appOpts.Get(server.FlagMinGasPrices)))
	return false, ""
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
		panicked, panicMessage := resolveMinGasPrices(raw)

		// A semicolon is only reachable as a separator, so any value carrying one has to panic for
		// the disjointness this file records to hold.
		if strings.Contains(raw, ";") && !panicked {
			t.Errorf("minimum-gas-prices=%q carries a semicolon and booted anyway. ParseDecCoins now "+
				"accepts the separator the flag's help text and Config.GetMinGasPrices already use, so "+
				"the two syntaxes have stopped being disjoint. That is a fine end state and it changes "+
				"what a node accepts, so update this file in the PR that widens the parser", raw)
		}
		// Where the rejection happens, not just that it happens. Every rejection reachable from a
		// string arrives as an error ParseDecCoins returns and SetMinGasPrices wraps, so the wrap is
		// the marker that the refusal is still baseapp's.
		if panicked && !strings.Contains(panicMessage, "invalid minimum gas prices") {
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
	if panicked, _ := resolveMinGasPrices(documented); !panicked {
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
	if panicked, _ := resolveMinGasPrices(commaSeparated); panicked {
		t.Errorf("the live reader now rejects %q, which was the one multi-denomination spelling that "+
			"booted a node. If the parser changed, say what an operator should write instead",
			commaSeparated)
	}
	if panicked, _ := resolveMinGasPrices(semicolonSeparated); !panicked {
		t.Errorf("the live reader now accepts %q, so the documented separator boots and this "+
			"inversion is closed", semicolonSeparated)
	}
}
