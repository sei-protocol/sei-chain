package sei_test

import (
	"bufio"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/mod/modfile"
	"golang.org/x/mod/module"
)

// bannedUpstream lists upstream modules that Sei does not depend on directly.
// Most have local forks; IBC is retired.
var bannedUpstream = []string{
	"github.com/tendermint/tendermint",
	"github.com/cosmos/cosmos-sdk",
	"github.com/CosmWasm/wasmd",
	"github.com/CosmWasm/wasmvm",
	"github.com/cosmos/ibc-go",
}

// isBannedModule reports whether modPath (after stripping any /vN suffix)
// matches a banned upstream module exactly.
func isBannedModule(modPath string) (banned string, ok bool) {
	prefix, _, valid := module.SplitPathVersion(modPath)
	if !valid {
		prefix = modPath
	}
	for _, b := range bannedUpstream {
		if prefix == b {
			return b, true
		}
	}
	return "", false
}

// TestGoMod_NoBannedDependencies ensures that go.mod require directives never
// reference banned upstream modules.
//
// Module paths may carry a major-version suffix (e.g. github.com/cosmos/ibc-go/v6).
// The check strips the suffix with module.SplitPathVersion before comparing,
// so both github.com/cosmos/ibc-go and github.com/cosmos/ibc-go/v6 are caught.
//
// Modules that share a prefix but are distinct (e.g. github.com/tendermint/tm-db,
// github.com/tendermint/go-amino) are intentionally NOT banned.
func TestGoMod_NoBannedDependencies(t *testing.T) {
	data, err := os.ReadFile("go.mod")
	require.NoError(t, err)

	f, err := modfile.Parse("go.mod", data, nil)
	require.NoError(t, err)

	for _, req := range f.Require {
		banned, ok := isBannedModule(req.Mod.Path)
		require.Falsef(t, ok, "go.mod must not depend on banned module %s, but found: %s %s",
			banned, req.Mod.Path, req.Mod.Version)
	}
}

// TestGoSum_NoBannedDependencies ensures that go.sum never contains checksums
// for banned upstream modules.
func TestGoSum_NoBannedDependencies(t *testing.T) {
	f, err := os.Open("go.sum")
	require.NoError(t, err)
	defer func() { require.NoError(t, f.Close()) }()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		// go.sum lines have the format: <module> <version>[/go.mod] <hash>
		modPath, _, _ := strings.Cut(line, " ")
		if modPath == "" {
			continue
		}
		banned, ok := isBannedModule(modPath)
		require.Falsef(t, ok, "go.sum must not reference banned module %s, but found: %s",
			banned, line)
	}
	require.NoError(t, scanner.Err())
}
