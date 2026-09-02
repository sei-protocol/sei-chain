package configmanager

import (
	"cmp"
	"fmt"
	"log/slog"
	"os"
	"reflect"
	"strings"

	"github.com/spf13/viper"

	"github.com/sei-protocol/seilog"

	"github.com/sei-protocol/sei-chain/config/registry"
	"github.com/sei-protocol/sei-chain/sei-cosmos/server"
	tmcfg "github.com/sei-protocol/sei-chain/sei-tendermint/config"
)

// deliverDecodedSections puts the resolved values of the decoded sections into the node's own
// configuration.
//
// Putting a value into the source a node reads is the whole delivery for a section whose reader looks its
// keys up one at a time. It is no delivery at all for the sections this covers, which the boot's handler
// reads once into a struct before this runs. Those values are decoded into that struct instead, which is
// the same mechanism the handler used and therefore the same casts, the same tags and the same hooks.
//
// Nothing here can stop a node starting.
func deliverDecodedSections(ctx *server.Context, bySection map[string]map[string]any,
	log *slog.Logger, said func(string, ...any)) {
	if len(bySection) == 0 {
		return
	}
	if ctx == nil || ctx.Config == nil {
		log.Error("no node configuration to deliver into; every one of these keys reads as it always has",
			"sections", len(bySection))
		return
	}

	// Each section names why its values need decoding rather than installing, and the reason names the
	// struct they are decoded into. Reported here because this is the only place that claim is acted on:
	// a section whose reason no longer describes what reads it is delivered the wrong way, and there is
	// nothing else that would show it.
	reasons := registry.DecodedSections()

	// One section at a time. A decode is all or nothing for what it is handed. Handed the whole file, a
	// single refused value would cost every key in it. An operator who fixes one setting and mistypes
	// another has to end up with the first one applied.
	for _, name := range sortedKeys(bySection) {
		log.Debug("delivering a section by decoding it rather than by a lookup",
			"section", name, "why", reasons[name], "keys", len(bySection[name]))
		deliverOneSection(ctx, name, bySection[name], log, said)
	}
}

// deliverOneSection decodes one section's resolved values into the node's configuration.
//
// Decoded into a copy of that configuration first, and published into the live one only once the whole
// section has decoded. A decoder gathers errors and keeps going. A value it refuses partway therefore
// leaves its target holding some new values and some old ones, with nothing to compare against and no way
// back. Rehearsing into a copy of the configuration the node already has, rather than into a fresh one, is
// what makes the rehearsal answer the same question: what a decoder writes can depend on what the target
// already holds, and only a copy holds the same things.
func deliverOneSection(ctx *server.Context, name string, values map[string]any, log *slog.Logger,
	said func(string, ...any)) {
	keys := sortedKeys(values)

	source := viper.New()
	for key, value := range values {
		source.Set(key, value)
	}

	// Refused before the decode, because a plain number where a length of time belongs decodes cleanly
	// and means nanoseconds. Nothing after this can tell that apart from a value somebody meant.
	fields := keyFieldTypes(reflect.TypeOf(*ctx.Config), "")
	if bad := whatDecodesToSomethingElse(fields, values); len(bad) > 0 {
		// Each message says what is wrong with the value it names, and there is more than one thing that
		// can be. Stating one of them here would describe the others wrongly.
		log.Error("a written value in this section decodes to something other than what it says; none of "+
			"the section is applied and every one of its keys reads as it always has",
			"section", name, "written", strings.Join(problemsInOrder(bad), "; "))
		return
	}

	candidate, err := copyNodeConfig(ctx.Config)
	if err != nil {
		log.Error("cannot copy this node's configuration, so nothing can be delivered into it without "+
			"risking a half-written one; these keys read as they always have",
			"section", name, "keys", strings.Join(keys, ","), "err", err)
		return
	}
	before, unreadBefore, readErr := describe(ctx.Config, keys)

	if err := source.Unmarshal(candidate); err != nil {
		log.Error("a written value in this section was refused, so none of the section is applied and "+
			"every one of its keys reads as it always has",
			"section", name, "keys", strings.Join(keys, ","), "err", err)
		return
	}

	// The node's own rules, on the copy, before anything is published. A value can decode cleanly, mean
	// what it says, and still be one the node refuses: a negative transaction-size ceiling decodes to
	// minus one and then every transaction measures larger than it, so the node accepts none. Around
	// thirty such checks live here and nothing above this can see any of them.
	//
	// Cheap here and nowhere else, because this is the one place a copy exists to test. It also inherits
	// the section-scoped refusal, so a bad value costs its own section and not the whole file.
	// Held to this section's own rules, not the whole configuration's. The whole set stops at the first
	// failing section, so a node already failing on one section makes every other section's failure
	// unattributable, and a value written here lands under a line saying the node was already broken.
	if err := whatTheSectionsOwnRulesSay(candidate, sectionPrefix(keys)); err != nil {
		log.Error("this section's written values break its own rules, so none of the section is applied "+
			"and every one of its keys reads as it always has",
			"section", name, "keys", strings.Join(keys, ","), "err", err)
		return
	}

	if err := publishNodeConfig(ctx.Config, candidate); err != nil {
		log.Error("cannot publish this node's configuration, so these keys read as they always have",
			"section", name, "keys", strings.Join(keys, ","), "err", err)
		return
	}
	after, unreadAfter, afterErr := describe(ctx.Config, keys)
	if readErr != nil || afterErr != nil {
		// Reported rather than compared. Two unreadable sides look identical, so comparing them would
		// say every value matched, which is a statement about nothing produced by reading nothing.
		log.Error("this section was applied and what moved cannot be read, so nothing here says which "+
			"settings now differ from the node's own file", "section", name,
			"keys", strings.Join(keys, ","), "err", cmp.Or(readErr, afterErr))
		return
	}
	// The same hazard one key at a time. A key absent from both answers compares equal, so it would be
	// reported as a setting that did not move, which is what a key an operator wrote and got looks like.
	unread := asSet(append(unreadBefore, unreadAfter...))
	if len(unread) > 0 {
		shown, omitted := capLoggedItems(sortedKeys(unread))
		log.Error("this section was applied and some of its keys cannot be read back, so nothing here "+
			"says whether those moved", "section", name, "count", len(shown)+omitted,
			"keys", strings.Join(shown, ","), "omitted", omitted)
	}

	reportWhatMoved(name, whatBothSidesCouldBeReadFor(keys, unread), before, after, log, said)
}

// copyNodeConfig returns a configuration that holds what this one holds and shares nothing with it.
//
// A shallow copy shares every section. A decode into it would write through to the original, and a refused
// value would leave the half-written configuration the copy exists to prevent. This copies the top level
// and every section under it.
//
// Written against the type rather than field by field, so a section added to it is copied without this
// function changing. Every exported reference gets one of its own and one that cannot is an error rather
// than a silent share. An unexported field is copied by value and shared, which is safe only because the
// decoder this protects against cannot write to one.
func copyNodeConfig(from *tmcfg.Config) (*tmcfg.Config, error) {
	if from == nil {
		return nil, fmt.Errorf("no configuration to copy")
	}
	out := *from
	if err := detachReferences(&out); err != nil {
		return nil, err
	}
	return &out, nil
}

// whatBothSidesCouldBeReadFor drops the keys neither side could be read for.
//
// A key missing from both answers compares equal. Left in, it makes the report say the section matches the
// node's own file, which is what the line above it withholds. Dropped here rather than inside the
// comparison: the comparison says what moved, and this says which keys it can speak for.
func whatBothSidesCouldBeReadFor(keys []string, unread map[string]struct{}) []string {
	out := make([]string, 0, len(keys))
	for _, key := range keys {
		if _, missing := unread[key]; !missing {
			out = append(out, key)
		}
	}
	return out
}

// reportWhatMoved names every key whose value the delivery changed, and what it changed from.
//
// The node's own configuration file still says what it said, and every tool an operator reaches for reads
// that file: a patch command, a validator, an audit, somebody reading it over their shoulder at three in
// the morning. None of them describes the running node after this. This log line is the only place the two
// can be told apart, so it names the key, what the file gave it and what the node now runs.
//
// A key that did not move is not named. An operator who writes the value their file already held has
// changed nothing, and naming it buries the keys that did move. The section still reports that it matched,
// at a level a routine boot does not raise.
//
// The rendered list is capped for the reason every other one here is: the count is what an operator alerts
// on, and one line per key of a large section buries whichever of them mattered.
func reportWhatMoved(name string, keys []string, before, after map[string]string, log *slog.Logger,
	said func(string, ...any)) {
	var moved []string
	for _, key := range keys {
		if before[key] != after[key] {
			moved = append(moved, fmt.Sprintf("%s: %s -> %s", key, before[key], after[key]))
		}
	}
	if len(moved) == 0 {
		log.Debug("this section's written values match what the node's own file already gave it",
			"section", name, "keys", len(keys))
		return
	}
	shown, omitted := capLoggedItems(moved)
	said("this section's settings now differ from what the node's own configuration file says",
		"section", name, "count", len(moved), "changed", strings.Join(shown, "; "), "omitted", omitted)
}

// logLevelKey is the one delivered setting the struct is not the end of.
const logLevelKey = "log-level"

// loggerOwnVariable is the environment variable the logger itself reads when it starts.
//
// Not the variable this key answers to in the resolution, which carries the binary's own prefix. Two names
// for one setting, and the older one is read before any of this runs.
const loggerOwnVariable = "SEI_LOG_LEVEL"

// asSet collapses repeats, so a key unread on both sides is named once.
func asSet(keys []string) map[string]struct{} {
	out := make(map[string]struct{}, len(keys))
	for _, key := range keys {
		out[key] = struct{}{}
	}
	return out
}

// applyTheLevelTheStructNowHolds hands the logger the level the node's configuration holds, which the
// struct alone does not reach.
//
// The boot's handler reads the level off the struct and sets it before any of this runs. A value that only
// reaches the struct therefore moves a field and changes no logging. This key space exists to remove a
// setting that appears to take and does not.
//
// Read from the struct rather than from the resolution, and after the deliveries, so the two always agree.
// The section carrying this key is refused as a whole when any of its values is wrong, and the resolution
// still holds a level for it, so applying that would move the process while the struct kept what it had.
//
// A level that cannot be read is reported and skipped, and the node keeps the level it had.
func applyTheLevelTheStructNowHolds(ctx *server.Context, typed map[string]string, log *slog.Logger) {
	if ctx == nil || ctx.Config == nil {
		return
	}
	text := ctx.Config.LogLevel

	// The logger reads its own variable at start-up, under a different name from this key. The boot's own
	// handler steps aside when that variable is set: a flag beats it, a file does not. Applying here
	// regardless would put the file above it. An operator who exported a level and then adopted this file
	// would find the exported level ignored. A typed flag still wins, which is the order that was already
	// there.
	if _, fromFlag := flagValues(typed)[logLevelKey]; !fromFlag {
		if os.Getenv(loggerOwnVariable) != "" {
			log.Info("a log level is set in the environment under the logger's own variable, which the "+
				"node already applied; the level this node's configuration holds is not used",
				"variable", loggerOwnVariable, "ignored", text)
			return
		}
	}
	var level slog.Level
	if err := level.UnmarshalText([]byte(text)); err != nil {
		log.Error("the log level this node's configuration holds cannot be read; the node keeps the level "+
			"it already had", "level", text, "err", err)
		return
	}
	seilog.SetDefaultLevel(level, true)
	// That set every logger in the process, this one included, so the floor goes back on.
	keepOwnReportingVisible()
	log.Info("log level applied", "level", text)
}

// whatTheSectionsOwnRulesSay reports what this section's own rules say about a candidate.
//
// Each section type states its own, and the whole configuration's rules are those called in turn, stopping
// at the first failure. So asking the whole set cannot say which section a failure belongs to: on a node
// already failing elsewhere every answer is the other section's, and on a node that is not, a failure
// anywhere reads as this section's.
//
// Returns nil for a section whose type states no rules of its own.
func whatTheSectionsOwnRulesSay(candidate *tmcfg.Config, prefix string) error {
	holder := reflect.ValueOf(candidate).Elem()
	for i := 0; i < holder.NumField(); i++ {
		field := holder.Type().Field(i)
		tag := field.Tag.Get("mapstructure")
		name := strings.Split(tag, ",")[0]
		squashed := strings.Contains(tag, ",squash")

		// A section with no prefix keeps its keys at the root of the file, which is the group the node's
		// own type squashes in, so its rules are that group's.
		if (prefix == "" && squashed) || (prefix != "" && name == prefix) {
			value := holder.Field(i)
			if value.Kind() == reflect.Pointer && value.IsNil() {
				return nil
			}
			if rules, states := value.Interface().(interface{ ValidateBasic() error }); states {
				return rules.ValidateBasic()
			}
			return nil
		}
	}
	return nil
}

// sectionPrefix returns the segment every key of a section shares, empty for a section whose keys sit at
// the root of the file.
func sectionPrefix(keys []string) string {
	if len(keys) == 0 {
		return ""
	}
	if at := strings.IndexByte(keys[0], '.'); at >= 0 {
		return keys[0][:at]
	}
	return ""
}
