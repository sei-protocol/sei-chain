package tracers

// PR_REVIEW_NOTE: I defined a global registry to make it easy to register tracer.
// Let me know if you guys prefer to not have a global registry and instead register
// tracers explicitly, maybe in the `app/app.go` file.
var GlobalLiveTracerRegistry = NewLiveTracerRegistry()

type LiveTracerRegistry interface {
	GetFactoryByID(id string) (BlockchainLoggerFactory, bool)
	Register(id string, factory BlockchainLoggerFactory)
}

var _ LiveTracerRegistry = (*liveTracerRegistry)(nil)

func NewLiveTracerRegistry() LiveTracerRegistry {
	return &liveTracerRegistry{
		tracers: make(map[string]BlockchainLoggerFactory),
	}
}

type liveTracerRegistry struct {
	tracers map[string]BlockchainLoggerFactory
}

// Register implements LiveTracerRegistry.
func (r *liveTracerRegistry) Register(id string, factory BlockchainLoggerFactory) {
	r.tracers[id] = factory
}

// GetFactoryByID implements LiveTracerRegistry.
func (r *liveTracerRegistry) GetFactoryByID(id string) (BlockchainLoggerFactory, bool) {
	v, found := r.tracers[id]
	return v, found
}
