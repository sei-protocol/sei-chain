package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sei-protocol/sei-chain/testutil/configtest"
	"github.com/spf13/viper"
)

// config.toml is the half of the surface that reaches consensus, and it validates
// almost nothing. The rows here pin the parts it does validate, the defaults that
// are computed from the machine rather than declared, and the two root-scope keys
// whose placement in the file decides whether they are read at all.
//
// The reads themselves go through viper.Unmarshal, so what a test controls is the
// document and what it observes is the struct — the same path
// server.interceptConfigs takes.

// unmarshalConfigTOML parses a config.toml body the way interceptConfigs does:
// defaults first, then the file merged over them.
func unmarshalConfigTOML(t testing.TB, body string) (*Config, error) {
	t.Helper()
	v := viper.New()
	v.SetConfigType("toml")
	if err := v.ReadConfig(strings.NewReader(body)); err != nil {
		return nil, err
	}
	conf := DefaultConfig()
	if err := v.Unmarshal(conf); err != nil {
		return nil, err
	}
	return conf, nil
}

// FuzzValidateBasicMode pins the mode vocabulary, the only field in config.toml
// whose absence is an error rather than a default.
//
// Mode decides whether a node signs blocks, so the three names are a closed set and
// an empty value is refused with its own message rather than falling back to full.
// The distinction matters for the diagnostic: "no mode has been set" tells an
// operator the key is missing, while "unknown mode" tells them it is misspelled, and
// a validator that silently became a full node would be far worse than either.
func FuzzValidateBasicMode(f *testing.F) {
	f.Add("full")
	f.Add("validator")
	f.Add("seed")
	f.Add("")
	f.Add("Validator") // case-sensitive
	f.Add("archive")   // not a tendermint mode
	f.Add(" full")     // not trimmed

	f.Fuzz(func(t *testing.T, mode string) {
		conf := DefaultConfig()
		conf.Mode = mode
		err := conf.ValidateBasic()

		switch mode {
		case ModeFull, ModeValidator, ModeSeed:
			if err != nil {
				t.Fatalf("mode = %q is a known mode and must validate, got %v", mode, err)
			}
		case "":
			if err == nil {
				t.Fatal("an empty mode must be rejected rather than defaulting to full")
			}
		default:
			if err == nil {
				t.Fatalf("mode = %q must be rejected as unknown", mode)
			}
		}
	})
}

// TestValidateBasicDistinguishesAnAbsentModeFromAnUnknownOne pins the property the two
// mode diagnostics exist for, without pinning either one's wording.
//
// The distinction is operator-facing and worth holding: "no mode has been set" says the key
// is missing, "unknown mode" says it is misspelled, and collapsing them into one message
// would leave an operator unable to tell which mistake they made. Asserting that the two
// errors differ keeps that property while surviving any rewording.
//
// It is expressed this way because there is nothing better available. The repo's guide asks
// for errors.Is/As rather than message matching, and both sites build their errors with a
// bare errors.New or fmt.Errorf, so there is no identity to match on. Giving them sentinels
// means editing production code, which this PR deliberately does not do. That follow-up is
// tracked as PLT-855, so the exemption is a recorded decision rather than a precedent set
// in a comment.
func TestValidateBasicDistinguishesAnAbsentModeFromAnUnknownOne(t *testing.T) {
	absent := DefaultConfig()
	absent.Mode = ""
	unknown := DefaultConfig()
	unknown.Mode = "archive"

	absentErr := absent.ValidateBasic()
	unknownErr := unknown.ValidateBasic()
	if absentErr == nil || unknownErr == nil {
		t.Fatalf("both an absent and an unknown mode must be rejected, got %v and %v",
			absentErr, unknownErr)
	}
	if absentErr.Error() == unknownErr.Error() {
		t.Fatalf("an absent mode and a misspelled one now report the same failure (%v), so an "+
			"operator cannot tell whether the key is missing or wrong", absentErr)
	}
}

// FuzzRootScopeKeysRequireRootScope pins the placement trap on the two root-scope
// keys.
//
// autobahn-config-file and hash-vault-disabled-unsafe are declared at the top level
// of the Config struct, so in TOML they must appear before any [section] header.
// Written after one they become that section's key — p2p.autobahn-config-file —
// which nothing reads, and the node starts with the subsystem the operator meant to
// enable silently disabled. There is no diagnostic, because a key nobody reads is
// indistinguishable from a key nobody wrote.
//
// The autobahn pointer is the master gate for the whole GigaRouter path, so a
// silently-ignored placement is the difference between running autobahn and not.
func FuzzRootScopeKeysRequireRootScope(f *testing.F) {
	f.Add(true, false, "/var/lib/sei/autobahn.json")
	f.Add(true, true, "/var/lib/sei/autobahn.json")
	f.Add(false, false, "")
	f.Add(true, false, "relative/path.json")

	f.Fuzz(func(t *testing.T, present, underSection bool, path string) {
		// The property under test is placement, not the path's bytes, and the path is
		// written into a TOML basic string — which forbids control characters and
		// requires valid UTF-8. Restricting to what an operator can actually type
		// keeps a rejected *document* from being reported as a placement failure;
		// malformed-file behavior is pinned through Apply instead.
		if !configtest.IsTOMLWritable(path) {
			return
		}

		var doc strings.Builder
		if present {
			if underSection {
				doc.WriteString("[p2p]\n")
			}
			doc.WriteString("autobahn-config-file = \"" + path + "\"\n")
			doc.WriteString("hash-vault-disabled-unsafe = true\n")
		}

		conf, err := unmarshalConfigTOML(t, doc.String())
		if err != nil {
			t.Fatalf("the document must parse as TOML, got %v: %s", err, doc.String())
		}

		wantPath := ""
		wantDisabled := false
		if present && !underSection {
			wantPath = path
			wantDisabled = true
		}
		if conf.AutobahnConfigFile != wantPath {
			t.Fatalf("autobahn-config-file resolved to %q, want %q (present=%v underSection=%v); "+
				"root-scope keys are only read before the first section header",
				conf.AutobahnConfigFile, wantPath, present, underSection)
		}
		if conf.HashVaultDisabledUnsafe != wantDisabled {
			t.Fatalf("hash-vault-disabled-unsafe resolved to %v, want %v (present=%v underSection=%v)",
				conf.HashVaultDisabledUnsafe, wantDisabled, present, underSection)
		}
	})
}

// TestConsensusWalFileLayoutFollowsFilesystemState pins the consensus WAL's path
// selection, which mirrors the database layout fallback: while wal-file holds either
// default spelling, the path chosen depends on whether a legacy cs.wal directory exists.
//
// An explicitly-set wal-file is honored verbatim, so the fallback only applies to
// operators who never touched the key — which is most of them, and which is why the same
// config resolves to two different paths on two nodes.
func TestConsensusWalFileLayoutFollowsFilesystemState(t *testing.T) {
	t.Run("fresh home uses the subdirectory layout", func(t *testing.T) {
		cfg := DefaultConfig()
		cfg.SetRoot(t.TempDir())
		got := cfg.Consensus.WalFile()
		if !strings.Contains(got, filepath.Join("tendermint", "cs.wal")) {
			t.Fatalf("wal file = %q, want the tendermint/cs.wal layout on a fresh home", got)
		}
	})

	t.Run("legacy directory pins the flat layout", func(t *testing.T) {
		root := t.TempDir()
		if err := os.MkdirAll(filepath.Join(root, "data", "cs.wal"), 0o750); err != nil {
			t.Fatalf("create legacy dir: %v", err)
		}
		cfg := DefaultConfig()
		cfg.SetRoot(root)
		got := cfg.Consensus.WalFile()
		if strings.Contains(got, filepath.Join("tendermint", "cs.wal")) {
			t.Fatalf("wal file = %q, want the flat data/cs.wal layout when legacy data exists", got)
		}
	})

	t.Run("an explicit wal-file is honored verbatim", func(t *testing.T) {
		root := t.TempDir()
		if err := os.MkdirAll(filepath.Join(root, "data", "cs.wal"), 0o750); err != nil {
			t.Fatalf("create legacy dir: %v", err)
		}
		cfg := DefaultConfig()
		cfg.Consensus.WalPath = filepath.Join("data", "chosen", "wal")
		cfg.SetRoot(root)
		got := cfg.Consensus.WalFile()
		if got != filepath.Join(root, "data", "chosen", "wal") {
			t.Fatalf("an explicit wal-file must bypass the layout fallback, got %q", got)
		}
	})
}

// TestToMempoolConfigIsALossyProjection records that the [mempool] section reaches the node
// by two routes, and that ToMempoolConfig is only one of them.
//
// The projection does not carry every field, and the fields it drops are not therefore
// unreachable. check-tx-error-blacklist-enabled and check-tx-error-threshold carry
// mapstructure tags, are rendered into every generated config.toml, and are read straight off
// the outer *config.MempoolConfig by the mempool reactor. An operator who sets them gets the
// behavior; they simply never pass through ToMempoolConfig on the way.
//
// That is the trap for a replacement manager. Reproducing ToMempoolConfig's output looks like
// reproducing the mempool's configuration, and it would silently drop two live knobs. So this
// row asserts both routes: the values arrive on the outer struct, and they are invisible in
// the projection.
//
// pending-ttl-duration and pending-ttl-num-blocks are the opposite case, writable and
// rendered into the generated file but documented in the struct as having no effect. They
// belong to the inert-key class this suite records rather than fixes.
func TestToMempoolConfigIsALossyProjection(t *testing.T) {
	// Every value here differs from its default, so a field that fails to arrive is
	// distinguishable from one that arrived carrying the default.
	const doc = `mode = "full"

[mempool]
check-tx-error-blacklist-enabled = false
check-tx-error-threshold = 7
pending-ttl-num-blocks = 9
`
	fromFile, err := unmarshalConfigTOML(t, doc)
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	declared := DefaultMempoolConfig()
	if !declared.CheckTxErrorBlacklistEnabled || declared.CheckTxErrorThreshold == 7 ||
		declared.PendingTTLNumBlocks == 9 {
		t.Fatalf("the fixture no longer differs from the declared defaults (%v, %d, %d), so this "+
			"row cannot tell an arriving value from a default", declared.CheckTxErrorBlacklistEnabled,
			declared.CheckTxErrorThreshold, declared.PendingTTLNumBlocks)
	}

	// Route one: the outer struct, which is where the reactor reads them.
	if fromFile.Mempool.CheckTxErrorBlacklistEnabled {
		t.Fatal("check-tx-error-blacklist-enabled = false did not reach the outer MempoolConfig. " +
			"The reactor reads the field there, so an operator-set value has to arrive or peer " +
			"blacklisting stays on when it was turned off")
	}
	if got := fromFile.Mempool.CheckTxErrorThreshold; got != 7 {
		t.Fatalf("check-tx-error-threshold reached the outer MempoolConfig as %d, want 7", got)
	}
	if got := fromFile.Mempool.PendingTTLNumBlocks; got != 9 {
		t.Fatalf("pending-ttl-num-blocks reached the outer MempoolConfig as %d, want 9", got)
	}

	// Route two: the projection, which is blind to all three. Setting them must leave
	// ToMempoolConfig's output identical to the default projection, or the loss has changed
	// shape and a manager built against this manifest needs to know.
	got := configtest.Dump(fromFile.Mempool.ToMempoolConfig())
	want := configtest.Dump(declared.ToMempoolConfig())
	if got != want {
		t.Fatalf("ToMempoolConfig now carries a field it used to drop. That is an improvement, and "+
			"it changes which keys a manager has to reproduce through the projection rather than "+
			"off the outer struct, so it gets recorded here\n--- got\n%s\n--- want\n%s", got, want)
	}
}

// TestAutobahnPointerAbsenceDisablesTheSubsystem pins the gate itself: an empty
// pointer is how autobahn is turned off, so any path that loses the value also loses
// the subsystem.
func TestAutobahnPointerAbsenceDisablesTheSubsystem(t *testing.T) {
	conf := DefaultConfig()
	if conf.AutobahnConfigFile != "" {
		t.Fatalf("the default autobahn pointer must be empty, got %q", conf.AutobahnConfigFile)
	}
	if conf.HashVaultDisabledUnsafe {
		t.Fatal("the default must leave the app-hash equivocation guard enabled")
	}
}

// TestDefaultMonikerComesFromTheHostname records that a config default can be
// machine-derived, which is why generated config.toml bytes are not a comparable
// artifact between two nodes or two managers.
//
// The default is os.Hostname() computed at package init, with a silent fallback to
// "anonymous" when the call fails. Nothing downstream validates it, so a node's
// identity in logs and telemetry depends on where it was started.
func TestDefaultMonikerComesFromTheHostname(t *testing.T) {
	host, err := os.Hostname()
	if err != nil {
		t.Skipf("hostname unavailable: %v", err)
	}
	got := DefaultConfig().Moniker
	if got != host && got != "anonymous" {
		t.Fatalf("default moniker = %q, want the hostname %q or the anonymous fallback", got, host)
	}
}

// TestDefaultP2PConnectionBudgetIsBounded pins the P2P defaults an absent [p2p]
// section resolves to. They are bounds, not preferences: a node that resolved 0 for
// MaxConnections would be interpreted downstream as a request for the code's own
// fallback rather than for unlimited, so the declared default is what keeps the
// budget legible.
func TestDefaultP2PConnectionBudgetIsBounded(t *testing.T) {
	p2p := DefaultP2PConfig()
	if p2p.MaxConnections == 0 {
		t.Fatal("DefaultP2PConfig must declare a non-zero MaxConnections; a zero here is read " +
			"downstream as 'use the code fallback', not as 'unlimited'")
	}
	if p2p.MaxIncomingConnectionAttempts == 0 {
		t.Fatal("DefaultP2PConfig must declare a non-zero MaxIncomingConnectionAttempts")
	}
	if p2p.QueueType == "" {
		t.Fatal("DefaultP2PConfig must declare a queue type")
	}

	// An absent [p2p] section resolves to exactly those defaults.
	conf, err := unmarshalConfigTOML(t, "mode = \"full\"\n")
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if a, b := configtest.Dump(conf.P2P.MaxConnections), configtest.Dump(p2p.MaxConnections); a != b {
		t.Fatalf("absent [p2p] resolved MaxConnections to %s, want %s", a, b)
	}
}

// TestValidatorModeDefaultsDisableTheTxIndex records a default that differs by mode
// rather than by key. DefaultValidatorConfig force-sets tx-index.indexer to ["null"]
// — a validator does not build the kv index — so the same absent key resolves
// differently depending on which constructor produced the config.
func TestValidatorModeDefaultsDisableTheTxIndex(t *testing.T) {
	full := DefaultConfig()
	validator := DefaultValidatorConfig()

	if validator.Mode != ModeValidator {
		t.Fatalf("DefaultValidatorConfig mode = %q, want %q", validator.Mode, ModeValidator)
	}
	if len(validator.TxIndex.Indexer) != 1 || validator.TxIndex.Indexer[0] != "null" {
		t.Fatalf("validator tx-index.indexer = %v, want [null]", validator.TxIndex.Indexer)
	}
	if a, b := configtest.Dump(full.TxIndex.Indexer), configtest.Dump(validator.TxIndex.Indexer); a == b {
		t.Fatal("the full-node and validator tx-index defaults are identical; the mode-conditional " +
			"default is gone, which changes what a validator indexes")
	}
}
