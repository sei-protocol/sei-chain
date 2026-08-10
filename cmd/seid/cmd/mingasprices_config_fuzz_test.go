package cmd

import (
	"fmt"
	"strings"
	"testing"

	"github.com/spf13/cast"
	"github.com/spf13/viper"
	"go.opentelemetry.io/otel/sdk/trace"

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
// Recorded rather than repaired. Teaching ParseDecCoins the semicolon widens what a node accepts,
// and correcting the help text changes what operators are told a working value looks like; both are
// decisions for the reader that replaces this one.

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
		if panicMessage != "" && !strings.Contains(panicMessage, "invalid minimum gas prices") {
			t.Errorf("minimum-gas-prices=%q panicked with %q rather than baseapp's "+
				"\"invalid minimum gas prices\". A different panic means the rejection moved to another "+
				"reader, and where a bad fee floor is refused is what this file tracks", raw, panicMessage)
		}
	})
}

// TestMinGasPricesFlagHelpShowsASeparatorTheLiveReaderPanicsOn reads the help text off the real
// command rather than a copy of it, so correcting either side lands in a diff.
func TestMinGasPricesFlagHelpShowsASeparatorTheLiveReaderPanicsOn(t *testing.T) {
	cmd := server.StartCmd(nil, "/foobar", []trace.TracerProviderOption{})
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
	if flag.DefValue == serverconfig.DefaultMinGasPrices {
		t.Fatalf("the flag default and DefaultMinGasPrices are both %q, so the gap between the "+
			"declared default and the resolved one has closed", flag.DefValue)
	}
}

// TestMinGasPricesGetterAcceptsOnlyWhatTheLiveReaderRejects pins the inversion itself, which is the
// part a reader would otherwise have to discover twice.
func TestMinGasPricesGetterAcceptsOnlyWhatTheLiveReaderRejects(t *testing.T) {
	const (
		commaSeparated     = "0.01usei,0.02uatom"
		semicolonSeparated = "0.01usei;0.02uatom"
	)

	// The dead getter, which panics on its own account when handed the live reader's syntax.
	getterTakes := func(raw string) (ok bool) {
		defer func() {
			if r := recover(); r != nil {
				ok = false
			}
		}()
		cfg := serverconfig.Config{BaseConfig: serverconfig.BaseConfig{MinGasPrices: raw}}
		return len(cfg.GetMinGasPrices()) == 2
	}

	if !getterTakes(semicolonSeparated) {
		t.Errorf("GetMinGasPrices no longer reads %q as two denominations, so config.go:323 has "+
			"stopped splitting on the separator the flag documents", semicolonSeparated)
	}
	if getterTakes(commaSeparated) {
		t.Errorf("GetMinGasPrices now also reads %q, so the two readers have stopped being disjoint "+
			"and a multi-denomination value exists that satisfies both", commaSeparated)
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
