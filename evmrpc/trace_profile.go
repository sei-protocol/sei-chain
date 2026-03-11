package evmrpc

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/sei-protocol/sei-chain/evmrpc/traceprofile"
	"github.com/tendermint/tendermint/libs/log"
)

type traceProfile struct {
	logger    log.Logger
	method    string
	threshold time.Duration
	start     time.Time

	mu        sync.Mutex
	fields    map[string]interface{}
	counters  map[string]int
	durations map[string]time.Duration
}

func newTraceProfile(logger log.Logger, method string, threshold time.Duration, fields map[string]interface{}) *traceProfile {
	if fields == nil {
		fields = map[string]interface{}{}
	}
	return &traceProfile{
		logger:    logger,
		method:    method,
		threshold: threshold,
		start:     time.Now(),
		fields:    fields,
		counters:  map[string]int{},
		durations: map[string]time.Duration{},
	}
}

func (p *traceProfile) AddDuration(name string, duration time.Duration) {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.durations[name] += duration
}

func (p *traceProfile) AddCount(name string, delta int) {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.counters[name] += delta
}

func (p *traceProfile) finish(err error) {
	if p == nil {
		return
	}
	total := time.Since(p.start)
	if err == nil && p.threshold > 0 && total < p.threshold {
		return
	}

	var dbGetterTotal time.Duration
	for key, value := range p.durations {
		if strings.HasPrefix(key, "db_") {
			dbGetterTotal += value
		}
	}
	if dbGetterTotal > 0 {
		p.durations["db_getters_total"] = dbGetterTotal
	}

	p.mu.Lock()
	fields := make([]interface{}, 0, 2+len(p.fields)*2+len(p.counters)*2+len(p.durations)*2+2)
	fields = append(fields, "method", p.method, "total", total)
	for key, value := range p.fields {
		fields = append(fields, key, value)
	}
	for key, value := range p.counters {
		fields = append(fields, key, value)
	}
	for key, value := range p.durations {
		fields = append(fields, key, value)
	}
	p.mu.Unlock()

	if err != nil {
		fields = append(fields, "error", err.Error())
	}
	p.logger.Info("trace profile", fields...)
}

func (api *DebugAPI) startTraceProfile(ctx context.Context, method string, fields map[string]interface{}) (context.Context, func(error)) {
	if api == nil || !api.traceProfileEnabled {
		return ctx, func(error) {}
	}
	logger := api.ctxProvider(LatestCtxHeight).Logger().With("module", "evmrpc", "conn_type", string(api.connectionType))
	profile := newTraceProfile(logger, method, api.traceProfileThreshold, fields)
	return traceprofile.WithRecorder(ctx, profile), profile.finish
}
