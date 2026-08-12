// Package experimental is the feature-flag framework for configuration values that change
// shape between binaries.
//
// A team declares a value at package scope and reads it back typed:
//
//	var OCCWorkers = experimental.Int("giga.executor.occ_worker_count", 8,
//	    experimental.WithOwner("giga"))
//
//	n := OCCWorkers.Get(appOpts)
//
// # Lifecycle
//
// A key moves through four steps, and the order is the contract.
//
//	declare   at package init, through Int, String or Bool. Registration is the whole cost.
//	check     at boot, after the configuration handler has populated its source. Check
//	          reports what this binary cannot use. It never halts.
//	report    by the configuration manager, which owns the decision not to halt.
//	promote   into the stable registry once the value stabilises, which changes the schema
//	          fingerprint and forces a schema bump in the same change.
//
// # Ownership
//
// This package owns the registry, the finding, and what counts as usable. The configuration
// manager owns emitting a finding and the decision never to halt on one. Nothing here writes,
// resolves, or refuses a boot.
//
// # Type roles
//
//	Handle[T]  a declared key. A team constructs one at package scope and reads it with Get.
//	Declared   the registry's type-erased view, for callers that walk every key.
//	Finding    one thing the check pass saw. The manager renders it.
//	Source     a configuration source the check pass reads. A *viper.Viper satisfies it.
//	FlatView   the narrow read interface Handle.Get takes, so a read site is not forced to
//	           accept a type it does not use.
//
// # Invariants
//
// Three, and each has a guard named beside it in the tests.
//
// Every declared key is lower case. The key space resolves case-insensitively, so register
// panics on a mixed-case key rather than letting it register as one spelling and resolve as
// another. Before this guard existed, such a key reported to an operator as undeclared while
// its value was silently discarded.
//
// The check pass walks the declared set, not the written set. A value can reach Handle.Get
// through a channel that does not enumerate, the environment being the one that matters, so a
// pass driven by what was written cannot see it. This is what makes Get's fall back to a
// declared default a reported substitution rather than a silent one.
//
// An unrecognized finding never carries an error. Callers distinguish the two by that, and an
// unrecognized key must never be the reason anything halts.
//
// # Zero values and sentinels
//
// A nil raw value means the key is absent, so the declared default applies. An empty string is
// not absent: for a non-string type it is rejected, because cast reads it as zero and blanking
// a limit would silently pin it to zero. A declaration with no owner registers and reports its
// owner as "unknown", so an unowned key is still better registered than absent.
//
// # Concurrency
//
// The registry is guarded by a mutex. Declarations land during package initialisation; Keys,
// Lookup and Check are read-only afterwards.
//
// # What this package is not
//
// An experimental key is outside the schema contract. It carries no fingerprint entry, needs
// no migration, and makes no compatibility promise across releases. An unrecognized key is
// reported and left in place rather than halting a boot, so a rollback does not lose it.
//
// An experimental key must not affect the state transition. Nothing here can enforce that, and
// the framework's own properties compose badly against it: node operators choose whether these
// keys run on a validator, no fingerprint covers them, and nothing may halt. A value that
// changes execution must be a stable key with the versioning ceremony that carries.
//
// These semantics ship under both configuration managers rather than behind the new manager's
// flag, because they are the unblock for values that change between binaries and cannot wait
// for that manager to become the default.
package experimental
