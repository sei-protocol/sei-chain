package statesyncsnapshot

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
)

const (
	childEnv       = "STATESYNCSNAPSHOT_TEST_CHILD"
	crashExitCode  = 3
	maxRerunsToEnd = 3
)

// childSpec tells TestChildProcess what to run.
type childSpec struct {
	Op   string `json:"op"`
	PSD  string `json:"psd"`
	From string `json:"from"`
	Out  string `json:"out"`
	Step string `json:"step"`
}

// TestChildProcess runs a command in a child process for the parent tests. At spec.Step it exits at
// once, without running any deferred cleanup, like a crash.
func TestChildProcess(t *testing.T) {
	raw := os.Getenv(childEnv)
	if raw == "" {
		t.Skip("run by the parent tests")
	}
	var spec childSpec
	require.NoError(t, json.Unmarshal([]byte(raw), &spec))
	tg := targetAt(spec.PSD)
	crash := func(step string) error {
		if step == spec.Step {
			os.Exit(crashExitCode)
		}
		return nil
	}
	ctx := context.Background()
	switch spec.Op {
	case "create":
		require.NoError(t, createWith(ctx, tg, createOptions{out: spec.Out}, os.Stdout, os.Stderr, crash))
	case "restore":
		require.NoError(t, restoreWith(ctx, tg, spec.From, nil, os.Stderr, crash))
	case "create-stdout-with-logs":
		archive, err := takeStdout()
		require.NoError(t, err)
		_, _ = fmt.Fprintln(os.Stdout, "INFO a log line that a logger writes to stdout")
		require.NoError(t, runCreate(ctx, tg, createOptions{out: "-"}, archive, os.Stderr))
		require.NoError(t, archive.Close())
	case "create-command-with-pre-run-log":
		parent := &cobra.Command{Use: "seid", SilenceUsage: true, PersistentPreRunE: func(*cobra.Command, []string) error {
			_, _ = fmt.Fprintln(os.Stdout, "INFO a log line that a pre-run hook writes to stdout")
			return errStopped
		}}
		parent.AddCommand(Cmd())
		parent.SetArgs([]string{"state-sync-snapshot", "create", "--out", "-"})
		require.ErrorIs(t, parent.Execute(), errStopped)
	default:
		t.Fatalf("unknown op %q", spec.Op)
	}
}

// runChild runs spec in a child process and returns its exit code, stdout and stderr.
func runChild(t *testing.T, spec childSpec) (int, []byte, []byte) {
	t.Helper()
	raw, err := json.Marshal(spec)
	require.NoError(t, err)
	cmd := exec.Command(os.Args[0], "-test.run=^TestChildProcess$", "-test.count=1") //nolint:gosec // the test binary itself.
	cmd.Env = append(os.Environ(), childEnv+"="+string(raw))
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err = cmd.Run()
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode(), stdout.Bytes(), stderr.Bytes()
	}
	require.NoError(t, err, "stderr: %s", stderr.String())
	return 0, stdout.Bytes(), stderr.Bytes()
}

// stopKind is how a test stops a command at a step.
type stopKind string

const (
	stopWithError stopKind = "error"
	stopWithCrash stopKind = "crash"
)

// stopOnce runs a command that stops at step, and reports whether it reached step.
func stopOnce(t *testing.T, kind stopKind, step string, inProcess func(stepHook) error, spec childSpec) bool {
	t.Helper()
	if kind == stopWithError {
		err := inProcess(stopAt(step))
		if err == nil {
			return false
		}
		require.ErrorIs(t, err, errStopped)
		return true
	}
	spec.Step = step
	code, _, stderr := runChild(t, spec)
	if code == 0 {
		return false
	}
	require.Equal(t, crashExitCode, code, "child stderr: %s", stderr)
	return true
}

// rerunUntilSuccess runs f until it returns nil.
func rerunUntilSuccess(t *testing.T, f func() error) {
	t.Helper()
	var err error
	for range maxRerunsToEnd {
		if err = f(); err == nil {
			return
		}
	}
	require.NoError(t, err, "the same command did not complete after %d reruns", maxRerunsToEnd)
}

func TestRestoreFailureGoals(t *testing.T) {
	p := newProducer(t, 2)
	earlierHeight := p.commit(t, 1)
	earlier, _ := p.createArchive(t)
	h := p.commit(t, 2)
	archive, _ := p.createArchive(t)
	p.closeStore(t)

	type home struct {
		newHome func(t *testing.T) target
		marker  utils.Option[int64]
	}
	homes := map[string]home{
		"a fresh home": {newHome: freshHome, marker: utils.None[int64]()},
		"a home with an earlier restore": {
			newHome: func(t *testing.T) target {
				tg := freshHome(t)
				require.NoError(t, runRestore(t.Context(), tg, earlier, nil, &bytes.Buffer{}))
				writeAnchor(t, tg)
				return tg
			},
			marker: utils.Some(earlierHeight),
		},
	}
	steps := []string{
		restoreStepChecked, restoreStepVaultCleared, restoreStepFlatKVCleared, restoreStepImported,
		restoreStepMarked, restoreStepProofInstalled, restoreStepEpochSeeded, restoreStepInstalled,
	}
	for homeName, hm := range homes {
		for _, step := range steps {
			for _, kind := range []stopKind{stopWithError, stopWithCrash} {
				t.Run(fmt.Sprintf("%s/%s/%s", homeName, step, kind), func(t *testing.T) {
					tg := hm.newHome(t)
					before := homeTree(t, tg)
					restore := func(stop stepHook) error {
						return restoreWith(t.Context(), tg, archive, nil, io.Discard, stop)
					}
					spec := childSpec{Op: "restore", PSD: tg.layout.PersistentStateDir, From: archive}
					if !stopOnce(t, kind, step, restore, spec) {
						t.Skipf("restore does not reach %q on this home", step)
					}
					requireSafeStop(t, tg, step, before, hm.marker, p.proofs[h], h)
					rerunUntilSuccess(t, func() error { return restore(noStop) })
					requireRestored(t, tg, p, h)
				})
			}
		}
	}
}

// requireSafeStop asserts what a restore of the checkpoint at h, whose state sync proof file is
// proof, left on the home of tg when it stopped at step. The home held before and the restore
// marker for marker when the restore started. Before restore writes its marker, the home keeps its
// marker, and keeps its files until restore clears an earlier restore. Once restore writes its
// marker, FlatKV reaches h only at the last step, so a node never finds FlatKV at the marker height
// without the state sync proof file and the epoch registry of that height.
func requireSafeStop(
	t *testing.T,
	tg target,
	step string,
	before map[string]string,
	marker utils.Option[int64],
	proof []byte,
	h int64,
) {
	t.Helper()
	switch step {
	case restoreStepChecked:
		require.Equal(t, before, homeTree(t, tg))
	case restoreStepVaultCleared:
		requireMarker(t, tg, marker)
		require.NoDirExists(t, tg.layout.AppHashVaultDir)
		require.Equal(t, marker.OrPanic("only an earlier restore has a vault to clear"), flatKVHeight(t, tg))
	case restoreStepFlatKVCleared, restoreStepImported:
		requireMarker(t, tg, marker)
		if marker.IsPresent() {
			require.NoDirExists(t, tg.layout.AppHashVaultDir)
			require.Zero(t, flatKVHeight(t, tg))
		} else {
			require.Equal(t, before, homeTree(t, tg))
		}
	case restoreStepMarked, restoreStepEpochSeeded:
		requireMarker(t, tg, utils.Some(h))
		require.Zero(t, flatKVHeight(t, tg))
	case restoreStepProofInstalled:
		requireMarker(t, tg, utils.Some(h))
		got, err := os.ReadFile(tg.layout.RestoreProofFile)
		require.NoError(t, err)
		require.Equal(t, proof, got)
		require.Zero(t, flatKVHeight(t, tg))
	case restoreStepInstalled:
		requireMarker(t, tg, utils.Some(h))
		require.Equal(t, h, flatKVHeight(t, tg))
	default:
		t.Fatalf("no stop state for step %q", step)
	}
}

// homeTree is treeOf the persistent state dir of tg without the staging dir.
func homeTree(t *testing.T, tg target) map[string]string {
	t.Helper()
	tree := treeOf(t, tg.layout.PersistentStateDir)
	for p := range tree {
		if p == restoreStagingDirName || strings.HasPrefix(p, restoreStagingDirName+string(filepath.Separator)) {
			delete(tree, p)
		}
	}
	return tree
}

func TestCreateFailureGoals(t *testing.T) {
	p := newProducer(t, 2)
	p.commit(t, 1)
	h := p.commit(t, 2)
	p.closeStore(t)
	want := treeOf(t, p.layout.PersistentStateDir)

	steps := []string{createStepStaged, createStepLinked, createStepManifest, createStepWritten}
	for _, toStdout := range []bool{false, true} {
		for _, step := range steps {
			for _, kind := range []stopKind{stopWithError, stopWithCrash} {
				t.Run(fmt.Sprintf("stdout=%t/%s/%s", toStdout, step, kind), func(t *testing.T) {
					outDir := t.TempDir()
					out := filepath.Join(outDir, "archive.tar.zst")
					if toStdout {
						out = "-"
					}
					create := func(stop stepHook) error {
						return createWith(t.Context(), p.target, createOptions{out: out}, io.Discard, io.Discard, stop)
					}
					spec := childSpec{Op: "create", PSD: p.layout.PersistentStateDir, Out: out}
					require.True(t, stopOnce(t, kind, step, create, spec))
					rerunUntilSuccess(t, func() error { return create(noStop) })

					require.Equal(t, want, treeOf(t, p.layout.PersistentStateDir))
					entries, err := os.ReadDir(outDir)
					require.NoError(t, err)
					if toStdout {
						require.Empty(t, entries)
						return
					}
					require.Len(t, entries, 1)
					require.Equal(t, h, readManifest(t, out).CheckpointHeight)
				})
			}
		}
	}
}

func TestCreateKeepsLogLinesOutOfTheArchive(t *testing.T) {
	p := newProducer(t, 2)
	h := p.commit(t, 1)
	p.closeStore(t)

	code, stdout, stderr := runChild(t, childSpec{Op: "create-stdout-with-logs", PSD: p.layout.PersistentStateDir})
	require.Equal(t, 0, code, "child stderr: %s", stderr)
	require.Contains(t, string(stderr), "a log line that a logger writes to stdout")
	require.Equal(t, h, heightLine(t, filterHeightLines(stderr)))

	dst := freshHome(t)
	require.NoError(t, runRestore(t.Context(), dst, "-", bytes.NewReader(stdout), &bytes.Buffer{}))
	requireRestored(t, dst, p, h)
}

func TestCreateCommandTakesStdoutBeforeTheParentPreRun(t *testing.T) {
	code, stdout, stderr := runChild(t, childSpec{Op: "create-command-with-pre-run-log"})
	require.Equal(t, 0, code, "child stderr: %s", stderr)
	require.Empty(t, stdout)
	require.Contains(t, string(stderr), "a log line that a pre-run hook writes to stdout")
}

// filterHeightLines keeps only the lines of out that start like a height line.
func filterHeightLines(out []byte) string {
	var kept []byte
	for _, line := range bytes.Split(out, []byte("\n")) {
		if bytes.HasPrefix(line, []byte(`{"height":`)) {
			kept = append(append(kept, line...), '\n')
		}
	}
	return string(kept)
}
