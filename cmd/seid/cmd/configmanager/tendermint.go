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
	"github.com/sei-protocol/sei-chain/sei-cosmos/client/flags"
	"github.com/sei-protocol/sei-chain/sei-cosmos/server"
	tmcfg "github.com/sei-protocol/sei-chain/sei-tendermint/config"
)

// deliverDecodedSections decodes each decoded section's resolved values into the node's configuration
// struct, one section at a time, so a refused value costs only its own section.
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

	reasons := registry.DecodedSections()
	for _, name := range sortedKeys(bySection) {
		log.Debug("delivering a section by decoding it rather than by a lookup",
			"section", name, "why", reasons[name], "keys", len(bySection[name]))
		deliverOneSection(ctx, name, bySection[name], log, said)
	}
}

// deliverOneSection decodes one section's resolved values into a copy of the node's configuration, checks
// the section's own rules, and publishes the copy only if both succeed, since a failed decode leaves its
// target half-written.
func deliverOneSection(ctx *server.Context, name string, values map[string]any, log *slog.Logger,
	said func(string, ...any)) {
	keys := sortedKeys(values)

	source := viper.New()
	for key, value := range values {
		source.Set(key, value)
	}

	// Before the decode, which accepts these silently.
	fields := keyFieldTypes(reflect.TypeOf(*ctx.Config), "")
	if bad := whatDecodesToSomethingElse(fields, values); len(bad) > 0 {
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

	// Values can decode cleanly and still break the node's own rules.
	if err := whatTheSectionsOwnRulesSay(candidate, keys); err != nil {
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
		// Two unreadable sides would compare equal, so do not compare.
		log.Error("this section was applied and what moved cannot be read, so nothing here says which "+
			"settings now differ from the node's own file", "section", name,
			"keys", strings.Join(keys, ","), "err", cmp.Or(readErr, afterErr))
		return
	}
	// Likewise per key.
	unread := asSet(append(unreadBefore, unreadAfter...))
	if len(unread) > 0 {
		shown, omitted := capLoggedItems(sortedKeys(unread))
		log.Error("this section was applied and some of its keys cannot be read back, so nothing here "+
			"says whether those moved", "section", name, "count", len(shown)+omitted,
			"keys", strings.Join(shown, ","), "omitted", omitted)
	}

	reportWhatMoved(name, whatBothSidesCouldBeReadFor(keys, unread), before, after, log, said)
}

// copyNodeConfig returns a copy of from whose exported references share nothing with it. See
// detachReferences.
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

// whatBothSidesCouldBeReadFor returns keys minus those in unread.
func whatBothSidesCouldBeReadFor(keys []string, unread map[string]struct{}) []string {
	out := make([]string, 0, len(keys))
	for _, key := range keys {
		if _, missing := unread[key]; !missing {
			out = append(out, key)
		}
	}
	return out
}

// reportWhatMoved logs the keys whose value this delivery changed from what config.toml gave. It names
// keys, never values, because a value can be a secret such as tx-index.psql-conn.
func reportWhatMoved(name string, keys []string, before, after map[string]string, log *slog.Logger,
	said func(string, ...any)) {
	var moved []string
	for _, key := range keys {
		if before[key] != after[key] {
			moved = append(moved, key)
		}
	}
	if len(moved) == 0 {
		log.Debug("this section's written values match what the node's own file already gave it",
			"section", name, "keys", len(keys))
		return
	}
	shown, omitted := capLoggedItems(moved)
	said("this section's settings now differ from what the node's own configuration file says",
		"section", name, "count", len(moved), "keys", strings.Join(shown, ","), "omitted", omitted)
}

// loggerOwnVariable is the environment variable the logger reads at start-up, distinct from the key's
// SEID_ variable.
const loggerOwnVariable = "SEI_LOG_LEVEL"

// asSet collapses repeats, so a key unread on both sides is named once.
func asSet(keys []string) map[string]struct{} {
	out := make(map[string]struct{}, len(keys))
	for _, key := range keys {
		out[key] = struct{}{}
	}
	return out
}

// applyTheLevelTheStructNowHolds applies the log level the delivered configuration struct holds, since the
// boot set the level before delivery. It defers to a level from a flag, SEID_LOG_LEVEL, or
// loggerOwnVariable.
func applyTheLevelTheStructNowHolds(ctx *server.Context, log *slog.Logger) {
	if ctx == nil || ctx.Config == nil {
		return
	}
	text := ctx.Config.LogLevel

	if applied := theLevelTheBootApplied(ctx); applied != "" {
		log.Info("the boot already applied a log level that outranks this node's configuration file; the "+
			"level that file holds is not used", "level", applied, "ignored", text)
		return
	}

	if os.Getenv(loggerOwnVariable) != "" {
		log.Info("a log level is set in the environment under the logger's own variable, which the "+
			"node already applied; the level this node's configuration holds is not used",
			"variable", loggerOwnVariable, "ignored", text)
		return
	}
	var level slog.Level
	if err := level.UnmarshalText([]byte(text)); err != nil {
		log.Error("the log level this node's configuration holds cannot be read; the node keeps the level "+
			"it already had", "level", text, "err", err)
		return
	}
	seilog.SetDefaultLevel(level, true)
	// That reset this package's floor too.
	keepOwnReportingVisible()
	log.Info("log level applied", "level", text)
}

// theLevelTheBootApplied returns the log level the boot took from --log_level or its SEID_ variable, or
// empty when neither was set.
func theLevelTheBootApplied(ctx *server.Context) string {
	if ctx.Viper == nil {
		return ""
	}
	return ctx.Viper.GetString(flags.FlagLogLevel)
}

// whatTheSectionsOwnRulesSay runs ValidateBasic for the one section of candidate that holds keys, so a
// failure is attributable to that section. It returns nil for a section with no ValidateBasic.
func whatTheSectionsOwnRulesSay(candidate *tmcfg.Config, keys []string) error {
	prefix := sectionPrefix(keys)
	holder := reflect.ValueOf(candidate).Elem()
	for i := 0; i < holder.NumField(); i++ {
		field := holder.Type().Field(i)
		tag := field.Tag.Get("mapstructure")
		name := strings.Split(tag, ",")[0]
		squashed := strings.Contains(tag, ",squash")

		// Root keys belong to the squashed base group.
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
