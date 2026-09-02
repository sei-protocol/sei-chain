package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
)

type commandSpec struct {
	dir  string
	env  []string
	name string
	args []string
}

type commandRunner interface {
	output(context.Context, commandSpec) (string, error)
	stream(context.Context, commandSpec) error
	lookPath(string) error
}

type execRunner struct {
	in     io.Reader
	stdout io.Writer
	stderr io.Writer
}

func newExecRunner(in io.Reader, stdout, stderr io.Writer) commandRunner {
	return &execRunner{in: in, stdout: stdout, stderr: stderr}
}

func (r *execRunner) output(ctx context.Context, spec commandSpec) (string, error) {
	cmd := exec.CommandContext(ctx, spec.name, spec.args...) //nolint:gosec // this is the command suite's validated process boundary.
	cmd.Dir = spec.dir
	cmd.Env = append(os.Environ(), spec.env...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	value, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("%s: %w: %s", commandString(spec), err, bytes.TrimSpace(stderr.Bytes()))
	}
	return string(value), nil
}

func (r *execRunner) stream(ctx context.Context, spec commandSpec) error {
	cmd := exec.CommandContext(ctx, spec.name, spec.args...) //nolint:gosec // this is the command suite's validated process boundary.
	cmd.Dir = spec.dir
	cmd.Env = append(os.Environ(), spec.env...)
	cmd.Stdin = r.in
	cmd.Stdout = r.stdout
	cmd.Stderr = r.stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s: %w", commandString(spec), err)
	}
	return nil
}

func (*execRunner) lookPath(name string) error {
	if _, err := exec.LookPath(name); err != nil {
		return fmt.Errorf("required command %q is unavailable: %w", name, err)
	}
	return nil
}

func commandString(spec commandSpec) string {
	return spec.name + " " + fmt.Sprint(spec.args)
}
