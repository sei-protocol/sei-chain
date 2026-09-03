package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
)

const (
	defaultClusterName = "autobahn-evmonly"
	targetLocal        = "local"
	targetAWS          = "aws"
)

type application struct {
	runner   commandRunner
	stdout   io.Writer
	stderr   io.Writer
	stateDir string
}

func newRootCommand(runner commandRunner, stdout, stderr io.Writer) *cobra.Command {
	app := &application{runner: runner, stdout: stdout, stderr: stderr}
	cmd := &cobra.Command{
		Use:           "autobahn-e2e",
		Short:         "Manage Autobahn EVM-only end-to-end test clusters",
		SilenceErrors: true,
		SilenceUsage:  true,
	}
	cmd.SetOut(stdout)
	cmd.SetErr(stderr)
	cmd.PersistentFlags().StringVar(&app.stateDir, "state-dir", defaultStateDir(), "cluster metadata directory")
	cmd.AddCommand(
		app.newDeployCommand(),
		app.newListCommand(),
		app.newForwardCommand(),
		app.newTeardownCommand(),
	)
	return cmd
}

func (a *application) store() stateStore {
	return newStateStore(a.stateDir)
}

func findRepoRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("get working directory: %w", err)
	}
	for {
		data, readErr := os.ReadFile(filepath.Join(dir, "go.mod")) //nolint:gosec // dir is each ancestor of the current working directory.
		if readErr == nil && strings.Contains(string(data), "module github.com/sei-protocol/sei-chain") {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("run autobahn-e2e from a sei-chain checkout")
		}
		dir = parent
	}
}

func findNode(nodes []node, selector string) (node, error) {
	normalized := strings.TrimPrefix(strings.TrimPrefix(selector, "sei-"), "node-")
	for _, candidate := range nodes {
		if selector == candidate.Name || selector == candidate.Container || normalized == fmt.Sprint(candidate.Index) {
			return candidate, nil
		}
	}
	return node{}, fmt.Errorf("node %q does not exist", selector)
}

func expandHome(path string) string {
	if path == "~" || strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err == nil {
			return filepath.Join(home, strings.TrimPrefix(path, "~/"))
		}
	}
	return path
}
