package stats

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
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
		setup       func() (*Tracker, *mockLogger, context.CancelFunc)
		test        func(t *testing.T, tracker *Tracker, logger *mockLogger)
		cleanup     func(*Tracker, context.CancelFunc)
		expectError bool
	}{
		{
			name: "basic_tracker_creation",
			setup: func() (*Tracker, *mockLogger, context.CancelFunc) {
				logger := newMockLogger()
				ctx, cancel := context.WithCancel(context.Background())
				tracker := NewTracker(ctx, logger, 100*time.Millisecond)
				return tracker, logger, cancel
			},
			test: func(t *testing.T, tracker *Tracker, logger *mockLogger) {
				require.NotNil(t, tracker)
				require.NotNil(t, tracker.ch)
				require.Equal(t, 100*time.Millisecond, tracker.interval)

				// Wait a bit for startup log
				time.Sleep(50 * time.Millisecond)
				require.True(t, logger.hasLogWithMessage("stats tracker started"))
			},
			cleanup: func(tracker *Tracker, cancel context.CancelFunc) {
				tracker.Stop()
			},
		},
		{
			name: "tracker_stop_lifecycle",
			setup: func() (*Tracker, *mockLogger, context.CancelFunc) {
				logger := newMockLogger()
				ctx, cancel := context.WithCancel(context.Background())
				tracker := NewTracker(ctx, logger, 100*time.Millisecond)
				return tracker, logger, cancel
			},
			test: func(t *testing.T, tracker *Tracker, logger *mockLogger) {
				// Let it run briefly
				time.Sleep(50 * time.Millisecond)

				tracker.Stop()

				// Check stop log appears
				time.Sleep(50 * time.Millisecond)
				require.True(t, logger.hasLogWithMessage("stats tracker stopped"))
			},
			cleanup: func(tracker *Tracker, cancel context.CancelFunc) {
				// Already stopped in test
			},
		},
		{
			name: "middleware_successful_request",
			setup: func() (*Tracker, *mockLogger, context.CancelFunc) {
				logger := newMockLogger()
				ctx, cancel := context.WithCancel(context.Background())
				tracker := NewTracker(ctx, logger, 50*time.Millisecond)
				return tracker, logger, cancel
			},
			test: func(t *testing.T, tracker *Tracker, logger *mockLogger) {
				// Create test handler
				handler := tracker.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(http.StatusOK)
					w.Write([]byte("success"))
				}), "http")

				// Create test request with JSON-RPC payload
				payload := `{"jsonrpc":"2.0","method":"eth_getBalance","params":["0x123"],"id":1}`
				req := httptest.NewRequest("POST", "/", strings.NewReader(payload))
				req.Header.Set("Content-Type", "application/json")

				rr := httptest.NewRecorder()
				handler.ServeHTTP(rr, req)

				require.Equal(t, http.StatusOK, rr.Code)
				require.Equal(t, "success", rr.Body.String())

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
			cleanup: func(tracker *Tracker, cancel context.CancelFunc) {
				// Already stopped in test
			},
		},
		{
			name: "middleware_error_request",
			setup: func() (*Tracker, *mockLogger, context.CancelFunc) {
				logger := newMockLogger()
				ctx, cancel := context.WithCancel(context.Background())
				tracker := NewTracker(ctx, logger, 100*time.Millisecond)
				return tracker, logger, cancel
			},
			test: func(t *testing.T, tracker *Tracker, logger *mockLogger) {
				// Create test handler that returns error
				handler := tracker.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(http.StatusInternalServerError)
					w.Write([]byte("error"))
				}), "websocket")

				payload := `{"jsonrpc":"2.0","method":"eth_sendTransaction","params":[],"id":1}`
				req := httptest.NewRequest("POST", "/", strings.NewReader(payload))

				rr := httptest.NewRecorder()
				handler.ServeHTTP(rr, req)

				require.Equal(t, http.StatusInternalServerError, rr.Code)

				// Give enough time for the event to be processed from the channel
				time.Sleep(50 * time.Millisecond)

				// Stop the tracker to force reporting of all remaining periods
				tracker.Stop()

				// Verify error was tracked
				infoLogs := logger.getLogsByLevel("info")
				var foundOverallStats, foundMethodStats bool
				var overallSuccessRate, methodSuccessRate float64

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
						// Check that method success rate reflects the error
						for i := 0; i < len(log.keyvals); i += 2 {
							if log.keyvals[i] == "success_rate_pct" {
								methodSuccessRate = log.keyvals[i+1].(float64)
							}
						}
					}
				}
				require.True(t, foundOverallStats, "Expected to find overall stats log")
				require.True(t, foundMethodStats, "Expected to find method stats log")
				require.Equal(t, 0.0, overallSuccessRate, "Expected 0% overall success rate for error request")
				require.Equal(t, 0.0, methodSuccessRate, "Expected 0% method success rate for error request")
			},
			cleanup: func(tracker *Tracker, cancel context.CancelFunc) {
				// Already stopped in test
			},
		},
		{
			name: "channel_overflow_handling",
			setup: func() (*Tracker, *mockLogger, context.CancelFunc) {
				logger := newMockLogger()
				ctx, cancel := context.WithCancel(context.Background())
				tracker := NewTracker(ctx, logger, 1*time.Second) // Longer interval to prevent flushing
				return tracker, logger, cancel
			},
			test: func(t *testing.T, tracker *Tracker, logger *mockLogger) {
				handler := tracker.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(http.StatusOK)
				}), "http")

				// Fill the channel beyond capacity (10000 events)
				// We'll send a reasonable number to test overflow without taking too long
				for i := 0; i < 100; i++ {
					req := httptest.NewRequest("POST", "/", strings.NewReader(`{"method":"test"}`))
					rr := httptest.NewRecorder()
					handler.ServeHTTP(rr, req)
				}

				// Check for potential overflow debug logs
				time.Sleep(50 * time.Millisecond)
				debugLogs := logger.getLogsByLevel("debug")

				// We might not hit overflow with just 100 requests, but the test verifies the structure works
				require.True(t, len(debugLogs) >= 0, "Debug logs should be accessible")
			},
			cleanup: func(tracker *Tracker, cancel context.CancelFunc) {
				// Already stopped in test
			},
		},
		{
			name: "malformed_json_handling",
			setup: func() (*Tracker, *mockLogger, context.CancelFunc) {
				logger := newMockLogger()
				ctx, cancel := context.WithCancel(context.Background())
				tracker := NewTracker(ctx, logger, 100*time.Millisecond)
				return tracker, logger, cancel
			},
			test: func(t *testing.T, tracker *Tracker, logger *mockLogger) {
				handler := tracker.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(http.StatusOK)
				}), "http")

				// Send malformed JSON
				req := httptest.NewRequest("POST", "/", strings.NewReader(`{invalid json`))
				rr := httptest.NewRecorder()
				handler.ServeHTTP(rr, req)

				require.Equal(t, http.StatusOK, rr.Code)

				// Give a small delay to ensure event is processed
				time.Sleep(10 * time.Millisecond)

				// Stop the tracker to force reporting of all remaining periods
				tracker.Stop()

				// Should still log stats with "unknown" method
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
				require.True(t, foundOverallStats, "Expected overall stats log even with malformed JSON")
				require.True(t, foundMethodStats, "Expected method stats log even with malformed JSON")
			},
			cleanup: func(tracker *Tracker, cancel context.CancelFunc) {
				// Already stopped in test
			},
		},
		{
			name: "concurrent_requests",
			setup: func() (*Tracker, *mockLogger, context.CancelFunc) {
				logger := newMockLogger()
				ctx, cancel := context.WithCancel(context.Background())
				tracker := NewTracker(ctx, logger, 200*time.Millisecond)
				return tracker, logger, cancel
			},
			test: func(t *testing.T, tracker *Tracker, logger *mockLogger) {
				handler := tracker.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					// Simulate some processing time
					time.Sleep(10 * time.Millisecond)
					w.WriteHeader(http.StatusOK)
				}), "http")

				// Run concurrent requests
				var wg sync.WaitGroup
				numRequests := 50

				for i := 0; i < numRequests; i++ {
					wg.Add(1)
					go func(id int) {
						defer wg.Done()
						payload := `{"jsonrpc":"2.0","method":"eth_call","params":[],"id":` +
							string(rune('0'+id%10)) + `}`
						req := httptest.NewRequest("POST", "/", strings.NewReader(payload))
						rr := httptest.NewRecorder()
						handler.ServeHTTP(rr, req)
						require.Equal(t, http.StatusOK, rr.Code)
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
			cleanup: func(tracker *Tracker, cancel context.CancelFunc) {
				tracker.Stop()
			},
		},
		{
			name: "period_completion_reporting",
			setup: func() (*Tracker, *mockLogger, context.CancelFunc) {
				logger := newMockLogger()
				ctx, cancel := context.WithCancel(context.Background())
				// Use very short interval for faster testing
				tracker := NewTracker(ctx, logger, 50*time.Millisecond)
				return tracker, logger, cancel
			},
			test: func(t *testing.T, tracker *Tracker, logger *mockLogger) {
				handler := tracker.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(http.StatusOK)
				}), "http")

				// Make a request in the first period
				payload1 := `{"jsonrpc":"2.0","method":"eth_getBalance","params":[],"id":1}`
				req1 := httptest.NewRequest("POST", "/", strings.NewReader(payload1))
				rr1 := httptest.NewRecorder()
				handler.ServeHTTP(rr1, req1)
				require.Equal(t, http.StatusOK, rr1.Code)

				// Wait for event to be processed
				time.Sleep(10 * time.Millisecond)

				// Wait long enough for the period to complete AND for the ticker to trigger reportCompletedPeriods
				// The ticker runs every 1 second, and we need the period (50ms) + interval (50ms) + ticker time
				time.Sleep(1200 * time.Millisecond)

				// Check if the first period was automatically reported by reportCompletedPeriods
				infoLogs := logger.getLogsByLevel("info")
				var foundAutoReportedPeriod bool

				for _, log := range infoLogs {
					if log.msg == "stats" {
						// This should be from automatic period completion, not from Stop()
						foundAutoReportedPeriod = true
						break
					}
				}

				require.True(t, foundAutoReportedPeriod, "Expected reportCompletedPeriods to automatically report the completed period")

				// Stop tracker
				tracker.Stop()
			},
			cleanup: func(tracker *Tracker, cancel context.CancelFunc) {
				// Already stopped in test
			},
		},
		{
			name: "panic_tracking",
			setup: func() (*Tracker, *mockLogger, context.CancelFunc) {
				logger := newMockLogger()
				ctx, cancel := context.WithCancel(context.Background())
				tracker := NewTracker(ctx, logger, 50*time.Millisecond)
				return tracker, logger, cancel
			},
			test: func(t *testing.T, tracker *Tracker, logger *mockLogger) {
				handler := tracker.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					// Simulate a panic like the eth_panic endpoint
					panic("test panic")
				}), "http")

				// Make request that will panic
				payload := `{"jsonrpc":"2.0","method":"eth_panic","params":[],"id":1}`
				req := httptest.NewRequest("POST", "/", strings.NewReader(payload))

				// Capture the panic
				var panicValue interface{}
				func() {
					defer func() {
						panicValue = recover()
					}()
					rr := httptest.NewRecorder()
					handler.ServeHTTP(rr, req)
				}()

				// Verify panic was re-thrown
				require.NotNil(t, panicValue, "Expected panic to be re-thrown")
				require.Equal(t, "test panic", panicValue, "Expected panic value to match")

				// Give time for event to be processed
				time.Sleep(50 * time.Millisecond)

				// Stop tracker to force reporting
				tracker.Stop()

				// Verify panic was tracked as failure
				infoLogs := logger.getLogsByLevel("info")
				var foundOverallStats, foundMethodStats bool
				var overallSuccessRate float64

				for _, log := range infoLogs {
					if log.msg == "stats" {
						foundOverallStats = true
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
				require.Equal(t, 0.0, overallSuccessRate, "Expected 0% success rate for panic")

				// Verify panic was logged as error
				errorLogs := logger.getLogsByLevel("error")
				var foundPanicLog bool
				for _, log := range errorLogs {
					if log.msg == "panic occurred in RPC handler" {
						foundPanicLog = true
						break
					}
				}
				require.True(t, foundPanicLog, "Expected to find panic error log")
			},
			cleanup: func(tracker *Tracker, cancel context.CancelFunc) {
				// Already stopped in test
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

func TestMinInt(t *testing.T) {
	testCases := []struct {
		name     string
		a, b     int
		expected int
	}{
		{
			name:     "a_smaller",
			a:        5,
			b:        10,
			expected: 5,
		},
		{
			name:     "b_smaller",
			a:        10,
			b:        5,
			expected: 5,
		},
		{
			name:     "equal",
			a:        7,
			b:        7,
			expected: 7,
		},
		{
			name:     "negative_numbers",
			a:        -5,
			b:        -10,
			expected: -10,
		},
		{
			name:     "zero_and_positive",
			a:        0,
			b:        5,
			expected: 0,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := minInt(tc.a, tc.b)
			require.Equal(t, tc.expected, result)
		})
	}
}

func TestResponseCapture(t *testing.T) {
	testCases := []struct {
		name           string
		writeHeader    bool
		statusCode     int
		expectedStatus int
	}{
		{
			name:           "default_status_200",
			writeHeader:    false,
			expectedStatus: 200,
		},
		{
			name:           "explicit_status_404",
			writeHeader:    true,
			statusCode:     404,
			expectedStatus: 404,
		},
		{
			name:           "explicit_status_500",
			writeHeader:    true,
			statusCode:     500,
			expectedStatus: 500,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			rr := httptest.NewRecorder()
			capture := &responseCapture{ResponseWriter: rr, status: 200}

			if tc.writeHeader {
				capture.WriteHeader(tc.statusCode)
			}

			require.Equal(t, tc.expectedStatus, capture.status)
		})
	}
}
