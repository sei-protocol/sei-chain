package log

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestTMFmtHandler_BasicOutput(t *testing.T) {
	var buf bytes.Buffer
	h := NewTMFmtHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	logger := slog.New(h)

	logger.Info("Stopping AddrBook", "module", "main", "reason", "already stopped")

	out := buf.String()
	t.Logf("output: %s", out)

	require.True(t, strings.HasPrefix(out, "I["), "expected line to start with 'I['")
	require.Contains(t, out, "] Stopping AddrBook")
	require.Contains(t, out, "module=main")
	require.Contains(t, out, `reason="already stopped"`)
}

func TestTMFmtHandler_LevelChars(t *testing.T) {
	tests := []struct {
		level slog.Level
		char  string
	}{
		{slog.LevelDebug, "D["},
		{slog.LevelInfo, "I["},
		{slog.LevelWarn, "W["},
		{slog.LevelError, "E["},
	}

	for _, tt := range tests {
		var buf bytes.Buffer
		h := NewTMFmtHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})
		logger := slog.New(h)

		logger.Log(nil, tt.level, "test message")

		require.True(t, strings.HasPrefix(buf.String(), tt.char),
			"level %v: expected prefix %q, got: %s", tt.level, tt.char, buf.String())
	}
}

func TestTMFmtHandler_ByteSliceHex(t *testing.T) {
	var buf bytes.Buffer
	h := NewTMFmtHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	logger := slog.New(h)

	logger.Info("test", "hash", slog.AnyValue([]byte{0xde, 0xad, 0xbe, 0xef}))

	require.Contains(t, buf.String(), "DEADBEEF")
}

func TestTMFmtHandler_WithAttrs(t *testing.T) {
	var buf bytes.Buffer
	h := NewTMFmtHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	logger := slog.New(h).With("module", "consensus", "height", 100)

	logger.Info("new round")

	out := buf.String()
	require.Contains(t, out, "module=consensus")
	require.Contains(t, out, "height=100")
}

func TestTMFmtHandler_WithGroup(t *testing.T) {
	var buf bytes.Buffer
	h := NewTMFmtHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	logger := slog.New(h).WithGroup("peer").With("id", "abc123")

	logger.Info("connected")

	require.Contains(t, buf.String(), "peer.id=abc123")
}

func TestTMFmtHandler_Enabled(t *testing.T) {
	h := NewTMFmtHandler(&bytes.Buffer{}, &slog.HandlerOptions{Level: slog.LevelWarn})

	require.False(t, h.Enabled(nil, slog.LevelInfo), "Info should be disabled when level is Warn")
	require.True(t, h.Enabled(nil, slog.LevelWarn), "Warn should be enabled")
	require.True(t, h.Enabled(nil, slog.LevelError), "Error should be enabled")
}

func TestTMFmtHandler_TimeFormat(t *testing.T) {
	var buf bytes.Buffer
	h := NewTMFmtHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	logger := slog.New(h)

	logger.Info("test")

	// Match at least up to the minute.
	now := time.Now().Format("2006-01-02|15:04:05")
	require.Contains(t, buf.String(), now[:16])
}

func TestTMFmtHandler_ModuleNotDuplicatedInKV(t *testing.T) {
	var buf bytes.Buffer
	h := NewTMFmtHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	logger := slog.New(h)

	logger.Info("test", "module", "p2p", "peer", "abc")

	out := buf.String()
	count := strings.Count(out, "module=")
	require.Equal(t, 1, count, "module= should appear exactly once: %s", out)
}

func TestTMFmtHandler_ModuleBeforeOtherKVs(t *testing.T) {
	var buf bytes.Buffer
	h := NewTMFmtHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	logger := slog.New(h)

	logger.Info("test", "aaa", "first", "module", "p2p", "zzz", "last")

	out := buf.String()
	modIdx := strings.Index(out, "module=")
	aaaIdx := strings.Index(out, "aaa=")
	zzzIdx := strings.Index(out, "zzz=")

	require.Greater(t, modIdx, -1, "module= not found")
	require.Less(t, modIdx, aaaIdx, "module should come before aaa")
	require.Less(t, modIdx, zzzIdx, "module should come before zzz")
}

func TestTMFmtHandler_QuotingValues(t *testing.T) {
	var buf bytes.Buffer
	h := NewTMFmtHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	logger := slog.New(h)

	logger.Info("test", "key", "has space", "simple", "nospace")

	out := buf.String()
	require.Contains(t, out, `key="has space"`)
	require.Contains(t, out, "simple=nospace")
}

func TestTMFmtHandler_EmptyValue(t *testing.T) {
	var buf bytes.Buffer
	h := NewTMFmtHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	logger := slog.New(h)

	logger.Info("test", "key", "")

	require.Contains(t, buf.String(), `key=""`)
}
