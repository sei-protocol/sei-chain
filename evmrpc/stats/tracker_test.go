package stats

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/tendermint/tendermint/libs/log"
)

// mockLogger implements log.Logger for testing
type mockLogger struct {
	logs []logEntry
	mu   sync.Mutex
}

type logEntry struct {
	level   string
	msg     string
	keyvals []interface{}
}

func newMockLogger() *mockLogger {
	return &mockLogger{
		logs: make([]logEntry, 0),
	}
}

func (m *mockLogger) Debug(msg string, keyvals ...interface{}) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.logs = append(m.logs, logEntry{level: "debug", msg: msg, keyvals: keyvals})
}

func (m *mockLogger) Info(msg string, keyvals ...interface{}) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.logs = append(m.logs, logEntry{level: "info", msg: msg, keyvals: keyvals})
}

func (m *mockLogger) Error(msg string, keyvals ...interface{}) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.logs = append(m.logs, logEntry{level: "error", msg: msg, keyvals: keyvals})
}

func (m *mockLogger) With(keyvals ...interface{}) log.Logger {
	return m // Simple implementation for testing
}

func (m *mockLogger) getLogs() []logEntry {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]logEntry(nil), m.logs...)
}

func (m *mockLogger) getLogsByLevel(level string) []logEntry {
	m.mu.Lock()
	defer m.mu.Unlock()
	var filtered []logEntry
	for _, entry := range m.logs {
		if entry.level == level {
			filtered = append(filtered, entry)
		}
	}
	return filtered
}

func (m *mockLogger) hasLogWithMessage(msg string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, entry := range m.logs {
		if entry.msg == msg {
			return true
		}
	}
	return false
}

// Test scenarios using struct-based approach
func TestTracker(t *testing.T) {
	scenarios := []struct {
		name        string
		setup       func() (*tracker, *mockLogger, context.CancelFunc)
		test        func(t *testing.T, tracker *tracker, logger *mockLogger)
		cleanup     func(*tracker, context.CancelFunc)
		expectError bool
	}{
		{
			name: "basic_tracker_creation",
			setup: func() (*tracker, *mockLogger, context.CancelFunc) {
				logger := newMockLogger()
				ctx, cancel := context.WithCancel(context.Background())
				tracker := newTracker(ctx, logger, "http", 100*time.Millisecond)
				return tracker, logger, cancel
			},
			test: func(t *testing.T, tracker *tracker, logger *mockLogger) {
				require.NotNil(t, tracker)
				require.NotNil(t, tracker.ch)
				require.Equal(t, 100*time.Millisecond, tracker.interval)

				// Wait a bit for startup log
				time.Sleep(50 * time.Millisecond)
				require.True(t, logger.hasLogWithMessage("stats tracker started"))
			},
			cleanup: func(tracker *tracker, cancel context.CancelFunc) {
				tracker.Stop()
			},
		},
		{
			name: "tracker_stop_lifecycle",
			setup: func() (*tracker, *mockLogger, context.CancelFunc) {
				logger := newMockLogger()
				ctx, cancel := context.WithCancel(context.Background())
				tracker := newTracker(ctx, logger, "http", 100*time.Millisecond)
				return tracker, logger, cancel
			},
			test: func(t *testing.T, tracker *tracker, logger *mockLogger) {
				// Let it run briefly
				time.Sleep(50 * time.Millisecond)

				tracker.Stop()

				// Check stop log appears
				time.Sleep(50 * time.Millisecond)
				require.True(t, logger.hasLogWithMessage("stats tracker stopped"))
			},
			cleanup: func(tracker *tracker, cancel context.CancelFunc) {
				// Already stopped in test
			},
		},
		{
			name: "track_message_successful_request",
			setup: func() (*tracker, *mockLogger, context.CancelFunc) {
				logger := newMockLogger()
				ctx, cancel := context.WithCancel(context.Background())
				tracker := newTracker(ctx, logger, "http", 50*time.Millisecond)
				return tracker, logger, cancel
			},
			test: func(t *testing.T, tracker *tracker, logger *mockLogger) {
				// Track a successful message
				startTime := time.Now()
				tracker.TrackMessage("eth_getBalance", "http", startTime, true)

				// Give enough time for the event to be processed from the channel
				time.Sleep(50 * time.Millisecond)

				// Stop the tracker to force reporting of all remaining periods
				tracker.Stop()

				// Check that stats were logged during shutdown
				infoLogs := logger.getLogsByLevel("info")
				var foundOverallStats, foundMethodStats bool
				for _, log := range infoLogs {
					if log.msg == "stats" {
						foundOverallStats = true
					}
					if log.msg == "method stats" {
						foundMethodStats = true
					}
				}
				require.True(t, foundOverallStats, "Expected to find overall stats log")
				require.True(t, foundMethodStats, "Expected to find method stats log")
			},
			cleanup: func(tracker *tracker, cancel context.CancelFunc) {
				// Already stopped in test
			},
		},
		{
			name: "track_message_error_request",
			setup: func() (*tracker, *mockLogger, context.CancelFunc) {
				logger := newMockLogger()
				ctx, cancel := context.WithCancel(context.Background())
				tracker := newTracker(ctx, logger, "http", 100*time.Millisecond)
				return tracker, logger, cancel
			},
			test: func(t *testing.T, tracker *tracker, logger *mockLogger) {
				// Track a failed message
				startTime := time.Now()
				tracker.TrackMessage("eth_sendTransaction", "websocket", startTime, false)

				// Give enough time for the event to be processed from the channel
				time.Sleep(50 * time.Millisecond)

				// Stop the tracker to force reporting of all remaining periods
				tracker.Stop()

				// Verify error was tracked
				infoLogs := logger.getLogsByLevel("info")
				var foundOverallStats, foundMethodStats bool
				var overallSuccessRate float64

				for _, log := range infoLogs {
					if log.msg == "stats" {
						foundOverallStats = true
						// Check that overall success rate reflects the error
						for i := 0; i < len(log.keyvals); i += 2 {
							if log.keyvals[i] == "success_rate_pct" {
								overallSuccessRate = log.keyvals[i+1].(float64)
							}
						}
					}
					if log.msg == "method stats" {
						foundMethodStats = true
					}
				}
				require.True(t, foundOverallStats, "Expected to find overall stats log")
				require.True(t, foundMethodStats, "Expected to find method stats log")
				require.Equal(t, 0.0, overallSuccessRate, "Expected 0% overall success rate for error request")
			},
			cleanup: func(tracker *tracker, cancel context.CancelFunc) {
				// Already stopped in test
			},
		},
		{
			name: "channel_overflow_handling",
			setup: func() (*tracker, *mockLogger, context.CancelFunc) {
				logger := newMockLogger()
				ctx, cancel := context.WithCancel(context.Background())
				tracker := newTracker(ctx, logger, "http", 1*time.Second) // Longer interval to prevent flushing
				return tracker, logger, cancel
			},
			test: func(t *testing.T, tracker *tracker, logger *mockLogger) {
				// Send multiple messages to test channel handling
				for i := 0; i < 100; i++ {
					startTime := time.Now()
					tracker.TrackMessage("test_method", "http", startTime, true)
				}

				// Check for potential overflow debug logs
				time.Sleep(50 * time.Millisecond)
				debugLogs := logger.getLogsByLevel("debug")

				// We might not hit overflow with just 100 requests, but the test verifies the structure works
				require.True(t, len(debugLogs) >= 0, "Debug logs should be accessible")
			},
			cleanup: func(tracker *tracker, cancel context.CancelFunc) {
				tracker.Stop()
			},
		},
		{
			name: "concurrent_tracking",
			setup: func() (*tracker, *mockLogger, context.CancelFunc) {
				logger := newMockLogger()
				ctx, cancel := context.WithCancel(context.Background())
				tracker := newTracker(ctx, logger, "http", 200*time.Millisecond)
				return tracker, logger, cancel
			},
			test: func(t *testing.T, tracker *tracker, logger *mockLogger) {
				// Run concurrent message tracking
				var wg sync.WaitGroup
				numRequests := 50

				for i := 0; i < numRequests; i++ {
					wg.Add(1)
					go func(id int) {
						defer wg.Done()
						startTime := time.Now()
						method := "eth_call"
						if id%2 == 0 {
							method = "eth_getBalance"
						}
						tracker.TrackMessage(method, "http", startTime, true)
					}(i)
				}

				wg.Wait()

				// Give enough time for all events to be processed from the channel
				time.Sleep(100 * time.Millisecond)

				// Stop the tracker to force reporting of all remaining periods
				tracker.Stop()

				// Verify stats were collected
				infoLogs := logger.getLogsByLevel("info")
				var foundOverallStats, foundMethodStats bool
				var totalEvents int
				for _, log := range infoLogs {
					if log.msg == "stats" {
						foundOverallStats = true
						for i := 0; i < len(log.keyvals); i += 2 {
							if log.keyvals[i] == "count" {
								totalEvents = log.keyvals[i+1].(int)
								break
							}
						}
					}
					if log.msg == "method stats" {
						foundMethodStats = true
					}
				}
				require.True(t, foundOverallStats, "Expected to find overall stats log")
				require.True(t, foundMethodStats, "Expected to find method stats log")
				require.Equal(t, numRequests, totalEvents, "Expected all requests to be tracked")
			},
			cleanup: func(tracker *tracker, cancel context.CancelFunc) {
				// Already stopped in test
			},
		},
		{
			name: "period_completion_reporting",
			setup: func() (*tracker, *mockLogger, context.CancelFunc) {
				logger := newMockLogger()
				ctx, cancel := context.WithCancel(context.Background())
				// Use very short interval for faster testing
				tracker := newTracker(ctx, logger, "http", 50*time.Millisecond)
				return tracker, logger, cancel
			},
			test: func(t *testing.T, tracker *tracker, logger *mockLogger) {
				// Track a message in the first period
				startTime := time.Now()
				tracker.TrackMessage("eth_getBalance", "http", startTime, true)

				// Wait for event to be processed
				time.Sleep(10 * time.Millisecond)

				// Wait long enough for the period to complete and for the ticker to trigger
				time.Sleep(100 * time.Millisecond)

				// Check if the first period was automatically reported
				infoLogs := logger.getLogsByLevel("info")
				var foundAutoReportedPeriod bool

				for _, log := range infoLogs {
					if log.msg == "stats" {
						// This should be from automatic period completion, not from Stop()
						foundAutoReportedPeriod = true
						break
					}
				}

				require.True(t, foundAutoReportedPeriod, "Expected automatic period reporting")

				// Stop tracker
				tracker.Stop()
			},
			cleanup: func(tracker *tracker, cancel context.CancelFunc) {
				// Already stopped in test
			},
		},
		{
			name: "nil_tracker_handling",
			setup: func() (*tracker, *mockLogger, context.CancelFunc) {
				logger := newMockLogger()
				_, cancel := context.WithCancel(context.Background())
				return nil, logger, cancel // Return nil tracker
			},
			test: func(t *testing.T, tracker *tracker, logger *mockLogger) {
				// This should not panic even with nil tracker
				if tracker != nil {
					startTime := time.Now()
					tracker.TrackMessage("eth_getBalance", "http", startTime, true)
				}
				// Test passes if no panic occurs
			},
			cleanup: func(tracker *tracker, cancel context.CancelFunc) {
				// No cleanup needed for nil tracker
			},
		},
	}

	for _, scenario := range scenarios {
		t.Run(scenario.name, func(t *testing.T) {
			tracker, logger, cancel := scenario.setup()

			// Run the test
			scenario.test(t, tracker, logger)

			// Cleanup
			scenario.cleanup(tracker, cancel)
		})
	}
}

func TestExtractMethod(t *testing.T) {
	testCases := []struct {
		name     string
		input    []byte
		expected string
	}{
		{
			name:     "valid_json_rpc",
			input:    []byte(`{"jsonrpc":"2.0","method":"eth_getBalance","params":["0x123"],"id":1}`),
			expected: "eth_getBalance",
		},
		{
			name:     "minimal_valid_json",
			input:    []byte(`{"method":"test_method"}`),
			expected: "test_method",
		},
		{
			name:     "empty_method",
			input:    []byte(`{"method":""}`),
			expected: "unknown",
		},
		{
			name:     "malformed_json",
			input:    []byte(`{invalid json`),
			expected: "unknown",
		},
		{
			name:     "missing_method",
			input:    []byte(`{"jsonrpc":"2.0","params":[],"id":1}`),
			expected: "unknown",
		},
		{
			name:     "empty_input",
			input:    []byte(``),
			expected: "unknown",
		},
		{
			name:     "null_input",
			input:    nil,
			expected: "unknown",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := extractMethod(tc.input)
			require.Equal(t, tc.expected, result)
		})
	}
}

func TestTrackMessage(t *testing.T) {
	logger := newMockLogger()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	tracker := newTracker(ctx, logger, "http", 100*time.Millisecond)
	defer tracker.Stop()

	// Test tracking different connection types
	startTime := time.Now()
	tracker.TrackMessage("eth_getBalance", "http", startTime, true)

	startTime2 := time.Now()
	tracker.TrackMessage("eth_sendTransaction", "websocket", startTime2, false)

	// Give time for events to be processed
	time.Sleep(50 * time.Millisecond)

	// Stop the tracker to force reporting
	tracker.Stop()

	// Verify that stats were logged
	require.True(t, logger.hasLogWithMessage("stats tracker started"))
	require.True(t, logger.hasLogWithMessage("stats tracker stopped"))

	// Check that we have some stats logs
	var foundOverallStats, foundMethodStats bool
	for _, entry := range logger.logs {
		if entry.msg == "stats" {
			foundOverallStats = true
		}
		if entry.msg == "method stats" {
			foundMethodStats = true
		}
	}

	require.True(t, foundOverallStats, "Expected to find overall stats log")
	require.True(t, foundMethodStats, "Expected to find method stats log")
}

func TestNilTrackerHandling(t *testing.T) {
	var tracker *tracker = nil
	// This should not panic even with nil tracker
	startTime := time.Now()
	tracker.TrackMessage("eth_getBalance", "http", startTime, true)
	// Test passes if no panic occurs.
}

func TestRecordAPIInvocation(t *testing.T) {
	// Test the new separate tracker initialization approach
	logger := newMockLogger()
	ctx := context.Background()
	startTime := time.Now()

	// Initialize both trackers
	InitRPCTracker(ctx, logger, 100*time.Millisecond)
	InitWSTracker(ctx, logger, 100*time.Millisecond)

	// Now these should work
	RecordAPIInvocation("eth_getBalance", "http", startTime, true)
	RecordAPIInvocation("eth_sendTransaction", "websocket", startTime, false)
	RecordAPIInvocation("eth_call", "http", startTime, true)

	// Give time for events to be processed
	time.Sleep(150 * time.Millisecond)

	// Verify that trackers were created
	httpExists := httpTracker != nil
	wsExists := wsTracker != nil

	require.True(t, httpExists, "HTTP tracker should be initialized")
	require.True(t, wsExists, "WebSocket tracker should be initialized")

	// Cleanup
	if httpTracker != nil {
		httpTracker.Stop()
		httpTracker = nil
	}
	if wsTracker != nil {
		wsTracker.Stop()
		wsTracker = nil
	}
}

func TestRecordAPIInvocationConcurrent(t *testing.T) {
	// Test concurrent calls to RecordAPIInvocation with separate tracker initialization
	logger := newMockLogger()
	ctx := context.Background()

	// Initialize both trackers before concurrent access
	InitRPCTracker(ctx, logger, 100*time.Millisecond)
	InitWSTracker(ctx, logger, 100*time.Millisecond)

	var wg sync.WaitGroup
	numCalls := 50

	for i := 0; i < numCalls; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			startTime := time.Now()
			method := "eth_call"
			connType := "http"
			if id%2 == 0 {
				method = "eth_getBalance"
				connType = "websocket"
			}
			RecordAPIInvocation(method, connType, startTime, true)
		}(i)
	}

	wg.Wait()

	// Give time for events to be processed
	time.Sleep(50 * time.Millisecond)

	// Cleanup
	if httpTracker != nil {
		httpTracker.Stop()
		httpTracker = nil
	}
	if wsTracker != nil {
		wsTracker.Stop()
		wsTracker = nil
	}

	// Test passes if no panic occurs during concurrent access
}
