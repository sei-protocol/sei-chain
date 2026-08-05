// Command gate runs the configuration mutation gate.
//
// It decides, per named falsifier, whether the characterization suite would actually catch a change to
// production code. See the gate package's documentation for what that means and why reading the tests
// cannot answer it.
//
//	go run ./testutil/configtest/mutations/cmd/gate
//
// The gate's own logic is covered by ordinary unit tests in that package, which run in milliseconds and
// prove it can fail. Run those first — an instrument nobody has shown can fail cannot certify anything:
//
//	go test ./testutil/configtest/mutations/gate/
//
// Both steps together are `make mutation-gate`.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/sei-protocol/sei-chain/testutil/configtest/mutations/gate"
)

func main() {
	perCall := flag.Duration("timeout", 15*time.Minute,
		"deadline for any single git or go invocation; a deadlocked test would otherwise hang the run")
	flag.Parse()

	// Cancelled on SIGINT or SIGTERM, which reaches every subprocess through the process group and
	// lets the worktree teardown run. Without it an interrupt leaves orphaned test binaries and a
	// worktree behind.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	code, err := run(ctx, *perCall)
	if err != nil {
		fmt.Fprintf(os.Stderr, "\nGATE ABORTED: %v\n", err)
		code = gate.ExitAborted
	}
	os.Exit(code)
}

func run(ctx context.Context, perCall time.Duration) (int, error) {
	repoRoot, err := findRepoRoot(ctx)
	if err != nil {
		return gate.ExitAborted, err
	}
	mutations := filepath.Join(repoRoot, "testutil", "configtest", "mutations")

	rows, requirements, err := readInputs(mutations)
	if err != nil {
		return gate.ExitAborted, err
	}

	tree, err := gate.NewWorktreeTree(ctx, repoRoot, filepath.Join(mutations, "patches"), perCall)
	if err != nil {
		return gate.ExitAborted, err
	}
	defer closeTree(tree)

	fmt.Printf("observing %d row(s) in %s\n", len(rows), tree.Dir())
	noticeUncommittedWork(ctx, repoRoot, perCall)

	result := gate.Run(ctx, gate.Config{
		Tree:          tree,
		Rows:          rows,
		Requirements:  requirements,
		PatchDir:      filepath.Join(mutations, "patches"),
		CheckRunModes: true,
		Log:           func(line string) { fmt.Println(line) },
	})
	fmt.Print(result.Summary())
	// Returned rather than exited here, so the deferred worktree teardown runs.
	return result.ExitCode(), nil
}

// readInputs loads the two files that drive the run.
//
// Their absence is an abort rather than an empty run. The file that names every patch is the gate's
// primary input, and a gate with nothing to observe reporting success is the failure it exists to
// detect.
func readInputs(mutations string) ([]gate.Row, []string, error) {
	expectations := filepath.Join(mutations, "expectations.tsv")
	rows, err := readFile(expectations, gate.ParseExpectations)
	if err != nil {
		return nil, nil, fmt.Errorf("%s: %w\nThis file names every patch the gate observes, so its "+
			"absence is a broken gate rather than a gate with nothing to do", expectations, err)
	}
	if len(rows) == 0 {
		return nil, nil, fmt.Errorf("%s has no rows, so nothing would be observed", expectations)
	}

	requirementsPath := filepath.Join(mutations, "falsifier_requirements.txt")
	requirements, err := readFile(requirementsPath, gate.ParseRequirements)
	if err != nil {
		return nil, nil, fmt.Errorf("%s: %w", requirementsPath, err)
	}
	return rows, requirements, nil
}

func readFile[T any](path string, parse func(r io.Reader) ([]T, error)) ([]T, error) {
	f, err := os.Open(path) //nolint:gosec // a path composed from the repository root
	if err != nil {
		return nil, err
	}
	defer f.Close() //nolint:errcheck // read-only
	return parse(f)
}

func findRepoRoot(ctx context.Context) (string, error) {
	out, err := exec.CommandContext(ctx, "git", "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return "", fmt.Errorf("locate the repository root: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

// noticeUncommittedWork tells the reader their edits were not measured.
//
// A notice rather than a refusal. The gate observes a worktree at HEAD, so uncommitted work is neither
// touched nor tested, and refusing to run would only mean nobody can use the tool while editing —
// which is most of the time. What must not happen is a reader assuming their change was measured.
func noticeUncommittedWork(ctx context.Context, repoRoot string, perCall time.Duration) {
	cctx, cancel := context.WithTimeout(ctx, perCall)
	defer cancel()

	cmd := exec.CommandContext(cctx, "git", "status", "--porcelain", "--untracked-files=all")
	cmd.Dir = repoRoot
	out, err := cmd.Output()
	if err != nil || len(strings.TrimSpace(string(out))) == 0 {
		return
	}
	fmt.Printf("\nNOTE: your checkout has uncommitted changes. The gate observed HEAD, so those "+
		"changes were neither tested nor touched:\n%s\n", strings.TrimRight(string(out), "\n"))
}

func closeTree(tree *gate.WorktreeTree) {
	if err := tree.Close(); err != nil {
		fmt.Fprintf(os.Stderr, "could not remove the worktree at %s: %v\n"+
			"Recover the disk with `git worktree prune`.\n", tree.Dir(), err)
	}
}
