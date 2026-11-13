package statesyncsnapshot

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	tmcfg "github.com/sei-protocol/sei-chain/sei-tendermint/config"
	"github.com/sei-protocol/sei-chain/sei-tendermint/node"
)

func TestCreateRestoreRoundTrip(t *testing.T) {
	p := newProducer(t, 2)
	p.commit(t, 1)
	h := p.commit(t, 2)

	t.Run("through files", func(t *testing.T) {
		archive, got := p.createArchive(t)
		require.Equal(t, h, got)
		dst := freshHome(t)
		var stderr bytes.Buffer
		require.NoError(t, runRestore(t.Context(), dst, archive, nil, &stderr))
		require.Equal(t, h, heightLine(t, stderr.String()))
		requireRestored(t, dst, p, h)
	})
	t.Run("through stdout and stdin", func(t *testing.T) {
		var archive, createErr bytes.Buffer
		require.NoError(t, runCreate(t.Context(), p.target, createOptions{out: "-"}, &archive, &createErr))
		require.Equal(t, h, heightLine(t, createErr.String()))
		dst := freshHome(t)
		require.NoError(t, runRestore(t.Context(), dst, "-", &archive, &bytes.Buffer{}))
		requireRestored(t, dst, p, h)
	})
}

func TestRestoreRefusals(t *testing.T) {
	p := newProducer(t, 2)
	p.commit(t, 1)
	archive, _ := p.createArchive(t)
	data, err := os.ReadFile(archive) //nolint:gosec // test path.
	require.NoError(t, err)
	truncated := filepath.Join(t.TempDir(), "truncated.tar.zst")
	require.NoError(t, os.WriteFile(truncated, data[:len(data)/2], 0o600))

	writeFile := func(p string) {
		require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o750))
		require.NoError(t, os.WriteFile(p, []byte("x"), 0o600))
	}
	cases := []struct {
		name    string
		setup   func(tg *target)
		from    string
		wantErr func(l node.AutobahnLayout) string
	}{
		{
			name:    "a validator home",
			setup:   func(tg *target) { tg.mode = tmcfg.ModeValidator },
			wantErr: func(node.AutobahnLayout) string { return "validator" },
		},
		{
			name:    "another chain",
			setup:   func(tg *target) { tg.chainID = "chain-b" },
			wantErr: func(node.AutobahnLayout) string { return "chain id" },
		},
		{
			name:    "block data",
			setup:   func(tg *target) { writeFile(filepath.Join(tg.layout.BlockDBDirs[0], "x")) },
			wantErr: func(l node.AutobahnLayout) string { return "remove " + l.BlockDBDirs[0] },
		},
		{
			name: "block data with a marker",
			setup: func(tg *target) {
				writeFile(filepath.Join(tg.layout.BlockDBDirs[0], "x"))
				writeFile(tg.layout.RestoreMarkerFile)
			},
			wantErr: func(l node.AutobahnLayout) string { return "remove " + l.BlockDBDirs[0] },
		},
		{
			name:    "state store data",
			setup:   func(tg *target) { writeFile(filepath.Join(tg.layout.StateStoreDir, "x")) },
			wantErr: func(l node.AutobahnLayout) string { return l.StateStoreDir },
		},
		{
			name:    "FlatKV data without a marker",
			setup:   func(tg *target) { writeFile(filepath.Join(tg.layout.FlatKVDir, "snapshot-x", "y")) },
			wantErr: func(l node.AutobahnLayout) string { return "remove " + l.FlatKVDir },
		},
		{
			name:    "app hashes without a marker",
			setup:   func(tg *target) { writeFile(filepath.Join(tg.layout.AppHashVaultDir, "x")) },
			wantErr: func(l node.AutobahnLayout) string { return "remove " + l.AppHashVaultDir },
		},
		{
			name:    "a truncated archive",
			from:    truncated,
			wantErr: func(node.AutobahnLayout) string { return "" },
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tg := freshHome(t)
			if tc.setup != nil {
				tc.setup(&tg)
			}
			from := archive
			if tc.from != "" {
				from = tc.from
			}
			before := treeOf(t, tg.layout.PersistentStateDir)
			err := runRestore(t.Context(), tg, from, nil, &bytes.Buffer{})
			require.Error(t, err)
			require.ErrorContains(t, err, tc.wantErr(tg.layout))
			require.Equal(t, before, treeOf(t, tg.layout.PersistentStateDir), "restore changed the home")
		})
	}

	t.Run("a running node", func(t *testing.T) {
		tg := freshHome(t)
		s := openStore(t, tg.layout.FlatKVDir, 2)
		defer func() { require.NoError(t, s.Close()) }()
		err := runRestore(t.Context(), tg, archive, nil, &bytes.Buffer{})
		require.ErrorContains(t, err, "is a node running on this home")
		require.NoFileExists(t, tg.layout.RestoreMarkerFile)
	})
}

func TestRestoreReplacesAnEarlierRestoreThatHasNotSeeded(t *testing.T) {
	p := newProducer(t, 2)
	h1 := p.commit(t, 1)
	first, _ := p.createArchive(t)
	p.commit(t, 2)
	h2 := p.commit(t, 3)
	second, _ := p.createArchive(t)

	dst := freshHome(t)
	require.NoError(t, runRestore(t.Context(), dst, first, nil, &bytes.Buffer{}))
	requireRestored(t, dst, p, h1)
	writeAnchor(t, dst)
	require.NoError(t, runRestore(t.Context(), dst, second, nil, &bytes.Buffer{}))
	requireRestored(t, dst, p, h2)
	require.NoDirExists(t, dst.layout.AppHashVaultDir)
	require.NoError(t, runRestore(t.Context(), dst, second, nil, &bytes.Buffer{}), "a rerun of a finished restore")
	requireRestored(t, dst, p, h2)
}

func TestRestoreRefusesAnArchiveThatFailsItsChecks(t *testing.T) {
	p := newProducer(t, 2)
	h := p.commit(t, 1)
	p.closeStore(t)

	cases := []struct {
		name    string
		change  func(t *testing.T, m *manifest, entries *[]tarEntry)
		wantMsg string
	}{
		{
			name: "a state sync proof of another height",
			change: func(t *testing.T, _ *manifest, entries *[]tarEntry) {
				require.True(t, replaceEntry(*entries, proofArchivePath(h), stateSyncProofAt(t, h+1)))
			},
			wantMsg: "decode the archived state sync proof file",
		},
		{
			name: "a state sync proof that does not decode",
			change: func(t *testing.T, _ *manifest, entries *[]tarEntry) {
				require.True(t, replaceEntry(*entries, proofArchivePath(h), []byte("not a proof")))
			},
			wantMsg: "decode the archived state sync proof file",
		},
		{
			name: "a row that is not in ascending order",
			change: func(t *testing.T, m *manifest, entries *[]tarEntry) {
				key := []byte("bank/\x00")
				value := []byte("x")
				*entries = appendLogicalRow(*entries, key, value)
				m.RowCount++
				m.StateBytes += int64(len(key) + len(value))
			},
			wantMsg: "keys must be in ascending order",
		},
		{
			name: "a row count that does not match the manifest",
			change: func(t *testing.T, m *manifest, entries *[]tarEntry) {
				m.RowCount++
			},
			wantMsg: "manifest records",
		},
		{
			name: "a state hash that does not match the rows",
			change: func(t *testing.T, m *manifest, _ *[]tarEntry) {
				m.StateHash[0] ^= 1
			},
			wantMsg: "the imported checkpoint holds",
		},
		{
			name: "a row value changed at the same length",
			change: func(t *testing.T, _ *manifest, entries *[]tarEntry) {
				for i := range *entries {
					if (*entries)[i].name == rowChunkPath(0) {
						chunk := bytes.Clone((*entries)[i].content)
						chunk[len(chunk)-1] ^= 1
						(*entries)[i].content = chunk
						return
					}
				}
				t.Fatal("the archive has no row chunk")
			},
			wantMsg: "the imported checkpoint holds",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			archive := p.archiveWithChange(t, h, func(m *manifest, entries *[]tarEntry) { tc.change(t, m, entries) })
			tg := freshHome(t)
			before := treeOf(t, tg.layout.PersistentStateDir)
			err := runRestore(t.Context(), tg, archive, nil, &bytes.Buffer{})
			require.Error(t, err)
			require.ErrorContains(t, err, tc.wantMsg)
			require.Equal(t, before, treeOf(t, tg.layout.PersistentStateDir), "restore changed the home")
		})
	}

	t.Run("an unchanged archive", func(t *testing.T) {
		archive := p.archiveWithChange(t, h, nil)
		tg := freshHome(t)
		var stderr bytes.Buffer
		require.NoError(t, runRestore(t.Context(), tg, archive, nil, &stderr))
		require.Contains(t, stderr.String(), "FlatKV import complete")
		requireRestored(t, tg, p, h)
	})
}

// writeAnchor writes a file into the app-hash vault of tg, as the seed of a restore does when it
// stores the restore anchor.
func writeAnchor(t *testing.T, tg target) {
	t.Helper()
	require.NoError(t, os.MkdirAll(tg.layout.AppHashVaultDir, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(tg.layout.AppHashVaultDir, "anchor"), []byte("anchor"), 0o600))
}
