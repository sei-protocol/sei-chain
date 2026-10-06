package configtest

import (
	"log/slog"
	"os"
	"path"
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"

	"github.com/sei-protocol/seilog"
)

// envAllowlist names the variables Isolate preserves. Everything outside it is
// unset, because the legacy path runs one viper with an empty env prefix and
// AutomaticEnv, which makes *any* bare variable whose upper-cased name matches a
// bound key a config source (TRACE, HOME, OUTPUT, CHAIN_ID all land). The
// allowlist is therefore restricted to what the Go toolchain and the OS need to
// run a test binary at all.
//
// Kept out on purpose, and named here because the resulting failure would not look
// like an environment problem: SSL_CERT_FILE and SSL_CERT_DIR, the XDG_* group, and
// USER with LOGNAME. Nothing pinned today needs any of them, since no row makes an
// outbound TLS call and the one row that touches a keyring pins
// keyring-backend = "test". A row that does need one will fail somewhere far from
// this file, so the fix is to add the variable here with a line saying which row
// requires it rather than to widen the list pre-emptively. Every entry added is a
// name the empty-prefix viper can then resolve as a config value, which is the cost
// that keeps this list short.
var envAllowlist = map[string]bool{
	// CI is required by recordWriteRefused in golden.go: it refuses to rewrite a checked-in record
	// on a CI run, and a refusal keyed on a variable Isolate strips would silently no-op in any
	// test that isolates before writing — reopening the hole with nothing to notice. Its cost here
	// is nil rather than merely small, in that no key in the closed read-key set is named ci, and the
	// env lane recorded no bare CI resolution, so the empty-prefix viper has nothing to resolve it to.
	//
	// Two consequences are worth having next to the entry. Isolate is no longer hermetic with respect
	// to CI, and the nil cost above rests on no read key ever being named ci, which nothing asserts.
	// And -update cannot run wherever CI is set, which is the intended posture, so refuseRecordWrite
	// names the variable and gives the env -u form rather than saying only "regenerate it locally".
	"CI":                true,
	"PATH":              true,
	"TMPDIR":            true,
	"TMP":               true,
	"TEMP":              true,
	"GOCACHE":           true,
	"GOCOVERDIR":        true,
	"GOFLAGS":           true,
	"GOMODCACHE":        true,
	"GOPATH":            true,
	"GOROOT":            true,
	"GOTMPDIR":          true,
	"DYLD_LIBRARY_PATH": true,
	"LD_LIBRARY_PATH":   true,
}

// logProbeName is the one registry entry this package adds, used only to read a value
// seilog does not otherwise expose.
const logProbeName = "configtest"

// LogDefaultLevel reports seilog's current default level, which is the level every logger in
// the process runs at.
//
// seilog has SetDefaultLevel and no matching getter, so the value is read through a probe
// logger. NewLogger returns the existing registry entry for a name it has seen before, and
// SetDefaultLevel(_, true) writes every registered entry, so this one name follows the
// default rather than freezing at whatever it was when first created.
func LogDefaultLevel() slog.Level {
	seilog.NewLogger(logProbeName, "isolate")
	level, _ := seilog.GetLevel(logProbeName + "/isolate")
	return level
}

// Isolate pins the process environment for the duration of the test: every
// variable outside envAllowlist is unset, $HOME is repointed at a scratch
// directory, and the original environment is restored on cleanup.
//
// Two properties make this load-bearing rather than hygiene. The global viper
// runs with an empty env prefix, so an unrelated variable on the developer's
// machine can silently become a resolved config value; and $HOME outranks the
// --home flag default in that same viper, so an un-pinned HOME makes several
// commands resolve a different node directory than the fixture. Isolate removes
// both, which is what lets a characterization assertion mean the same thing on a
// laptop and in CI.
//
// The seilog default level IS restored, because this suite writes it constantly.
// InterceptConfigsPreRunHandler calls seilog.SetDefaultLevel whenever a log level
// resolves, so every applyLegacy call in cmd/seid/cmd moves a process global, and
// TestSeiLogLevelEnvSuppressesTheConfigFileValue moves it on purpose. A target that
// reads the level back through LogDefaultLevel depends on that restore for its result
// to be the same in any execution order.
//
// Reading it back needs a detour: seilog exports SetDefaultLevel but no getter. A
// single probe logger stands in. SetDefaultLevel(_, true) updates every registered
// logger, and NewLogger reuses the registry entry for a name it has already seen, so
// one fixed name tracks the default for the life of the process and adds one registry
// entry rather than one per call.
//
// Two preconditions this restore rests on, stated because neither is enforced. It assumes no
// target in the same binary calls seilog.SetDefaultLevel with updateExisting=false or sets a
// level on the probe's own name, either of which would leave the probe reporting a stale
// default; the only in-binary writer today is InterceptConfigsPreRunHandler, which passes
// true. And it does not reset the process-global viper singleton that tmcli's InitEnv builds,
// so a target depending on a pristine one has to arrange that itself. Nothing depends on
// either today.
//
// What it still does NOT restore is server/config's package-global app.toml template
// (config.SetConfigTemplate), also written from inside InterceptConfigsPreRunHandler.
// Nothing asserted today depends on it, but a second manager carrying a different
// template would become execution-order dependent, so such a target needs its own
// save/restore rather than assuming this function covers it.
//
// It returns the scratch $HOME. A test using Isolate cannot call t.Parallel:
// the environment is process-global, and the t.Setenv below makes the testing
// package enforce that.
func Isolate(t testing.TB) string {
	t.Helper()

	// Registers this test as an environment mutator, so the testing package
	// rejects a t.Parallel() here or in any ancestor.
	t.Setenv("SEI_CONFIGTEST_ISOLATED", "1")

	saved := os.Environ()
	t.Cleanup(func() {
		os.Clearenv()
		for _, kv := range saved {
			if k, v, ok := strings.Cut(kv, "="); ok {
				_ = os.Setenv(k, v)
			}
		}
	})

	// Restored with updateExisting=true, matching how InterceptConfigsPreRunHandler sets it.
	//
	// The residue is worth stating: this rewrites every registered logger, so a test that had
	// tuned one individually before calling Isolate does not get that level back. There is no
	// faithful alternative, because seilog exposes no way to enumerate the registry and the
	// code under test already clobbers per-logger levels the same way on every applyLegacy.
	// Restoring with the subject's own semantics is the closest reachable approximation.
	savedLevel := LogDefaultLevel()
	t.Cleanup(func() { seilog.SetDefaultLevel(savedLevel, true) })

	for _, kv := range saved {
		k, _, ok := strings.Cut(kv, "=")
		if ok && !envAllowlist[k] {
			_ = os.Unsetenv(k)
		}
	}

	home := t.TempDir()
	if err := os.Setenv("HOME", home); err != nil {
		t.Fatalf("pin HOME: %v", err)
	}
	return home
}

// EnvValueIsSettable reports whether os.Setenv accepts value. The syscall rejects
// a NUL byte, so a fuzzer exploring arbitrary strings will produce values that
// cannot be placed in the environment at all. Such a value is out of scope rather
// than a failure — an operator has no way to set it either — so a target skips it
// instead of reporting a harness error as a finding.
func EnvValueIsSettable(value string) bool {
	return !strings.ContainsRune(value, 0)
}

// ServerEnvPrefix is the env prefix the server viper answers to:
// path.Base(os.Executable()), the same expression
// server.InterceptConfigsPreRunHandler evaluates. In a production node it is
// "seid"; in a test binary it is that binary's name. Deriving it here rather
// than hardcoding "seid" is what lets a characterization test assert the
// *relationship* between prefix and env var, and it pins the binary-rename edge:
// renaming or symlinking seid changes every variable the node responds to.
func ServerEnvPrefix() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	return path.Base(exe), nil
}

// envKeyReplacer mirrors the replacer InterceptConfigsPreRunHandler installs.
var envKeyReplacer = strings.NewReplacer(".", "_", "-", "_")

// ServerEnvKey returns the environment variable the server viper reads for a
// dotted config key, reproducing viper's own construction: upper-case the
// prefixed key, then apply the "."/"-" to "_" replacer. Note the replacer runs
// over the *whole* name including the prefix, which matters in a test binary
// whose basename contains a dot (config.test -> CONFIG_TEST_...).
//
// Example, for a node built as seid: "state-commit.sc-enable" ->
// "SEID_STATE_COMMIT_SC_ENABLE".
func ServerEnvKey(prefix, key string) string {
	return envKeyReplacer.Replace(strings.ToUpper(prefix + "_" + key))
}

// ClientEnvKey returns the environment variable the *client* viper reads for a
// key. That viper is built by client.Context.WithViper("SEI") with SetEnvPrefix
// plus AutomaticEnv but deliberately no key replacer, so only the upper-casing
// applies. The consequence is a sharp edge worth pinning rather than fixing: a
// dashed key such as chain-id has no legal env spelling at all, and SEI_CHAIN_ID
// silently does nothing.
func ClientEnvKey(key string) string {
	return strings.ToUpper("SEI_" + key)
}

// GlobalEnvKey returns the environment variable the process-global viper reads
// for a key. tmcli.PrepareBaseCmd wires that singleton through InitEnv, which sets
// an empty env prefix and the same "."/"-" to "_" replacer the server viper uses.
// An empty prefix means the name is just the upper-cased, replaced key — so HOME
// matches home, TRACE matches trace, OUTPUT matches output, and any bare variable
// whose name collides with a bound key becomes a config source.
func GlobalEnvKey(key string) string {
	return envKeyReplacer.Replace(strings.ToUpper(key))
}

// IsTOMLWritable reports whether s can appear verbatim inside a TOML basic string:
// valid UTF-8, printable runes only, and no quote or backslash needing an escape.
//
// Fuzz targets that build a config file from a generated string need this. Without it
// the generated document is itself rejected by the TOML parser, and the target reports
// a parse failure as though it were a finding about resolution. Malformed-file behavior
// belongs to the targets that feed arbitrary bytes to Apply on purpose.
//
// The UTF-8 check is not redundant with the printable loop: ranging over a string
// holding invalid bytes yields utf8.RuneError for each one, and RuneError is itself
// printable, so the loop alone would admit exactly the bytes TOML rejects.
func IsTOMLWritable(s string) bool {
	if !utf8.ValidString(s) || strings.ContainsAny(s, "\"\\") {
		return false
	}
	for _, r := range s {
		if !unicode.IsPrint(r) {
			return false
		}
	}
	return true
}
