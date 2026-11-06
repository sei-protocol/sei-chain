package rpc

import (
	"maps"
	"path/filepath"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/giga/evmonly/rpc/parity"
)

// parityFixtureDir holds the recorded v2 fixtures and the known differences.
var parityFixtureDir = filepath.Join("testdata", "parity")

// TestV2Parity replays each recorded fixture through the EVM-only executor and
// RPC, and requires every response to match v2's recorded response except
// where a known difference accepts it. Every known difference must still occur.
func TestV2Parity(t *testing.T) {
	fixtures, err := parity.LoadFixtures(parityFixtureDir)
	require.NoError(t, err)
	require.NotEmpty(t, fixtures, "no fixtures in %s", parityFixtureDir)
	known, err := parity.LoadKnownDiffs(filepath.Join(parityFixtureDir, parity.KnownDiffsFile))
	require.NoError(t, err)

	replayed := 0
	for _, fixture := range fixtures {
		t.Run(fixture.Name, func(t *testing.T) {
			replayed++
			requireParity(t, fixture, known)
		})
	}
	// A filtered run cannot tell a stale entry from one another fixture matches.
	if replayed < len(fixtures) {
		return
	}
	for _, entry := range known.Unmatched() {
		t.Errorf("known difference no longer occurs, remove it from %s: %+v", parity.KnownDiffsFile, entry)
	}
}

// requireParity sends every request in fixture to a replay of it and reports
// the differences no known difference accepts, one line per method and path.
func requireParity(t *testing.T, fixture *parity.Fixture, known *parity.KnownDiffs) {
	call := serveFixture(t, fixture)
	unexpected := map[string]*diffGroup{}
	for _, request := range fixture.Requests {
		got, err := call(t.Context(), request.Method, request.Params)
		require.NoError(t, err, "%s%s", request.Method, parity.FormatParams(request.Params))
		diffs, err := parity.Compare(request.V2, got)
		require.NoError(t, err, "%s%s", request.Method, parity.FormatParams(request.Params))
		for _, diff := range diffs {
			if known.Accepts(fixture.Name, request, diff) {
				continue
			}
			key := request.Method + " " + parity.PathPattern(diff.Path)
			group, ok := unexpected[key]
			if !ok {
				group = &diffGroup{example: request, diff: diff}
				unexpected[key] = group
			}
			group.count++
		}
	}
	for _, key := range slices.Sorted(maps.Keys(unexpected)) {
		group := unexpected[key]
		v2, evmOnly := group.diff.Display()
		t.Errorf("%s (%d occurrences), e.g. %s%s at %s:\n    v2:       %s\n    evm-only: %s",
			key, group.count, group.example.Method, parity.FormatParams(group.example.Params), group.diff.Path, v2, evmOnly)
	}
}

type diffGroup struct {
	example parity.Request
	diff    parity.Difference
	count   int
}
