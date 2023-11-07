package pruning

import (
	"github.com/sei-protocol/sei-db/common/logger"
	"github.com/sei-protocol/sei-db/ss/types"
	"time"
)

type PruningManager struct {
	logger        logger.Logger
	stateStore    types.StateStore
	keepRecent    int64
	pruneInterval int64
	started       bool
}

func NewPruningManager(
	logger logger.Logger,
	stateStore types.StateStore,
	keepRecent int64,
	pruneInterval int64,
) *PruningManager {
	return &PruningManager{
		logger:        logger,
		stateStore:    stateStore,
		keepRecent:    keepRecent,
		pruneInterval: pruneInterval,
	}
}

func (m *PruningManager) Start() {
	if m.keepRecent <= 0 || m.pruneInterval <= 0 || m.started {
		return
	}
	m.started = true
	go func() {
		for {
			latestVersion, _ := m.stateStore.GetLatestVersion()
			pruneVersion := latestVersion - m.keepRecent
			if pruneVersion > 0 {
				// prune all versions up to and including the pruneVersion
				if err := m.stateStore.Prune(pruneVersion); err != nil {
					m.logger.Error("failed to prune versions till", "version", pruneVersion, "err", err)
				}
			}
			time.Sleep(time.Duration(m.pruneInterval) * time.Second)
		}
	}()
}
