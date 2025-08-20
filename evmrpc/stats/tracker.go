package stats

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/tendermint/tendermint/libs/log"
)

type Event struct {
	Method     string
	Connection string
	Duration   time.Duration
	Success    bool
	Timestamp  time.Time
}

// periodStats holds aggregated stats for a time period
type periodStats struct {
	periodStart  time.Time
	connType     string
	totalEvents  int
	totalSuccess int
	methodData   map[string]*methodStats
}

// methodStats holds per-method aggregated stats
type methodStats struct {
	count        int
	successCount int
	totalLatency time.Duration
	maxLatency   time.Duration
}

type Tracker struct {
	logger   log.Logger
	ch       chan Event
	interval time.Duration
	ctx      context.Context
	cancel   context.CancelFunc
	wg       sync.WaitGroup

	// Simple current period tracking
	mu            sync.RWMutex
	currentPeriod *periodStats
}

func NewTracker(ctx context.Context, logger log.Logger, interval time.Duration) *Tracker {
	trackerCtx, cancel := context.WithCancel(ctx)

	t := &Tracker{
		logger:   logger,
		ch:       make(chan Event, 10000),
		interval: interval,
		ctx:      trackerCtx,
		cancel:   cancel,
	}

	t.wg.Add(1)
	go t.run()
	return t
}

func (t *Tracker) Middleware(next http.Handler, connType string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		// Read body for method extraction with error handling
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.logger.Debug("failed to read request body", "error", err)
		}
		r.Body = io.NopCloser(bytes.NewReader(body))
		method := extractMethod(body)

		rw := &responseCapture{ResponseWriter: w, status: 200, captureBody: true}

		// Track panics as failures
		var panicOccurred bool
		var panicValue interface{}
		defer func() {
			if recovered := recover(); recovered != nil {
				panicOccurred = true
				panicValue = recovered
				// Log the panic for debugging
				t.logger.Error("panic occurred in RPC handler",
					"method", method,
					"connection", connType,
					"panic", recovered)
			}

			// Check if response indicates a panic (JSON-RPC error -32603 "method handler crashed")
			isPanicResponse := rw.isPanicResponse()
			if isPanicResponse {
				t.logger.Error("panic detected from response",
					"method", method,
					"connection", connType,
					"response_body", string(rw.body))
			}

			// Create event and try to send non-blocking
			event := Event{
				Method:     method,
				Connection: connType,
				Duration:   time.Since(start),
				Success:    !panicOccurred && !isPanicResponse && rw.status < 400,
				Timestamp:  start, // Use request start time for bucketing
			}

			select {
			case t.ch <- event:
			default:
				// Drop on overflow to not block RPC - log at debug level to avoid spam
				t.logger.Debug("event channel full, dropping event",
					"method", method,
					"connection", connType)
			}

			// Re-panic to maintain original behavior
			if panicOccurred {
				panic(panicValue)
			}
		}()
		next.ServeHTTP(rw, r)
	})
}

// run processes events continuously and reports periods on interval
func (t *Tracker) run() {
	defer t.wg.Done()

	// Report stats every interval
	ticker := time.NewTicker(t.interval)
	defer ticker.Stop()

	t.logger.Info("stats tracker started", "interval", t.interval)

	for {
		select {
		case <-t.ctx.Done():
			t.logger.Info("stats tracker stopping", "reason", t.ctx.Err())
			// Report current period before stopping
			t.reportCurrentPeriod()
			return
		case event := <-t.ch:
			// Process event immediately
			t.processEvent(event)
		case <-ticker.C:
			// Report current period and start fresh
			t.reportCurrentPeriod()
		}
	}
}

// processEvent aggregates an event into the current period
func (t *Tracker) processEvent(event Event) {
	t.mu.Lock()
	defer t.mu.Unlock()

	// Initialize current period if needed
	if t.currentPeriod == nil {
		t.currentPeriod = &periodStats{
			periodStart:  time.Now().Truncate(t.interval),
			connType:     event.Connection,
			methodData:   make(map[string]*methodStats),
			totalEvents:  0,
			totalSuccess: 0,
		}
	}

	// Update overall stats
	t.currentPeriod.totalEvents++
	if event.Success {
		t.currentPeriod.totalSuccess++
	}

	// Get or create method stats
	method := t.currentPeriod.methodData[event.Method]
	if method == nil {
		method = &methodStats{}
		t.currentPeriod.methodData[event.Method] = method
	}

	// Update method stats
	method.count++
	method.totalLatency += event.Duration
	if event.Success {
		method.successCount++
	}
	if event.Duration > method.maxLatency {
		method.maxLatency = event.Duration
	}
}

// reportCurrentPeriod reports the current period and starts a new one
func (t *Tracker) reportCurrentPeriod() {
	t.mu.Lock()
	defer t.mu.Unlock()

	// If no current period, create an empty one for this interval
	if t.currentPeriod == nil {
		t.currentPeriod = &periodStats{
			periodStart:  time.Now().Truncate(t.interval),
			connType:     "http", // default connection type
			methodData:   make(map[string]*methodStats),
			totalEvents:  0,
			totalSuccess: 0,
		}
	}

	// Report the current period
	t.reportPeriod(t.currentPeriod)

	// Start a new period
	t.currentPeriod = nil
}

// reportPeriod logs the stats for a completed period
func (t *Tracker) reportPeriod(period *periodStats) {
	// Always log overall stats, even for periods with no events
	if period.totalEvents == 0 {
		// Log overall stats for periods with no requests
		t.logger.Info("stats",
			"period", period.periodStart.Format("2006-01-02T15:04:05Z"),
			"count", 0,
			"conn_type", period.connType,
			"connections", 0,
			"interval_ms", t.interval.Milliseconds(),
		)
		return // No method stats to report
	}

	// Calculate overall success rate
	overallSuccessRate := float64(period.totalSuccess) / float64(period.totalEvents) * 100

	// Log overall stats for this period
	t.logger.Info("stats",
		"period", period.periodStart.Format("2006-01-02T15:04:05Z"),
		"count", period.totalEvents,
		"success_rate_pct", overallSuccessRate,
		"conn_type", period.connType,
		"connections", period.totalEvents,
		"interval_ms", t.interval.Milliseconds(),
	)

	// Log per-method stats for this period
	for method, stats := range period.methodData {

		// Calculate average latency in milliseconds with 2 decimal places
		avgLatencyMs := float64(stats.totalLatency.Nanoseconds()) / float64(stats.count) / 1000000.0
		maxLatencyMs := float64(stats.maxLatency.Nanoseconds()) / 1000000.0

		t.logger.Info("method stats",
			"period", period.periodStart.Format("2006-01-02T15:04:05Z"),
			"method", method,
			"count", stats.count,
			"success", stats.successCount,
			"fails", stats.count-stats.successCount,
			"latency_avg", avgLatencyMs,
			"latency_max", maxLatencyMs,
		)
	}
}

func (t *Tracker) Stop() {
	t.cancel()  // Cancel context to stop the goroutine
	t.wg.Wait() // Wait for goroutine to finish
	t.logger.Info("stats tracker stopped")
}

type responseCapture struct {
	http.ResponseWriter
	status      int
	body        []byte
	captureBody bool
}

// WriteHeader captures status code.
func (r *responseCapture) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

// Write captures response body if captureBody is enabled.
func (r *responseCapture) Write(data []byte) (int, error) {
	if r.captureBody {
		r.body = append(r.body, data...)
	}
	return r.ResponseWriter.Write(data)
}

// isPanicResponse checks if the response indicates a panic occurred.
func (r *responseCapture) isPanicResponse() bool {
	if !r.captureBody || len(r.body) == 0 {
		return false
	}

	// Check for JSON-RPC error response with code -32603 and "method handler crashed"
	bodyStr := string(r.body)
	return strings.Contains(bodyStr, `"code":-32603`) &&
		strings.Contains(bodyStr, `"method handler crashed"`)
}

// minInt returns the minimum of two integers.
func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// extractMethod extracts method from JSON payload.
func extractMethod(body []byte) string {
	var tmp struct {
		Method string `json:"method"`
	}
	if err := json.Unmarshal(body, &tmp); err == nil {
		if tmp.Method == "" {
			return "unknown"
		}
		return tmp.Method
	}
	return "unknown"
}
