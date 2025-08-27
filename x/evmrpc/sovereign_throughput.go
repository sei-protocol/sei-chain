package evmrpc

import (
	"sync"
	"time"
)

type SovereignTPSMonitor struct {
	mu        sync.Mutex
	counter   int64
	lastReset time.Time
	maxTPS    int64
}

func NewSovereignTPSMonitor(maxTPS int64) *SovereignTPSMonitor {
	return &SovereignTPSMonitor{
		lastReset: time.Now(),
		maxTPS:    maxTPS,
	}
}

func (s *SovereignTPSMonitor) Allow() bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	if now.Sub(s.lastReset) > time.Second {
		s.counter = 0
		s.lastReset = now
	}

	if s.counter >= s.maxTPS {
		return false
	}

	s.counter++
	return true
}
