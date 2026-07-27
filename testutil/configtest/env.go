package configtest

import (
	"os"
	"path"
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"
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
// What it does NOT restore, because the legacy read path mutates more than the
// environment: server/config's package-global app.toml template
// (config.SetConfigTemplate) and seilog's default level (seilog.SetDefaultLevel),
// both written from inside InterceptConfigsPreRunHandler. Nothing asserted today
// depends on either, but a target that asserts on log level, or a second manager
// carrying a different template, becomes execution-order dependent — so such a
// target needs its own save/restore rather than assuming this function covers it.
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
