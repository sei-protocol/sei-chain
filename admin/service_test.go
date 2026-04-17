package admin

import (
	"context"
	"log/slog"
	"testing"

	"github.com/sei-protocol/sei-chain/admin/types"
	"github.com/sei-protocol/seilog"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func newService() *service {
	return &service{}
}

func grpcCode(err error) codes.Code {
	st, _ := status.FromError(err)
	return st.Code()
}

// --------------------------------------------------------------------------
// SetLogLevel
// --------------------------------------------------------------------------

func TestSetLogLevel_EmptyPattern(t *testing.T) {
	svc := newService()
	_, err := svc.SetLogLevel(context.Background(), &types.SetLogLevelRequest{
		Pattern: "",
		Level:   "info",
	})
	require.Error(t, err)
	require.Equal(t, codes.InvalidArgument, grpcCode(err))
}

func TestSetLogLevel_InvalidLevel(t *testing.T) {
	svc := newService()
	_, err := svc.SetLogLevel(context.Background(), &types.SetLogLevelRequest{
		Pattern: "*",
		Level:   "not-a-level",
	})
	require.Error(t, err)
	require.Equal(t, codes.InvalidArgument, grpcCode(err))
}

func TestSetLogLevel_StarPattern(t *testing.T) {
	_ = seilog.NewLogger("svc-star-a")
	_ = seilog.NewLogger("svc-star-b")
	defer seilog.SetLevel("*", slog.LevelInfo)

	svc := newService()
	resp, err := svc.SetLogLevel(context.Background(), &types.SetLogLevelRequest{
		Pattern: "*",
		Level:   "warn",
	})
	require.NoError(t, err)
	require.Equal(t, "*", resp.Pattern)
	require.Equal(t, "warn", resp.Level)
	require.GreaterOrEqual(t, resp.Affected, int32(2))
}

func TestSetLogLevel_ExactPattern(t *testing.T) {
	_ = seilog.NewLogger("svc-exact")

	svc := newService()
	resp, err := svc.SetLogLevel(context.Background(), &types.SetLogLevelRequest{
		Pattern: "svc-exact",
		Level:   "debug",
	})
	require.NoError(t, err)
	require.Equal(t, int32(1), resp.Affected)

	lvl, ok := seilog.GetLevel("svc-exact")
	require.True(t, ok)
	require.Equal(t, slog.LevelDebug, lvl)
}

func TestSetLogLevel_NoMatchReturnsNotFound(t *testing.T) {
	svc := newService()
	_, err := svc.SetLogLevel(context.Background(), &types.SetLogLevelRequest{
		Pattern: "nonexistent-logger-xyz",
		Level:   "info",
	})
	require.Error(t, err)
	require.Equal(t, codes.NotFound, grpcCode(err))
}

func TestSetLogLevel_AllValidLevels(t *testing.T) {
	_ = seilog.NewLogger("svc-levels")
	svc := newService()

	for _, level := range []string{"debug", "info", "warn", "error", "DEBUG", "INFO", "WARN", "ERROR"} {
		t.Run(level, func(t *testing.T) {
			resp, err := svc.SetLogLevel(context.Background(), &types.SetLogLevelRequest{
				Pattern: "svc-levels",
				Level:   level,
			})
			require.NoError(t, err)
			require.Equal(t, int32(1), resp.Affected)
		})
	}
}

func TestSetLogLevel_GlobPattern(t *testing.T) {
	_ = seilog.NewLogger("svc-glob", "child1")
	_ = seilog.NewLogger("svc-glob", "child2")
	_ = seilog.NewLogger("svc-glob", "child1", "grandchild")
	defer seilog.SetLevel("svc-glob/**", slog.LevelInfo)

	svc := newService()
	resp, err := svc.SetLogLevel(context.Background(), &types.SetLogLevelRequest{
		Pattern: "svc-glob/*",
		Level:   "error",
	})
	require.NoError(t, err)
	require.Equal(t, int32(2), resp.Affected, "glob should match only direct children")
}

func TestSetLogLevel_StarSetsDefaultLevel(t *testing.T) {
	_ = seilog.NewLogger("svc-star-def-a")
	_ = seilog.NewLogger("svc-star-def-b")
	defer seilog.SetDefaultLevel(slog.LevelInfo, true)

	svc := newService()
	resp, err := svc.SetLogLevel(context.Background(), &types.SetLogLevelRequest{
		Pattern: "*",
		Level:   "error",
	})
	require.NoError(t, err)
	require.Equal(t, int32(len(seilog.ListLoggers())), resp.Affected)
}

// --------------------------------------------------------------------------
// GetLogLevel
// --------------------------------------------------------------------------

func TestGetLogLevel_EmptyLogger(t *testing.T) {
	svc := newService()
	_, err := svc.GetLogLevel(context.Background(), &types.GetLogLevelRequest{
		Logger: "",
	})
	require.Error(t, err)
	require.Equal(t, codes.InvalidArgument, grpcCode(err))
}

func TestGetLogLevel_NotFound(t *testing.T) {
	svc := newService()
	_, err := svc.GetLogLevel(context.Background(), &types.GetLogLevelRequest{
		Logger: "nonexistent-logger-abc",
	})
	require.Error(t, err)
	require.Equal(t, codes.NotFound, grpcCode(err))
}

func TestGetLogLevel_ReturnsCorrectLevel(t *testing.T) {
	_ = seilog.NewLogger("svc-get")
	seilog.SetLevel("svc-get", slog.LevelWarn)

	svc := newService()
	resp, err := svc.GetLogLevel(context.Background(), &types.GetLogLevelRequest{
		Logger: "svc-get",
	})
	require.NoError(t, err)
	require.Equal(t, "svc-get", resp.Logger)
	require.Equal(t, "warn", resp.Level)
}

func TestGetLogLevel_LevelIsLowercase(t *testing.T) {
	_ = seilog.NewLogger("svc-get-case")

	svc := newService()
	for _, lvl := range []slog.Level{slog.LevelDebug, slog.LevelInfo, slog.LevelWarn, slog.LevelError} {
		seilog.SetLevel("svc-get-case", lvl)
		resp, err := svc.GetLogLevel(context.Background(), &types.GetLogLevelRequest{
			Logger: "svc-get-case",
		})
		require.NoError(t, err)
		require.Equal(t, resp.Level, lowercaseOf(resp.Level), "level should be lowercase")
	}
}

func lowercaseOf(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'A' && c <= 'Z' {
			b[i] = c + 32
		}
	}
	return string(b)
}

func TestGetLogLevel_ReflectsRuntimeChange(t *testing.T) {
	_ = seilog.NewLogger("svc-get-runtime")
	svc := newService()

	seilog.SetLevel("svc-get-runtime", slog.LevelDebug)
	resp, err := svc.GetLogLevel(context.Background(), &types.GetLogLevelRequest{
		Logger: "svc-get-runtime",
	})
	require.NoError(t, err)
	require.Equal(t, "debug", resp.Level)

	seilog.SetLevel("svc-get-runtime", slog.LevelError)
	resp, err = svc.GetLogLevel(context.Background(), &types.GetLogLevelRequest{
		Logger: "svc-get-runtime",
	})
	require.NoError(t, err)
	require.Equal(t, "error", resp.Level)
}

// --------------------------------------------------------------------------
// ListLoggers
// --------------------------------------------------------------------------

func TestListLoggers_NoPrefix(t *testing.T) {
	_ = seilog.NewLogger("svc-list-a")
	_ = seilog.NewLogger("svc-list-b")

	svc := newService()
	resp, err := svc.ListLoggers(context.Background(), &types.ListLoggersRequest{})
	require.NoError(t, err)

	names := make(map[string]bool)
	for _, l := range resp.Loggers {
		names[l.Name] = true
	}
	require.True(t, names["svc-list-a"], "missing svc-list-a")
	require.True(t, names["svc-list-b"], "missing svc-list-b")
}

func TestListLoggers_WithPrefix(t *testing.T) {
	_ = seilog.NewLogger("svc-pfx", "child1")
	_ = seilog.NewLogger("svc-pfx", "child2")
	_ = seilog.NewLogger("svc-other")

	svc := newService()
	resp, err := svc.ListLoggers(context.Background(), &types.ListLoggersRequest{
		Prefix: "svc-pfx",
	})
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(resp.Loggers), 2)

	for _, l := range resp.Loggers {
		require.Contains(t, l.Name, "svc-pfx", "all results should match prefix")
	}
}

func TestListLoggers_PrefixFiltersOut(t *testing.T) {
	_ = seilog.NewLogger("svc-keep")
	_ = seilog.NewLogger("svc-drop")

	svc := newService()
	resp, err := svc.ListLoggers(context.Background(), &types.ListLoggersRequest{
		Prefix: "svc-keep",
	})
	require.NoError(t, err)

	for _, l := range resp.Loggers {
		require.NotEqual(t, "svc-drop", l.Name, "svc-drop should be filtered out")
	}
}

func TestListLoggers_ResultsSorted(t *testing.T) {
	_ = seilog.NewLogger("svc-sort-c")
	_ = seilog.NewLogger("svc-sort-a")
	_ = seilog.NewLogger("svc-sort-b")

	svc := newService()
	resp, err := svc.ListLoggers(context.Background(), &types.ListLoggersRequest{
		Prefix: "svc-sort-",
	})
	require.NoError(t, err)
	require.Len(t, resp.Loggers, 3)

	for i := 1; i < len(resp.Loggers); i++ {
		require.LessOrEqual(t, resp.Loggers[i-1].Name, resp.Loggers[i].Name,
			"loggers should be sorted")
	}
}

func TestListLoggers_IncludesLevel(t *testing.T) {
	_ = seilog.NewLogger("svc-lvl-check")
	seilog.SetLevel("svc-lvl-check", slog.LevelError)

	svc := newService()
	resp, err := svc.ListLoggers(context.Background(), &types.ListLoggersRequest{
		Prefix: "svc-lvl-check",
	})
	require.NoError(t, err)

	var found bool
	for _, l := range resp.Loggers {
		if l.Name == "svc-lvl-check" {
			found = true
			require.Equal(t, "error", l.Level)
		}
	}
	require.True(t, found, "svc-lvl-check should be in the response")
}

func TestListLoggers_EmptyPrefix(t *testing.T) {
	svc := newService()
	resp, err := svc.ListLoggers(context.Background(), &types.ListLoggersRequest{})
	require.NoError(t, err)
	require.NotEmpty(t, resp.Loggers)
}

// --------------------------------------------------------------------------
// Integration: round-trips
// --------------------------------------------------------------------------

func TestSetThenGet_RoundTrip(t *testing.T) {
	_ = seilog.NewLogger("svc-roundtrip")
	svc := newService()

	_, err := svc.SetLogLevel(context.Background(), &types.SetLogLevelRequest{
		Pattern: "svc-roundtrip",
		Level:   "error",
	})
	require.NoError(t, err)

	resp, err := svc.GetLogLevel(context.Background(), &types.GetLogLevelRequest{
		Logger: "svc-roundtrip",
	})
	require.NoError(t, err)
	require.Equal(t, "error", resp.Level)
}

func TestSetThenList_Visible(t *testing.T) {
	_ = seilog.NewLogger("svc-setlist")
	svc := newService()

	_, err := svc.SetLogLevel(context.Background(), &types.SetLogLevelRequest{
		Pattern: "svc-setlist",
		Level:   "debug",
	})
	require.NoError(t, err)

	resp, err := svc.ListLoggers(context.Background(), &types.ListLoggersRequest{
		Prefix: "svc-setlist",
	})
	require.NoError(t, err)

	var found bool
	for _, l := range resp.Loggers {
		if l.Name == "svc-setlist" {
			found = true
			require.Equal(t, "debug", l.Level)
		}
	}
	require.True(t, found, "svc-setlist should appear in ListLoggers")
}
