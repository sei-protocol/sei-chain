package configmanager

import (
	"cmp"
	"fmt"
	"log/slog"
	"os"
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
// Nothing here can stop a node starting, which is the one promise this manager makes.
func deliverDecodedSections(ctx *server.Context, bySection map[string]map[string]any,
	log *slog.Logger) {
	if len(bySection) == 0 {
		return
	}
	if ctx == nil || ctx.Config == nil {
		log.Error("no node configuration to deliver into; every one of these keys reads as it always has",
			"sections", len(bySection))
		return
	}

	// One section at a time. A decode is all or nothing for whatever it is handed, so a single value a
	// decoder refuses would otherwise cost every key in the file rather than the keys of the section it
	// appeared in. An operator who fixes one setting and mistypes another has to end up with the first
	// one applied.
	for _, name := range sortedKeys(bySection) {
		deliverOneSection(ctx, name, bySection[name], log)
	}
}

// deliverOneSection decodes one section's resolved values into the node's configuration.
//
// Decoded into a copy of that configuration first, and published by replacing it. A decoder gathers errors
// and keeps going, so a value it refuses partway leaves its target holding some of the new values and some
// of the old, with nothing to compare against and no way back. Rehearsing into a copy of the configuration
// the node already has, rather than into a fresh one, is what makes the rehearsal answer the same question:
// what a decoder writes can depend on what the target already holds, and only a copy holds the same things.
func deliverOneSection(ctx *server.Context, name string, values map[string]any, log *slog.Logger) {
	keys := sortedKeys(values)

	source := viper.New()
	for key, value := range values {
		source.Set(key, value)
	}

	// Refused before the decode, because a plain number where a length of time belongs decodes cleanly
	// and means nanoseconds. Nothing after this can tell that apart from a value somebody meant.
	if bad := refuseWhatDecodesToSomethingElse(ctx.Config, values); len(bad) > 0 {
		log.Error("a length of time in this section is written as a plain number, which reads as "+
			"nanoseconds; none of the section is applied and every one of its keys reads as it always has",
			"section", name, "written", strings.Join(bad, "; "))
		return
	}

	candidate, err := copyNodeConfig(ctx.Config)
	if err != nil {
		log.Error("cannot copy this node's configuration, so nothing can be delivered into it without "+
			"risking a half-written one; these keys read as they always have",
			"section", name, "keys", strings.Join(keys, ","), "err", err)
		return
	}
	before, readErr := describe(ctx.Config, keys)

	if err := source.Unmarshal(candidate); err != nil {
		log.Error("a written value in this section was refused, so none of the section is applied and "+
			"every one of its keys reads as it always has",
			"section", name, "keys", strings.Join(keys, ","), "err", err)
		return
	}

	*ctx.Config = *candidate
	after, afterErr := describe(ctx.Config, keys)
	if readErr != nil || afterErr != nil {
		// Reported rather than compared. Two unreadable sides look identical, so comparing them would
		// say every value matched, which is a statement about nothing produced by reading nothing.
		log.Error("this section was applied and what moved cannot be read, so nothing here says which "+
			"settings now differ from the node's own file", "section", name,
			"keys", strings.Join(keys, ","), "err", cmp.Or(readErr, afterErr))
		return
	}
	reportWhatMoved(name, keys, before, after, log)
}

// copyNodeConfig returns a configuration that holds what this one holds and shares nothing with it.
//
// A shallow copy would share every section, so a decode into the copy would write through to the original
// and a refused value would leave exactly the half-written configuration the copy exists to prevent. This
// copies the top level and every section under it.
//
// Written against the type rather than field by field, so a section added to it is copied without this
// function changing. A field this cannot copy is an error rather than a silent share.
func copyNodeConfig(from *tmcfg.Config) (*tmcfg.Config, error) {
	if from == nil {
		return nil, fmt.Errorf("no configuration to copy")
	}
	out := *from
	if err := detachSections(&out, from); err != nil {
		return nil, err
	}
	return &out, nil
}

// reportWhatMoved names every key whose value the delivery changed, and what it changed from.
//
// The node's own configuration file still says what it said, and every tool an operator reaches for reads
// that file: a patch command, a validator, an audit, somebody reading it over their shoulder at three in
// the morning. None of them describes the running node after this. This log line is the only place the two
// can be told apart, so it names the key, what the file gave it and what the node now runs.
//
// Keys that did not move are not reported. An operator who writes the value their file already held has
// changed nothing, and a line saying so buries the ones that did.
func reportWhatMoved(name string, keys []string, before, after map[string]string, log *slog.Logger) {
	var moved []string
	for _, key := range keys {
		if before[key] != after[key] {
			moved = append(moved, fmt.Sprintf("%s: %s -> %s", key, before[key], after[key]))
		}
	}
	if len(moved) == 0 {
		log.Info("this section's written values match what the node's own file already gave it",
			"section", name, "keys", len(keys))
		return
	}
	log.Info("this section's settings now differ from what the node's own configuration file says",
		"section", name, "changed", strings.Join(moved, "; "))
}

// logLevelKey is the one delivered setting the struct is not the end of.
const logLevelKey = "log-level"

// loggerOwnVariable is the environment variable the logger itself reads when it starts.
//
// Not the variable this key answers to in the resolution, which carries the binary's own prefix. Two names
// for one setting, and the older one is read before any of this runs.
const loggerOwnVariable = "SEI_LOG_LEVEL"

// applyResolvedLogLevel hands a resolved log level to the logger, which the struct alone does not reach.
//
// The boot's handler reads the level off the struct and sets it before any of this runs, so a value that
// only reaches the struct moves a field and changes no logging. A setting that appears to take and does not
// is what this key space exists to remove.
//
// Applied from the resolution rather than after the decode, and before it. Every failure this manager can
// have is a log line, and a refusal is reported at a level an operator may have raised the threshold above.
// Waiting for a successful decode would mean the one setting somebody changes in order to see a refusal is
// the setting a refusal suppresses.
//
// Which value arrives is already decided: the resolution ranks a flag over the environment over the file.
// A level that cannot be read is reported and skipped, and the node keeps the level it had.
func applyResolvedLogLevel(resolved registry.Resolved, typed map[string]string, log *slog.Logger) {
	supplied := false
	for _, key := range resolved.Overrides {
		if key == logLevelKey {
			supplied = true
		}
	}
	if !supplied {
		return
	}

	// The logger reads a variable of its own at start-up, under a name that is not the one this key
	// answers to, and the boot's own handler steps aside when it is set: a flag beats it and a file does
	// not. Applying here regardless would put the file above it, so an operator who exported a level and
	// then adopted this file would find the level they exported ignored. A typed flag still wins, which is
	// the order that was already there.
	if _, fromFlag := flagValues(typed)[logLevelKey]; !fromFlag {
		if os.Getenv(loggerOwnVariable) != "" {
			log.Info("a log level is set in the environment under the logger's own variable, which the "+
				"node already applied; the level this file supplies is not used",
				"variable", loggerOwnVariable, "ignored", resolved.Values[logLevelKey])
			return
		}
	}
	text, isText := resolved.Values[logLevelKey].(string)
	if !isText {
		log.Error("the resolved log level is not text; the node keeps the level it already had",
			"value", resolved.Values[logLevelKey])
		return
	}
	var level slog.Level
	if err := level.UnmarshalText([]byte(text)); err != nil {
		log.Error("the resolved log level cannot be read; the node keeps the level it already had",
			"level", text, "err", err)
		return
	}
	seilog.SetDefaultLevel(level, true)
	// That set every logger in the process, this one included, so the floor goes back on.
	keepOwnReportingVisible()
	log.Info("resolved log level applied", "level", text)
}
