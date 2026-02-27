package pruning

import (
	"fmt"
	"math/rand"
	"sync"
	"time"

	"github.com/sei-protocol/sei-chain/sei-db/common/logger"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/types"
)

type Manager struct {
	logger        logger.Logger
	stateStore    types.StateStore
	keepRecent    int64
	pruneInterval int64

	// Lifecycle management
	startOnce sync.Once
	stopCh    chan struct{}
	stopOnce  sync.Once
	wg        sync.WaitGroup
}

// NewPruningManager creates a new pruning manager for state store
// Pruning Manager will periodically prune state store based on keep-recent and prune-interval configs.
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
		stopCh:        make(chan struct{}),
	}
}

func (m *Manager) Start() {
	if m.keepRecent <= 0 || m.pruneInterval <= 0 {
		return
	}
	m.startOnce.Do(func() {
		m.wg.Add(1)
		go m.pruneLoop()
	})
}

// Stop gracefully stops the pruning goroutine and waits for it to exit.
// Safe to call multiple times (idempotent).
func (m *Manager) Stop() {
	m.stopOnce.Do(func() {
		close(m.stopCh)
	})
	m.wg.Wait() // Safe: WaitGroup.Wait() is idempotent when counter is 0
}

func (m *Manager) pruneLoop() {
	defer m.wg.Done()

	for {
		// Check for stop signal before pruning
		select {
		case <-m.stopCh:
			m.logger.Info("Pruning manager stopped")
			return
		default:
		}

		pruneStartTime := time.Now()
		latestVersion := m.stateStore.GetLatestVersion()
		pruneVersion := latestVersion - m.keepRecent
		if pruneVersion > 0 {
			// prune all versions up to and including the pruneVersion
			if err := m.stateStore.Prune(pruneVersion); err != nil {
				m.logger.Error("failed to prune versions till", "version", pruneVersion, "err", err)
			} else {
				m.logger.Info(fmt.Sprintf("Pruned state store till version %d took %s\n", pruneVersion, time.Since(pruneStartTime)))
			}
		}

		// Generate a random percentage (between 0% and 100%) of the fixed interval as a delay
		randomPercentage := rand.Float64()
		randomDelay := int64(float64(m.pruneInterval) * randomPercentage)
		sleepDuration := time.Duration(m.pruneInterval+randomDelay) * time.Second

		// Wait with stop signal check
		select {
		case <-m.stopCh:
			m.logger.Info("Pruning manager stopped")
			return
		case <-time.After(sleepDuration):
			// Continue to next iteration
		}
	}
}
