Within sei-tendermint subdirectory
* sei-tendermint is a root of the go module, but it is not the root of the repo. When refactoring check references across of the whole repo.
* use sei-tendermint/libs/utils.Option for optional values, passing nil as function value is not allowed,
    unless explicitly documented (except for maps and slices, for which nil is a valid empty value).
* struct fields are assumed to be non-nil by default. Do not add defensive nil-checks in internal logic.
  External data, like proto generated code for example, might require nil checks.
* use utils.Recv/utils.Send instead of select { case <-ctx.Done(); ... }
* use strongly typed utils/require asserts instead of testify/require
* use sei-tendermint/libs/utils/scope.Run for structured concurrency (instead of waitgroup).
    * Don't spawn goroutines with plain "go func(){...}"
    * using testing.T assertions (inclusing those from "require" library) is not allowed from non-main test goroutine.
      In particular it is not allowed in Scope.Run() and goroutines spawned via Scope.Spawn (and related ones). On assertion error, return an error
      or panic directly. If you are unsure if a test helper would be called from non-main test goroutine, dont use testing.T assertions.
* use new golang features wherever possible, which is at least 1.25. In particular:
    * modern range iteration ("for range N", to iterate N times)
    * avoid capturing loop iterators ("tc := tc" is not needed)
    * use slices, maps libraries whenever they do not harm readability
    * use t.Context() in tests instead of context.Background().
* use sei-tendermint/libs/utils synchronization primitives: utils.Mutex instead of sync.Mutex, utils.RWMutex instead of sync.RWMutex.
  utils.Mutex/RWMutex are supposed to be parametrized by the data type they protect. Do NOT use utils.Mutex\[struct{}\] as a drop-in replacement
  of sync.Mutex.
* use public api of types in tests, never malform internal state of types. For read-only access,
  you can use private fields directly in case it is not accessible via public api.
* when writing tests, focus on asserting the publically visible properties, especially the API contract. Avoid asserting implementation details.
* Avoid sleeping and active polling in tests. Prefer waiting for updates (for example by using sei-tendermint/libs/utils.Watch or AtomicWatch).
* Do not introduce artificial timeouts in tests because they are a source of flakiness. Let the test hang and make the user run go tests with explicit
  timeout instead.
* Avoid checking human readable error messages in tests. If programatic error type check is requested, use errors.Is/As/AsType instead.
* After introducing changes, you may Use `go test --count=0` to quickly check if tests compile. You may run `go test` to actually run the tests
  afterwards. Prefer running modified tests selectively and run the whole test suite only if requested or looking for failures.
* Proto fields: For hashable messages, use explicit "optional" for all signular fields and annotate semantically required fields with `// required`
  and truly optional fields with `// optional`. Example:
    optional uint64 index = 1; // required
    optional AppProposal app = 4; // optional
  Required fields must be nil-checked at the boundary (constructor and proto Decode) and trusted
* Avoid removing comments and logs which are not obviously obsolete. Keep the original wording, only fixing mistakes or obsolete parts.
* TestRng instance should be one per test, constructed directly in the test function. In case of nested/table tests, each nested test should create its own instance. 
  Use TestRng.Split() (before spawning) if you need to pass entropy source to a spawned goroutine
  to ensure deterministic entropy across the goroutines.
