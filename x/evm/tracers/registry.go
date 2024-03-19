package tracers

// PR_REVIEW_NOTE: I defined a global registry to make it easy to register tracer.
// Let me know if you guys prefer to not have a global registry and instead register
// tracers explicitly, maybe in the `app/app.go` file.
var GlobalLiveTracerRegistry = NewLiveTracerRegistry()

type LiveTracerRegistry interface {
	GetFactoryByID(id string) (BlockchainTracerFactory, bool)
	Register(id string, factory BlockchainTracerFactory)
}

var _ LiveTracerRegistry = (*liveTracerRegistry)(nil)

func NewLiveTracerRegistry() LiveTracerRegistry {
	return &liveTracerRegistry{
		tracers: make(map[string]BlockchainTracerFactory),
	}
}

type liveTracerRegistry struct {
	tracers map[string]BlockchainTracerFactory
}

// Register implements LiveTracerRegistry.
func (r *liveTracerRegistry) Register(id string, factory BlockchainTracerFactory) {
	r.tracers[id] = factory
}

// GetFactoryByID implements LiveTracerRegistry.
func (r *liveTracerRegistry) GetFactoryByID(id string) (BlockchainTracerFactory, bool) {
	v, found := r.tracers[id]
	return v, found
}
