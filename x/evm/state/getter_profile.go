package state

import (
	"time"

	"github.com/sei-protocol/sei-chain/evmrpc/traceprofile"
)

func (s *DBImpl) traceProfile() traceprofile.Recorder {
	if s == nil {
		return nil
	}
	return traceprofile.FromContext(s.ctx.Context())
}

func (s *DBImpl) startGetterProfile(name string) (traceprofile.Recorder, time.Time) {
	profile := s.traceProfile()
	if profile == nil {
		return nil, time.Time{}
	}
	profile.AddCount(name+"_count", 1)
	return profile, time.Now()
}

func finishGetterProfile(profile traceprofile.Recorder, start time.Time, name string) {
	if profile == nil {
		return
	}
	profile.AddDuration(name+"_total", time.Since(start))
}
