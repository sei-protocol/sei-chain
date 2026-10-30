package rpc

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/eth/filters"
	ethrpc "github.com/ethereum/go-ethereum/rpc"
)

var errFilterNotFound = errors.New("filter does not exist")

// pollFilter is one filter installed by eth_newFilter or eth_newBlockFilter.
type pollFilter struct {
	// crit is a log filter's criteria, and nil for a block filter.
	crit *filters.FilterCriteria

	// mu serializes polls of the filter, so each poll resumes where the previous one stopped.
	mu sync.Mutex
	// cursor is the first block height the next eth_getFilterChanges scans.
	cursor uint64

	// lastPolled is when the filter was installed or last read, guarded by the registry's mutex.
	lastPolled time.Time
}

// filterRegistry holds the installed filters. A filter not read for timeout is removed; a zero
// timeout or maxFilters disables that limit.
type filterRegistry struct {
	timeout    time.Duration
	maxFilters uint64

	mu      sync.Mutex
	filters map[ethrpc.ID]*pollFilter
}

func newFilterRegistry(timeout time.Duration, maxFilters uint64) *filterRegistry {
	return &filterRegistry{timeout: timeout, maxFilters: maxFilters, filters: make(map[ethrpc.ID]*pollFilter)}
}

// add installs f and returns its ID, or an error when maxFilters filters are installed.
func (r *filterRegistry) add(f *pollFilter) (ethrpc.ID, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := time.Now()
	for id, installed := range r.filters {
		if r.expired(installed, now) {
			delete(r.filters, id)
		}
	}
	if r.maxFilters > 0 && uint64(len(r.filters)) >= r.maxFilters {
		return "", fmt.Errorf("too many filters: maximum allowed is %d", r.maxFilters)
	}
	f.lastPolled = now
	id := ethrpc.NewID()
	r.filters[id] = f
	return id, nil
}

// get returns the filter installed as id and restarts its timeout, or errFilterNotFound when it
// was never installed, was removed, or has expired.
func (r *filterRegistry) get(id ethrpc.ID) (*pollFilter, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	f, ok := r.filters[id]
	if !ok {
		return nil, errFilterNotFound
	}
	now := time.Now()
	if r.expired(f, now) {
		delete(r.filters, id)
		return nil, errFilterNotFound
	}
	f.lastPolled = now
	return f, nil
}

// remove uninstalls the filter installed as id, reporting whether it was installed and unexpired.
func (r *filterRegistry) remove(id ethrpc.ID) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	f, ok := r.filters[id]
	if !ok {
		return false
	}
	delete(r.filters, id)
	return !r.expired(f, time.Now())
}

func (r *filterRegistry) expired(f *pollFilter, now time.Time) bool {
	return r.timeout > 0 && now.Sub(f.lastPolled) > r.timeout
}
