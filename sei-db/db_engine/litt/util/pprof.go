//go:build littdb_wip

package util

import (
	"fmt"
	"log/slog"
	"net/http"

	_ "net/http/pprof"
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

	if err := http.ListenAndServe(pprofAddr, nil); err != nil {
		p.logger.Error("pprof server failed", "error", err, "pprofAddr", pprofAddr)
	}
}
