package cmd

import (
	"bytes"
	"context"
	"log/slog"
	"net"
	"testing"
	"time"

	"github.com/sei-protocol/sei-chain/admin"
	"github.com/sei-protocol/seilog"
	"github.com/stretchr/testify/require"
)

func TestLogLevelCmd_Structure(t *testing.T) {
	cmd := LogLevelCmd()
	require.Equal(t, "log", cmd.Use)
	require.NotEmpty(t, cmd.Short)

	levelCmd, _, err := cmd.Find([]string{"level"})
	require.NoError(t, err)
	require.Equal(t, "level", levelCmd.Use)

	setCmd, _, err := levelCmd.Find([]string{"set"})
	require.NoError(t, err)
	require.Contains(t, setCmd.Use, "set")

	getCmd, _, err := levelCmd.Find([]string{"get"})
	require.NoError(t, err)
	require.Contains(t, getCmd.Use, "get")

	listCmd, _, err := levelCmd.Find([]string{"list"})
	require.NoError(t, err)
	require.Contains(t, listCmd.Use, "list")
}

func TestLogLevelCmd_AdminAddrFlag(t *testing.T) {
	cmd := LogLevelCmd()
	levelCmd, _, err := cmd.Find([]string{"level"})
	require.NoError(t, err)

	flag := levelCmd.PersistentFlags().Lookup("admin-addr")
	require.NotNil(t, flag, "--admin-addr flag should exist")
	require.Equal(t, admin.DefaultAddress, flag.DefValue)
}

func TestLogLevelSetCmd_RequiresExactly2Args(t *testing.T) {
	cmd := LogLevelCmd()
	cmd.SetArgs([]string{"level", "set"})
	require.Error(t, cmd.Execute(), "set with 0 args should fail")

	cmd = LogLevelCmd()
	cmd.SetArgs([]string{"level", "set", "only-one"})
	require.Error(t, cmd.Execute(), "set with 1 arg should fail")

	cmd = LogLevelCmd()
	cmd.SetArgs([]string{"level", "set", "a", "b", "c"})
	require.Error(t, cmd.Execute(), "set with 3 args should fail")
}

func TestLogLevelGetCmd_RequiresExactly1Arg(t *testing.T) {
	cmd := LogLevelCmd()
	cmd.SetArgs([]string{"level", "get"})
	require.Error(t, cmd.Execute(), "get with 0 args should fail")

	cmd = LogLevelCmd()
	cmd.SetArgs([]string{"level", "get", "a", "b"})
	require.Error(t, cmd.Execute(), "get with 2 args should fail")
}

func TestLogLevelListCmd_AcceptsZeroOrOneArgs(t *testing.T) {
	cmd := LogLevelCmd()
	cmd.SetArgs([]string{"level", "list", "a", "b"})
	require.Error(t, cmd.Execute(), "list with 2 args should fail")
}

func startTestAdminServer(t *testing.T) (string, func()) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	addr := ln.Addr().String()
	require.NoError(t, ln.Close())

	srv, err := admin.StartServer(addr)
	require.NoError(t, err)
	return addr, func() { srv.GracefulStop() }
}

func TestLogLevelSet_Integration(t *testing.T) {
	_ = seilog.NewLogger("cli-set-test")
	addr, cleanup := startTestAdminServer(t)
	defer cleanup()

	var buf bytes.Buffer
	cmd := LogLevelCmd()
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"level", "set", "--admin-addr", addr, "cli-set-test", "error"})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	require.NoError(t, cmd.ExecuteContext(ctx))

	output := buf.String()
	require.Contains(t, output, "1 logger(s)")
	require.Contains(t, output, "cli-set-test")
	require.Contains(t, output, "error")

	lvl, ok := seilog.GetLevel("cli-set-test")
	require.True(t, ok)
	require.Equal(t, slog.LevelError, lvl)
}

func TestLogLevelGet_Integration(t *testing.T) {
	_ = seilog.NewLogger("cli-get-test")
	seilog.SetLevel("cli-get-test", slog.LevelWarn)
	addr, cleanup := startTestAdminServer(t)
	defer cleanup()

	var buf bytes.Buffer
	cmd := LogLevelCmd()
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"level", "get", "--admin-addr", addr, "cli-get-test"})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	require.NoError(t, cmd.ExecuteContext(ctx))

	output := buf.String()
	require.Contains(t, output, "cli-get-test")
	require.Contains(t, output, "warn")
}

func TestLogLevelList_Integration(t *testing.T) {
	_ = seilog.NewLogger("cli-list-test")
	addr, cleanup := startTestAdminServer(t)
	defer cleanup()

	var buf bytes.Buffer
	cmd := LogLevelCmd()
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"level", "list", "--admin-addr", addr})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	require.NoError(t, cmd.ExecuteContext(ctx))

	output := buf.String()
	require.Contains(t, output, "cli-list-test")
}

func TestLogLevelList_WithPrefix_Integration(t *testing.T) {
	_ = seilog.NewLogger("cli-pfx", "child1")
	_ = seilog.NewLogger("cli-pfx", "child2")
	_ = seilog.NewLogger("cli-other-logger")
	addr, cleanup := startTestAdminServer(t)
	defer cleanup()

	var buf bytes.Buffer
	cmd := LogLevelCmd()
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"level", "list", "--admin-addr", addr, "cli-pfx"})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	require.NoError(t, cmd.ExecuteContext(ctx))

	output := buf.String()
	require.Contains(t, output, "cli-pfx/child1")
	require.Contains(t, output, "cli-pfx/child2")
	require.NotContains(t, output, "cli-other-logger")
}

func TestLogLevelSet_ThenGet_Integration(t *testing.T) {
	_ = seilog.NewLogger("cli-roundtrip")
	addr, cleanup := startTestAdminServer(t)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	setCmd := LogLevelCmd()
	setCmd.SetArgs([]string{"level", "set", "--admin-addr", addr, "cli-roundtrip", "debug"})
	require.NoError(t, setCmd.ExecuteContext(ctx))

	var buf bytes.Buffer
	getCmd := LogLevelCmd()
	getCmd.SetOut(&buf)
	getCmd.SetErr(&buf)
	getCmd.SetArgs([]string{"level", "get", "--admin-addr", addr, "cli-roundtrip"})
	require.NoError(t, getCmd.ExecuteContext(ctx))

	require.Contains(t, buf.String(), "debug")
}

func TestLogLevelSet_InvalidServer_ReturnsError(t *testing.T) {
	cmd := LogLevelCmd()
	cmd.SetArgs([]string{"level", "set", "--admin-addr", "127.0.0.1:1", "*", "info"})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	require.Error(t, cmd.ExecuteContext(ctx))
}

func TestLogLevelGet_NonexistentLogger_ReturnsError(t *testing.T) {
	addr, cleanup := startTestAdminServer(t)
	defer cleanup()

	cmd := LogLevelCmd()
	cmd.SetArgs([]string{"level", "get", "--admin-addr", addr, "does-not-exist-xyz"})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	require.Error(t, cmd.ExecuteContext(ctx))
}

func TestLogLevelSet_InvalidLevel_ReturnsError(t *testing.T) {
	addr, cleanup := startTestAdminServer(t)
	defer cleanup()

	cmd := LogLevelCmd()
	cmd.SetArgs([]string{"level", "set", "--admin-addr", addr, "*", "bogus"})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	require.Error(t, cmd.ExecuteContext(ctx))
}
