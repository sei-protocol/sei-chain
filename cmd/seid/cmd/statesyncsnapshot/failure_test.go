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
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"
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
	p.commit(t, 1)
	earlier, _ := p.createArchive(t)
	h := p.commit(t, 2)
	archive, _ := p.createArchive(t)
	p.closeStore(t)

	homes := map[string]func(t *testing.T) target{
		"a fresh home": freshHome,
		"a home with a restore that has not seeded": func(t *testing.T) target {
			tg := freshHome(t)
			require.NoError(t, runRestore(t.Context(), tg, earlier, nil, &bytes.Buffer{}))
			return tg
		},
	}
	steps := []string{
		restoreStepChecked, restoreStepExtracted, restoreStepMarked, restoreStepFlatKVCleared,
		restoreStepEpochsInstalled, restoreStepInstalled,
	}
	for homeName, newHome := range homes {
		clean := newHome(t)
		require.NoError(t, runRestore(t.Context(), clean, archive, nil, &bytes.Buffer{}))
		want := treeOf(t, clean.layout.PersistentStateDir)

		for _, step := range steps {
			for _, kind := range []stopKind{stopWithError, stopWithCrash} {
				t.Run(fmt.Sprintf("%s/%s/%s", homeName, step, kind), func(t *testing.T) {
					tg := newHome(t)
					restore := func(stop stepHook) error {
						return restoreWith(t.Context(), tg, archive, nil, io.Discard, stop)
					}
					spec := childSpec{Op: "restore", PSD: tg.layout.PersistentStateDir, From: archive}
					if !stopOnce(t, kind, step, restore, spec) {
						t.Skipf("restore does not reach %q on this home", step)
					}
					if step != restoreStepChecked && step != restoreStepExtracted {
						marker, err := os.ReadFile(tg.layout.RestoreMarkerFile)
						require.NoError(t, err)
						require.Equal(t, strconv.FormatInt(h, 10)+"\n", string(marker))
					}
					rerunUntilSuccess(t, func() error { return restore(noStop) })
					require.Equal(t, want, treeOf(t, tg.layout.PersistentStateDir))
				})
			}
		}
	}
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
