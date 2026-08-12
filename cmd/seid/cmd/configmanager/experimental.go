package configmanager

import (
	"fmt"
	"os"
	"runtime/debug"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/sei-protocol/sei-chain/config/experimental"
	"github.com/sei-protocol/sei-chain/sei-cosmos/server"
)

// sweepingCommands are the fully-qualified command paths that construct an application and
// therefore read appOpts.
//
// Fully-qualified, never a prefix and never a name. sei-cosmos/server/export.go declares Use
// "export" and sei-cosmos/client/keys/export.go declares Use "export <name>", so both are named
// export, and a name match would emit records into `seid keys export mykey > key.asc`, a stream
// carrying an armored private key.
var sweepingCommands = map[string]bool{
	"seid start":     true,
	"seid export":    true,
	"seid rollback":  true,
	"seid ethreplay": true,
	"seid blocktest": true,
	"seid snapshot":  true,
}

// SweepsExperimental reports whether cmd is one of the commands that constructs an application.
//
// Gating on the path rather than emitting everywhere is what keeps the observable change honest.
// Ungated, the hook runs on every command and seilog's stdout sink puts a line inside
// $(seid version) and inside any command a caller pipes.
func SweepsExperimental(cmd *cobra.Command) bool {
	if cmd == nil {
		return false
	}
	return sweepingCommands[cmd.CommandPath()]
}

// ReportExperimental sweeps the resolved configuration for experimental keys this binary does not
// recognize and reports what it found.
//
// Advisory in the same sense the validation pass is: it reads the boot channels, modifies nothing,
// returns nothing, contains a panic in itself, and emits nothing at all when there is nothing to
// report. Internally it is sweepFor then logFindings, the same split as validateAdvisory and
// logAdvisory, so the pass is assertable without reading a log.
func ReportExperimental(cmd *cobra.Command) {
	lg := logger
	defer func() {
		if r := recover(); r != nil {
			// A second panic, from reporting the first, must not escape.
			defer func() { _ = recover() }()
			lg.Error("experimental config sweep panicked (advisory; recovered, node will boot)",
				"panic", r, "stack", string(debug.Stack()))
		}
	}()
	logFindings(lg, sweepFor(cmd))
}

// sweepFor resolves the source the handler populated and sweeps it.
//
// A command whose context never reached the handler yields an empty result rather than a panic.
// cmd.Context() is checked before GetServerContextFromCmd, which dereferences it immediately
// (sei-cosmos/server/util.go:225), so the recover above would otherwise be what keeps a boot
// alive instead of this check.
func sweepFor(cmd *cobra.Command) experimental.Findings {
	if cmd == nil || cmd.Context() == nil {
		return experimental.Findings{}
	}
	ctx := server.GetServerContextFromCmd(cmd)
	if ctx == nil || ctx.Viper == nil {
		return experimental.Findings{}
	}
	// The same derivation the handler used, from the one place that owns it. Reproducing
	// path.Base(os.Executable()) here would drift the moment the handler's changes, and the
	// shadow pass would then look for variables under a prefix no node uses.
	prefix, err := server.EnvPrefix()
	if err != nil {
		// Without the prefix the environment pass cannot run. Findings.EnvPassRan reports that,
		// rather than a clean sweep that had simply not looked.
		return experimental.SweepRegistry(ctx.Viper, "", nil)
	}
	return experimental.SweepRegistry(ctx.Viper, prefix, os.Environ())
}

// logFindings reports a sweep. It emits nothing when there is nothing to say.
//
// Total silence when clean is deliberate and it has a stated cost: a section an external tool
// erased and a section never written are indistinguishable and both silent. The trade is that a
// node with no experimental keys is byte-identical on every command.
func logFindings(lg logging, f experimental.Findings) {
	if f.Empty() {
		return
	}

	// One summary at error level, so an operator has a single line to alert on, and the classes
	// beneath it at warn.
	lg.Error("[experimental] keys need attention (advisory; node will boot)",
		"unrecognized", len(f.Unrecognized), "malformed", len(f.Malformed),
		"shadowed", len(f.Shadowed), "promoted", len(f.Promoted),
		"defects", len(f.Defects), "skipped", f.OversizeNames+f.Truncated)

	if len(f.Unrecognized) > 0 {
		keys, nearest, omitted := renderUnrecognized(f.Unrecognized)
		lg.Warn("unrecognized [experimental] keys (advisory; left in place, not removed)",
			"count", len(f.Unrecognized), "keys", keys, "nearest", nearest, "omitted", omitted)
	}
	for _, ve := range f.Malformed {
		lg.Warn("an [experimental] value is not usable; its declared default is in use",
			"key", ve.Key, "want", ve.Want, "in_effect", ve.Used,
			"got", logName(fmt.Sprint(ve.Raw)), "cause", ve.Cause)
	}
	// Shadowing is reported at error level because the operator's value is silently gone: the key
	// is written, it resolves to nothing, and the declared default is what runs.
	for _, sf := range f.Shadowed {
		lg.Error("an [experimental] key resolves to nothing, so its declared default is in use",
			"key", sf.Key, "cause", orUnknown(sf.Cause))
	}
	for _, pk := range f.Promoted {
		if pk.PromotedTo == "" {
			lg.Warn("an [experimental] key was removed and is no longer read",
				"key", pk.Key, "retired_in", pk.RetiredIn)
			continue
		}
		lg.Warn("an [experimental] key was promoted; move the value to its stable path",
			"key", pk.Key, "promoted_to", pk.PromotedTo, "retired_in", pk.RetiredIn)
	}
	for _, d := range f.Defects {
		lg.Warn("an [experimental] declaration in this binary was refused, so its key is inert",
			"key", d.Name, "reason", d.Reason)
	}
	if n := f.OversizeNames + f.Truncated; n > 0 {
		lg.Warn("[experimental] candidates were skipped before resolution",
			"oversize", f.OversizeNames, "over_limit", f.Truncated)
	}
	if !f.EnvPassRan {
		lg.Warn("the [experimental] environment pass did not run, so a variable collapsing a key " +
			"would not have been reported")
	}
}

// logging is the subset of the logger this file uses, so a test can capture records without
// reaching for a handler.
type logging interface {
	Warn(msg string, args ...any)
	Error(msg string, args ...any)
}

// renderUnrecognized bounds and quotes a key list for one record.
//
// Names are rendered through logName so a name carrying a newline or an escape sequence cannot
// forge a log line, and the list is capped so a rollback that invalidates a whole feature's keys
// cannot produce a record some shippers drop and others split.
func renderUnrecognized(us []experimental.Unrecognized) (keys, nearest []string, omitted int) {
	shown := us
	if len(shown) > maxReportedKeys {
		shown, omitted = shown[:maxReportedKeys], len(us)-maxReportedKeys
	}
	for _, u := range shown {
		keys = append(keys, logName(u.Key))
		// Nearest is dropped for a truncated name, since it is computed on the full name and would
		// otherwise sit beside a token describing a different string.
		if len(u.Key) > experimental.MaxLoggedNameBytes {
			nearest = append(nearest, "")
			continue
		}
		nearest = append(nearest, logName(u.Nearest))
	}
	return keys, nearest, omitted
}

// maxReportedKeys bounds the key list in one record, for the same reason maxLoggedDiagnostics
// bounds the sibling reporter in this package.
const maxReportedKeys = 10

// logName renders one name for a record: truncated, quoted, and marked when it was cut.
//
// Quoted through strconv.QuoteToASCII because a key name comes from an operator's file and can
// carry a newline or an ANSI escape. One helper rather than one per call site, so no record can be
// the one that forgot.
func logName(s string) string {
	if len(s) > experimental.MaxLoggedNameBytes {
		return strconv.QuoteToASCII(s[:experimental.MaxLoggedNameBytes] + "…")
	}
	return strconv.QuoteToASCII(s)
}

// orUnknown renders a shadow cause, naming that none was found rather than rendering empty.
//
// An unexplained shadow is the case deserving the most attention, since nothing in this design
// accounts for it, so it must not look like a missing field.
func orUnknown(cause string) string {
	if strings.TrimSpace(cause) == "" {
		return "unknown; no environment variable accounts for it"
	}
	return logName(cause)
}
