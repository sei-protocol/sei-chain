package pruning

import (
	"math/rand"
	"time"

	"github.com/sei-protocol/sei-db/common/logger"
	"github.com/sei-protocol/sei-db/ss/types"
)

type Manager struct {
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
) *Manager {
	return &Manager{
		logger:        logger,
		stateStore:    stateStore,
		keepRecent:    keepRecent,
		pruneInterval: pruneInterval,
	}
}

func (m *Manager) Start() {
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
			// Generate a random percentage (between 0% and 100%) of the fixed interval as a delay
			randomPercentage := rand.Float64() // Generate a random float between 0 and 1
			randomDelay := int64(float64(m.pruneInterval) * randomPercentage)
			time.Sleep(time.Duration(m.pruneInterval+randomDelay) * time.Second)
		}
	}()
}
