package util

import (
	"fmt"
	"log/slog"
	"net/http"
	"time"

	_ "net/http/pprof" //nolint:gosec // pprof endpoint is intentional for profiling
)

type PprofProfiler struct {
	logger   *slog.Logger
	httpPort string
}

func NewPprofProfiler(httpPort string, logger *slog.Logger) *PprofProfiler {
	return &PprofProfiler{
		logger:   logger.With("component", "PprofProfiler"),
		httpPort: httpPort,
	}
}

// Start the pprof server
func (p *PprofProfiler) Start() {
	pprofAddr := fmt.Sprintf("%s:%s", "0.0.0.0", p.httpPort)

	server := &http.Server{
		Addr:              pprofAddr,
		ReadHeaderTimeout: 10 * time.Second,
	}
	if err := server.ListenAndServe(); err != nil {
		p.logger.Error("pprof server failed", "error", err, "pprofAddr", pprofAddr)
	}
}
