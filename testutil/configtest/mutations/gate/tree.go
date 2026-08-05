package gate

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// maxCapturedOutput bounds what a single test run may hand back.
//
// The only consumers are one substring search and a three-line diagnostic, so a run that produces
// megabytes — a fuzz corpus dump, an accidental -v, a panic loop — would be buffered in full for no
// reader. The tail is kept rather than the head because the failure lines a row is attributed by come
// last.
const maxCapturedOutput = 1 << 20

// WorktreeTree runs the gate's mutations inside a throwaway git worktree.
//
// A separate worktree rather than the caller's checkout, for two reasons that are not convenience.
// Reverting with git means an interrupted run — a signal, a panic, a killed process — can leave a
// patch applied, and doing that to someone's working copy is a data-loss bug in a tool whose only job
// is measurement. And a gate that measures the tree as currently edited answers a question nobody
// asked: whether the suite catches a mutation is a property of the committed state.
//
// The cost is one checkout of the worktree at startup. The object database, module cache and build
// cache are all shared with the caller's checkout, so compilation stays warm.
type WorktreeTree struct {
	// dir is the worktree's path, and the working directory of every command.
	dir string
	// patchSource is where patch files are read from: the caller's checkout, not the worktree, so the
	// patches under review are the ones on disk rather than the ones at HEAD.
	patchSource string
	// perCall bounds any single git or go invocation.
	perCall time.Duration
}

// NewWorktreeTree creates a worktree at HEAD and returns a Tree that operates inside it.
//
// The caller must Close it. A leaked worktree is recoverable with `git worktree prune`, so a crash
// costs disk rather than correctness.
func NewWorktreeTree(ctx context.Context, repoRoot, patchSource string, perCall time.Duration) (*WorktreeTree, error) {
	dir, err := os.MkdirTemp("", "config-mutation-gate-")
	if err != nil {
		return nil, fmt.Errorf("create the worktree directory: %w", err)
	}

	// --detach so the worktree points at HEAD's commit rather than claiming the branch, which would
	// stop the caller from switching branches while the gate runs.
	if out, err := runCommand(ctx, repoRoot, perCall, "git", "worktree", "add", "--detach", dir, "HEAD"); err != nil {
		_ = os.RemoveAll(dir)
		return nil, fmt.Errorf("git worktree add: %w\n%s", err, out)
	}
	return &WorktreeTree{dir: dir, patchSource: patchSource, perCall: perCall}, nil
}

// Dir is the worktree the gate is operating in, for a caller that wants to report it.
func (w *WorktreeTree) Dir() string { return w.dir }

// Close removes the worktree.
func (w *WorktreeTree) Close() error {
	// Not the caller's ctx: Close runs during teardown, including after a cancellation, and a removal
	// skipped because the context was already done is the leak this is meant to prevent.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(context.Background()), w.perCall)
	defer cancel()

	if out, err := runCommand(ctx, w.dir, w.perCall, "git", "worktree", "remove", "--force", w.dir); err != nil {
		return fmt.Errorf("git worktree remove: %w\n%s", err, out)
	}
	return nil
}

// RunTests runs go test with the given arguments inside the worktree.
//
// args is the whole argument list rather than a package list, so the count and shuffle flags a
// run-mode obligation needs are chosen by its caller. A Tree that appended its own -count would have
// to decide what to do when the caller supplied one too, and every answer to that is a conditional
// standing between the caller's intent and what ran.
func (w *WorktreeTree) RunTests(ctx context.Context, args []string) (string, bool, error) {
	output, err := runCommand(ctx, w.dir, w.perCall, "go", append([]string{"test"}, args...)...)
	if err == nil {
		return output, true, nil
	}
	// A non-zero exit is the observation, not an error. Anything else — the binary missing, a timeout,
	// a cancelled context — is a failure to observe, and the two must not be reported alike.
	var exit *exec.ExitError
	if asExitError(err, &exit) {
		return output, false, nil
	}
	return output, false, fmt.Errorf("go test %s: %w\n%s", strings.Join(args, " "), err, output)
}

// Apply applies a patch from the caller's checkout into the worktree.
func (w *WorktreeTree) Apply(ctx context.Context, patchPath string) error {
	absolute := patchPath
	if !filepath.IsAbs(absolute) {
		absolute = filepath.Join(w.patchSource, filepath.Base(patchPath))
	}
	if out, err := runCommand(ctx, w.dir, w.perCall, "git", "apply", absolute); err != nil {
		return fmt.Errorf("%w\n%s", err, out)
	}
	return nil
}

// Reset returns the worktree to HEAD, discarding modifications and additions alike.
//
// Both commands are needed. `git checkout -- .` and `git reset --hard` restore tracked files but leave
// a file the patch created, so a patch with a new-file hunk would survive the revert and be observed
// as part of the next row. `clean -fd` removes those. Deliberately not -fdx: -x would also delete
// ignored build output, which is the cache that keeps the run warm.
func (w *WorktreeTree) Reset(ctx context.Context) error {
	if out, err := runCommand(ctx, w.dir, w.perCall, "git", "reset", "--hard"); err != nil {
		return fmt.Errorf("git reset --hard: %w\n%s", err, out)
	}
	if out, err := runCommand(ctx, w.dir, w.perCall, "git", "clean", "-fd"); err != nil {
		return fmt.Errorf("git clean -fd: %w\n%s", err, out)
	}
	return nil
}

// Dirty reports uncommitted changes in the worktree.
func (w *WorktreeTree) Dirty(ctx context.Context) (string, error) {
	out, err := runCommand(ctx, w.dir, w.perCall, "git", "status", "--porcelain", "--untracked-files=all")
	if err != nil {
		return "", fmt.Errorf("git status: %w\n%s", err, out)
	}
	return strings.TrimSpace(out), nil
}

// runCommand runs one command in dir with a deadline, returning stdout and stderr combined.
//
// Three things here are the difference between a tool that can hang a CI job and one that cannot.
//
// The deadline: go test with no timeout of its own can wait forever on a deadlocked test, and the
// predecessor to this package had no deadline anywhere — a hung row hung the whole gate, with the CI
// job's own limit as the only backstop and no diagnostic.
//
// The process group: go test compiles and runs child test binaries, so killing the go process leaves
// the grandchildren holding whatever they hold. Setpgid puts them in one group and Cancel signals the
// group, so a timeout ends the whole tree of processes.
//
// WaitDelay: after the signal, a child that ignores it gets a bounded grace period and then the pipes
// are closed regardless, so a process that refuses to die cannot make Wait block forever.
func runCommand(ctx context.Context, dir string, timeout time.Duration, name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		// Negative pid signals the whole process group.
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
	cmd.WaitDelay = 10 * time.Second

	var sink boundedBuffer
	sink.limit = maxCapturedOutput
	cmd.Stdout = &sink
	cmd.Stderr = &sink

	err := cmd.Run()
	if ctxErr := ctx.Err(); ctxErr != nil {
		return sink.String(), fmt.Errorf("%s %s: %w after %s", name, strings.Join(args, " "), ctxErr, timeout)
	}
	return sink.String(), err
}

// boundedBuffer keeps the last limit bytes written to it.
type boundedBuffer struct {
	limit    int
	buf      []byte
	overflow bool
}

func (b *boundedBuffer) Write(p []byte) (int, error) {
	b.buf = append(b.buf, p...)
	if len(b.buf) > b.limit {
		b.buf = b.buf[len(b.buf)-b.limit:]
		b.overflow = true
	}
	return len(p), nil
}

func (b *boundedBuffer) String() string {
	if b.overflow {
		return "[earlier output discarded]\n" + string(b.buf)
	}
	return string(b.buf)
}

// asExitError reports whether err is or wraps an *exec.ExitError, assigning it when so.
//
// A tiny wrapper so RunTests reads as one decision rather than an errors.As call spliced into a
// return statement.
func asExitError(err error, target **exec.ExitError) bool {
	for err != nil {
		if exit, ok := err.(*exec.ExitError); ok { //nolint:errorlint // walking the chain by hand
			*target = exit
			return true
		}
		unwrapped, ok := err.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		err = unwrapped.Unwrap()
	}
	return false
}
