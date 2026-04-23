package migration

import (
	"context"
	"errors"
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-db/proto"
)

var _ Router = (*ModuleRouter)(nil)

// ModuleRouter routes reads and writes between two databases (A and B)
// based on the module/store name they target. Each module name must be
// registered as belonging to exactly one of the two databases; reads or
// writes to an unregistered module return an error.
type ModuleRouter struct {

	// For reading values from database A.
	readerA DBReader

	// For writing values to database A.
	writerA DBWriter

	// For reading values from database B.
	readerB DBReader

	// For writing values to database B.
	writerB DBWriter

	// The module names where we should use readerA and writerA.
	aModules map[string]struct{}

	// The module names where we should use readerB and writerB.
	bModules map[string]struct{}
}

// NewModuleRouter creates a new ModuleRouter.
//
// All modules must be specified. Data routed to an unregistered module returns an error.
// This is intentionally fragile to misconfiguration: it is important to very specifically
// specify which modules belong to which database.
func NewModuleRouter(
	// For reading values from database A.
	readerA DBReader,
	// For writing values to database A.
	writerA DBWriter,
	// For reading values from database B.
	readerB DBReader,
	// For writing values to database B.
	writerB DBWriter,
	// The module names where we should use readerA and writerA.
	aModules map[string]struct{},
	// The module names where we should use readerB and writerB.
	bModules map[string]struct{},
) (*ModuleRouter, error) {
	if readerA == nil {
		return nil, fmt.Errorf("readerA must not be nil")
	}
	if writerA == nil {
		return nil, fmt.Errorf("writerA must not be nil")
	}
	if readerB == nil {
		return nil, fmt.Errorf("readerB must not be nil")
	}
	if writerB == nil {
		return nil, fmt.Errorf("writerB must not be nil")
	}
	if aModules == nil {
		return nil, fmt.Errorf("aModules must not be nil")
	}
	if bModules == nil {
		return nil, fmt.Errorf("bModules must not be nil")
	}
	for name := range aModules {
		if _, ok := bModules[name]; ok {
			return nil, fmt.Errorf("module %q is registered with both database A and B", name)
		}
	}
	return &ModuleRouter{
		readerA:  readerA,
		writerA:  writerA,
		readerB:  readerB,
		writerB:  writerB,
		aModules: aModules,
		bModules: bModules,
	}, nil
}

// ApplyChangeSets splits changesets between databases A and B based on the
// module name of each changeset and applies them in parallel. If any
// changeset targets a module that is not registered with either database,
// no writes are performed and an error is returned.
//
// Non-atomic between stores A and B, atomicity must be ensured by the caller.
func (m *ModuleRouter) ApplyChangeSets(ctx context.Context, changesets []*proto.NamedChangeSet) error {
	var aChangeSets, bChangeSets []*proto.NamedChangeSet
	for _, cs := range changesets {
		if cs == nil {
			continue
		}
		switch m.routeFor(cs.Name) {
		case routeA:
			aChangeSets = append(aChangeSets, cs)
		case routeB:
			bChangeSets = append(bChangeSets, cs)
		default:
			return fmt.Errorf("module %q is not registered with database A or B", cs.Name)
		}
	}

	aErrCh := make(chan error, 1)
	bErrCh := make(chan error, 1)
	go func() {
		err := m.writerA(ctx, aChangeSets)
		if err != nil {
			err = fmt.Errorf("failed to apply changes to database A: %w", err)
		}
		aErrCh <- err
	}()
	go func() {
		err := m.writerB(ctx, bChangeSets)
		if err != nil {
			err = fmt.Errorf("failed to apply changes to database B: %w", err)
		}
		bErrCh <- err
	}()

	var aErr, bErr error
	aDone, bDone := false, false
	for !aDone || !bDone {
		select {
		case e := <-aErrCh:
			aErr = e
			aDone = true
		case e := <-bErrCh:
			bErr = e
			bDone = true
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	if err := errors.Join(aErr, bErr); err != nil {
		return fmt.Errorf("failed to apply changes to databases: %w", err)
	}
	return nil
}

// Read returns the value for key in store, dispatching to database A or B
// based on which database owns store. Returns an error if store is not
// registered with either database.
func (m *ModuleRouter) Read(store string, key []byte) ([]byte, bool, error) {
	switch m.routeFor(store) {
	case routeA:
		return m.readerA(store, key)
	case routeB:
		return m.readerB(store, key)
	default:
		return nil, false, fmt.Errorf("module %q is not registered with database A or B", store)
	}
}

type routeTarget int

const (
	routeUnknown routeTarget = iota
	routeA
	routeB
)

func (m *ModuleRouter) routeFor(store string) routeTarget {
	if _, ok := m.aModules[store]; ok {
		return routeA
	}
	if _, ok := m.bModules[store]; ok {
		return routeB
	}
	return routeUnknown
}
