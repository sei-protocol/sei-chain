// Package experimental is the feature-flag framework for configuration values that change
// shape between binaries.
//
// A key declared here is outside the schema contract. It carries no fingerprint entry, needs
// no migration, and makes no compatibility promise across releases. An unrecognized key is
// reported and left in place rather than halting a boot, so a rollback does not lose it.
// Promoting a key to the stable registry is the commitment, and it changes the fingerprint.
//
// A team ships a value by declaring it at package scope and reading it back typed:
//
//	var OCCWorkers = experimental.Int("giga.executor.occ_worker_count", 8, experimental.Owner("giga"))
//
//	n := OCCWorkers.Get(appOpts)
//
// Check is the boot-time pass that reports what an operator wrote and this binary cannot use.
// It runs under both configuration managers, because these values are the unblock for changes
// that happen between binaries and cannot wait for the new manager to become the default.
package experimental
