package migration

import (
	"context"
	"errors"
	"fmt"

	ics23 "github.com/confio/ics23/go"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	db "github.com/tendermint/tm-db"
)

// Route binds a set of module/store names to the database accessors
// (reader, writer, and optionally iterator and proof builders) that
// should be used to access them. A ModuleRouter dispatches reads,
// writes, iteration and proof requests to the matching Route.
type Route struct {
	// The module names to route to this destination. Guaranteed to
	// contain no duplicates by NewRoute.
	modules []string
	// For reading values from the database.
	reader DBReader
	// For writing values to the database.
	writer DBWriter
	// For getting an iterator over a range of keys in a store. If nil, the route does not support iteration.
	iteratorBuilder DBIteratorBuilder
	// For building a proof of the value for a key in a store. If nil, the route does not support proofs.
	proofBuilder DBProofBuilder
}

// NewRoute creates a new Route.
//
// modules may be empty (the route will simply receive no traffic), but
// each name listed must be unique: a duplicate is rejected as a
// misconfiguration.
func NewRoute(
	// For reading values from the database.
	reader DBReader,
	// For writing values to the database.
	writer DBWriter,
	// For getting an iterator over a range of keys in a store. If nil, the route does not support iteration.
	iteratorBuilder DBIteratorBuilder,
	// For building a proof of the value for a key in a store. If nil, the route does not support proofs.
	proofBuilder DBProofBuilder,
	// The module names to route to this destination. Must not contain
	// duplicates.
	modules ...string,
) (*Route, error) {
	if reader == nil {
		return nil, fmt.Errorf("reader must not be nil")
	}
	if writer == nil {
		return nil, fmt.Errorf("writer must not be nil")
	}
	seen := make(map[string]struct{}, len(modules))
	for _, name := range modules {
		if _, ok := seen[name]; ok {
			return nil, fmt.Errorf("module %q listed more than once", name)
		}
		seen[name] = struct{}{}
	}
	// Defensive copy: callers passing a slice with `mods...` should not
	// be able to mutate our internal state after construction.
	owned := append([]string(nil), modules...)
	return &Route{
		modules:         owned,
		reader:          reader,
		writer:          writer,
		iteratorBuilder: iteratorBuilder,
		proofBuilder:    proofBuilder,
	}, nil
}

var _ Router = (*ModuleRouter)(nil)

// ModuleRouter routes reads and writes between any number of databases
// based on the module/store name they target. Each module name must be
// registered with exactly one Route; reads or writes to an unregistered
// module return an error.
type ModuleRouter struct {
	// The routes managed by this router, in the order they were
	// registered. ApplyChangeSets dispatches to each route's writer
	// sequentially in registration order.
	routes []*Route

	// Lookup from module/store name to the route that owns it.
	moduleToRoute map[string]*Route
}

// NewModuleRouter creates a new ModuleRouter from one or more Routes.
//
// At least one Route must be provided. No module name may appear in more
// than one Route. Data targeting a module that is not registered with any
// Route returns an error.
//
// This is intentionally fragile to misconfiguration: it is important to
// very specifically specify which modules belong to which database.
func NewModuleRouter(routes ...*Route) (*ModuleRouter, error) {
	if len(routes) == 0 {
		return nil, fmt.Errorf("at least one Route must be provided")
	}
	moduleToRoute := make(map[string]*Route)
	for i, r := range routes {
		if r == nil {
			return nil, fmt.Errorf("Route at index %d must not be nil", i)
		}
		for _, name := range r.modules {
			if _, ok := moduleToRoute[name]; ok {
				return nil, fmt.Errorf("module %q is registered with multiple Routes", name)
			}
			moduleToRoute[name] = r
		}
	}
	// Defensive copy: callers passing a slice with `routes...` should not
	// be able to mutate our internal state after construction.
	owned := append([]*Route(nil), routes...)
	return &ModuleRouter{
		routes:        owned,
		moduleToRoute: moduleToRoute,
	}, nil
}

// ApplyChangeSets splits changesets across the registered routes based
// on the module name of each changeset and applies them sequentially in
// registration order. If any changeset targets a module that is not
// registered with any route, no writes are performed and an error is
// returned.
//
// Every route's writer is invoked exactly once per call (provided the
// context has not been cancelled before the route's turn), even if no
// changesets are routed to it. A writer that returns an error does not
// abort the loop; later writers still run and all errors are aggregated
// via [errors.Join]. Between routes the context is checked: a cancelled
// context short-circuits the remaining routes and the cancellation error
// is joined with any errors already accumulated.
//
// Sequential is a deliberate correctness/simplicity choice at the
// current 2-3 routes/block scale: it makes thread-safety reasoning local
// (writers don't have to be safe for concurrent invocation by the
// router) and keeps the per-route ordering deterministic. Revisit if
// migration-window throughput becomes a bottleneck.
//
// Non-atomic across routes; atomicity must be ensured by the caller.
func (m *ModuleRouter) ApplyChangeSets(ctx context.Context, changesets []*proto.NamedChangeSet) error {
	perRoute := make(map[*Route][]*proto.NamedChangeSet, len(m.routes))
	for _, cs := range changesets {
		if cs == nil {
			continue
		}
		r, ok := m.moduleToRoute[cs.Name]
		if !ok {
			return fmt.Errorf("module %q is not registered with any Route", cs.Name)
		}
		perRoute[r] = append(perRoute[r], cs)
	}

	collected := make([]error, 0, len(m.routes))
	for _, r := range m.routes {
		if err := ctx.Err(); err != nil {
			collected = append(collected, err)
			break
		}
		if err := r.writer(ctx, perRoute[r]); err != nil {
			collected = append(collected, fmt.Errorf("failed to apply changes: %w", err))
		}
	}

	if err := errors.Join(collected...); err != nil {
		return fmt.Errorf("failed to apply changes to databases: %w", err)
	}
	return nil
}

// Read returns the value for key in store, dispatching to the route
// that owns store. Returns an error if store is not registered with any
// route.
func (m *ModuleRouter) Read(store string, key []byte) ([]byte, bool, error) {
	r, ok := m.moduleToRoute[store]
	if !ok {
		return nil, false, fmt.Errorf("module %q is not registered with any Route", store)
	}
	return r.reader(store, key)
}

func (m *ModuleRouter) GetProof(store string, key []byte) (*ics23.CommitmentProof, error) {
	r, ok := m.moduleToRoute[store]
	if !ok {
		return nil, fmt.Errorf("module %q is not registered with any Route", store)
	}
	if r.proofBuilder == nil {
		return nil, fmt.Errorf("proof builder not supported for store %q", store)
	}
	return r.proofBuilder(store, key)
}

func (m *ModuleRouter) Iterator(store string, start []byte, end []byte, ascending bool) (db.Iterator, error) {
	r, ok := m.moduleToRoute[store]
	if !ok {
		return nil, fmt.Errorf("module %q is not registered with any Route", store)
	}
	if r.iteratorBuilder == nil {
		return nil, fmt.Errorf("iterator builder not supported for store %q", store)
	}
	return r.iteratorBuilder(store, start, end, ascending)
}
