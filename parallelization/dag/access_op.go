package dag

import (
	"context"
	"fmt"
	"sync"
)

// AccessType describes whether an access operation is a read or a write.
type AccessType uint8

const (
	// AccessTypeRead represents a read-only access to a resource.
	AccessTypeRead AccessType = iota + 1
	// AccessTypeWrite represents a mutating access to a resource.
	AccessTypeWrite
)

// ResourceID uniquely identifies a resource that can participate in the access DAG.
type ResourceID string

// AccessOp models an ordered access (read or write) over a resource.
//
// Each operation keeps track of the dependency channels that must be satisfied before the
// access can safely proceed. Once the access completes, the operation signals dependents
// through its completion channel.
type AccessOp struct {
	name     string
	resource ResourceID
	access   AccessType

	parent *TxContext

	waitFor []<-chan struct{}
	done    chan struct{}

	once sync.Once
}

// NewAccessOp creates a new access operation for the provided resource.
func NewAccessOp(name string, resource ResourceID, access AccessType) *AccessOp {
	return &AccessOp{
		name:     name,
		resource: resource,
		access:   access,
		done:     make(chan struct{}),
	}
}

// Name returns the debug name for the access operation.
func (op *AccessOp) Name() string {
	return op.name
}

// Resource returns the resource identifier associated with the access operation.
func (op *AccessOp) Resource() ResourceID {
	return op.resource
}

// Access returns the access type (read or write).
func (op *AccessOp) Access() AccessType {
	return op.access
}

// parentTx returns the transaction context that owns the access operation.
func (op *AccessOp) parentTx() *TxContext {
	return op.parent
}

// setParent updates the parent transaction for the access operation.
func (op *AccessOp) setParent(tx *TxContext) {
	op.parent = tx
}

// resetDependencies clears all the dependencies recorded for the operation.
func (op *AccessOp) resetDependencies() {
	op.waitFor = nil
}

// AddDependency registers a dependency on another access operation. The dependency is ignored
// if the provided operation is nil.
func (op *AccessOp) AddDependency(dep *AccessOp) {
	if dep == nil {
		return
	}
	op.waitFor = append(op.waitFor, dep.Done())
}

// Wait blocks until all dependencies for the access operation have completed or the context is cancelled.
func (op *AccessOp) Wait(ctx context.Context) error {
	for _, dep := range op.waitFor {
		select {
		case <-ctx.Done():
			return fmt.Errorf("access op %s wait cancelled: %w", op.name, ctx.Err())
		case <-dep:
		}
	}
	return nil
}

// Signal notifies dependents that the access operation has finished executing.
func (op *AccessOp) Signal() {
	op.once.Do(func() {
		close(op.done)
	})
}

// Done returns a read-only channel that is closed once the access operation completes.
func (op *AccessOp) Done() <-chan struct{} {
	return op.done
}

// String implements fmt.Stringer for helpful debug output.
func (op *AccessOp) String() string {
	return fmt.Sprintf("%s(%s:%s)", op.name, op.resource, op.access)
}

// String implements fmt.Stringer for AccessType for readability.
func (t AccessType) String() string {
	switch t {
	case AccessTypeRead:
		return "read"
	case AccessTypeWrite:
		return "write"
	default:
		return "unknown"
	}
}
