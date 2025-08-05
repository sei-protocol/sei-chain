package types

// TxTracer is an interface for tracing transactions generic
// enough to be used by any transaction processing engine be it
// CoWasm or EVM.
//
// The TxTracer responsibility is to inject itself in the context
// that will be used to process the transaction. How the context
// will be used afterward is up to the transaction processing engine.
//
// Today, only EVM transaction processing engine do something with the
// TxTracer (it inject itself into the EVM execution context for
// go-ethereum level tracing).
//
// The TxTracer receives signals from the scheduler when the tracer
// should be reset because the transaction is being re-executed and
// when the transaction is committed.
type TxTracer interface {
	// InjectInContext injects the transaction specific tracer in the context
	// that will be used to process the transaction.
	//
	// For now only the EVM transaction processing engine uses the tracer
	// so it only make sense to inject an EVM tracer. Future updates might
	// add the possibility to inject a tracer for other transaction kind.
	//
	// Which tracer implementation to provied and how will be retrieved later on
	// from the context is dependent on the transaction processing engine.
	InjectInContext(ctx Context) Context

	// Reset is called when the transaction is being re-executed and the tracer
	// should be reset. A transaction executed by the OCC parallel engine might
	// be re-executed multiple times before being committed, each time `Reset`
	// will be called.
	//
	// When Reset is received, it means everything that was traced before should
	// be discarded.
	Reset()

	// Commit is called when the transaction is committed. This is the last signal
	// the tracer will receive for a given transaction. After this call, the tracer
	// should do whatever it needs to forward the tracing information to the
	// appropriate place/collector.
	Commit()
}
