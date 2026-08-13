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

// declaredButNotRead names every declared key this census does not see read, and what covers it instead.
//
// The other direction of the check above. A key the registry declares and nothing reads is a setting an
// operator can write, a diagnostic reports on, and no node consults: worse than an undeclared key, because
// the file says it takes effect.
//
// The census records what the application's creation resolves. Some configuration is read later, by the
// server configuration reader at start time, which reaches a viper directly rather than through the object
// this wraps. A key read only there is invisible here and is not unread.
var declaredButNotRead = map[string]string{
	"compaction-interval": "read by the server configuration reader at start time rather than during " +
		"the application's creation, so this census cannot see it. The base section's own check writes a " +
		"value under it and confirms which setting changes, which is the proof this cannot supply",
}

// TestEveryDeclaredKeyIsReadBySomething closes the direction the read census cannot.
//
// Both directions matter and they fail differently. An unaccounted read is a value that resolves through
// the machinery the registry replaced. An unread declaration is a value that resolves through nothing at
// all, while a file and a diagnostic both present it as a setting.
func TestEveryDeclaredKeyIsReadBySomething(t *testing.T) {
	declared := registry.Keys()
	if len(declared) == 0 {
		t.Fatal("no keys are declared, so this check holds over nothing")
	}
	observed := map[string]bool{}
	for _, key := range recordAppCreatorReads(t).Keys() {
		observed[key] = true
	}

	var unread []string
	for _, key := range declared {
		if observed[key] {
			continue
		}
		if _, named := declaredButNotRead[key]; named {
			continue
		}
		unread = append(unread, key)
	}
	sort.Strings(unread)

	if len(unread) > 0 {
		t.Errorf("these keys are declared and nothing reads them:\n  %s\n\nEach is a setting an operator "+
			"can write and a diagnostic reports on, which no node consults. Remove the declaration, or "+
			"name it in declaredButNotRead with what proves the key is read elsewhere",
			strings.Join(unread, "\n  "))
	}
}

// TestEveryReasonStillDescribesADeclaredKey keeps that list from outliving its entries too.
func TestEveryReasonStillDescribesADeclaredKey(t *testing.T) {
	declared := map[string]bool{}
	for _, key := range registry.Keys() {
		declared[key] = true
	}
	for key := range declaredButNotRead {
		if !declared[key] {
			t.Errorf("declaredButNotRead names %q and no section declares it. Either the declaration was "+
				"removed, in which case drop the entry, or the key was renamed", key)
		}
	}
}
