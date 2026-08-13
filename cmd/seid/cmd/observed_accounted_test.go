package cmd

import (
	"sort"
	"strings"
	"testing"

	"github.com/spf13/pflag"
	"go.opentelemetry.io/otel/sdk/trace"

	"github.com/sei-protocol/sei-chain/config/registry"
	"github.com/sei-protocol/sei-chain/sei-cosmos/server"
)

// notDeclaredBecause names every key a node reads that no section declares, and why.
//
// The list exists because "the migration is finished" is otherwise unfalsifiable. A key read by the
// node and declared by nothing resolves through the machinery that answered it before the registry, and
// that fall-through is silent by design, so a missing declaration looks exactly like a complete one.
// Every key here is either something the registry must not own, or work not done yet said out loud.
var notDeclaredBecause = map[string]string{
	"home": "the directory every other path is resolved against, not a setting inside a file; a value " +
		"for it would have to be read before the file that carried it",

	"receipt-store.backend": "read only to refuse it, so an operator who used the wrong name is told to " +
		"use rs-backend instead. Declaring it would turn that message into a value the registry stores",

	"index-events": "a key at the root of app.toml with no section, and the second of the two root " +
		"keys no flag delivers. It carries a list rather than a scalar, which is a shape no declared " +
		"key has yet",

	"occ-enabled": "a key at the root of app.toml with no section, which the registry cannot yet " +
		"declare: a key's first segment is its section's name and there is no section here. It needs " +
		"no flag layer, since nothing registers a flag for it, which makes it the one root key that " +
		"tests prefix-free declaration on its own",
}

// TestEveryKeyTheNodeReadsIsAccountedFor is what makes the remaining work countable.
//
// Three ways a key is accounted for. It is declared, so the registry resolves it. Or a command flag
// delivers it, which the registry must not own until it can resolve that channel: installing a declared
// value writes into the source's override layer, above a bound flag, so a declared flag name would make
// the file beat the command line. Or it is named above with a reason.
//
// A key in none of those is the failure. It is a reader nobody decided about, and nothing else reports
// one, because an undeclared key reads exactly as it always has.
func TestEveryKeyTheNodeReadsIsAccountedFor(t *testing.T) {
	read := recordAppCreatorReads(t).Keys()
	if len(read) == 0 {
		t.Fatal("no keys were recorded, so this check holds for any registry")
	}

	declared := map[string]bool{}
	for _, key := range registry.Keys() {
		declared[key] = true
	}
	bound := boundStartFlags(t)

	var unaccounted []string
	for _, key := range read {
		if declared[key] || bound[key] {
			continue
		}
		if _, named := notDeclaredBecause[key]; named {
			continue
		}
		unaccounted = append(unaccounted, key)
	}
	sort.Strings(unaccounted)

	if len(unaccounted) > 0 {
		t.Errorf("these keys are read by the node and accounted for by nothing:\n  %s\n\nEach one "+
			"resolves through the machinery that answered it before the registry existed, which reports "+
			"nothing. Declare it, or name it in notDeclaredBecause with the reason it must not be "+
			"declared", strings.Join(unaccounted, "\n  "))
	}
}

// TestEveryReasonStillDescribesAKeyTheNodeReads keeps the list from outliving its entries.
//
// A name here for a key nothing reads is a reason nobody can check, and it makes the list read as
// though more is left than there is.
func TestEveryReasonStillDescribesAKeyTheNodeReads(t *testing.T) {
	read := map[string]bool{}
	for _, key := range recordAppCreatorReads(t).Keys() {
		read[key] = true
	}
	for key := range notDeclaredBecause {
		if !read[key] {
			t.Errorf("notDeclaredBecause names %q and the node does not read it. Either the reader was "+
				"removed, in which case drop the entry, or the census no longer reaches it", key)
		}
	}
}

// boundStartFlags returns the flag names the start command binds, local and inherited.
func boundStartFlags(t *testing.T) map[string]bool {
	t.Helper()
	cmd := server.StartCmd(nil, t.TempDir(), []trace.TracerProviderOption{})
	out := map[string]bool{}
	collect := func(f *pflag.Flag) { out[strings.ToLower(f.Name)] = true }
	cmd.Flags().VisitAll(collect)
	cmd.PersistentFlags().VisitAll(collect)
	cmd.InheritedFlags().VisitAll(collect)
	if len(out) == 0 {
		t.Fatal("the start command reported no flags, so every key would look unaccounted for")
	}
	return out
}
