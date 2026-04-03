package pprof

import (
	"fmt"
	"net/http"

	_ "net/http/pprof"

	"github.com/Layr-Labs/eigensdk-go/logging"
)

type PprofProfiler struct {
	logger   logging.Logger
	httpPort string
}

func NewPprofProfiler(httpPort string, logger logging.Logger) *PprofProfiler {
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
