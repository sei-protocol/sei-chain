Within sei-tendermint subdirectory
* use sei-tendermint/libs/utils.Option for optional values, passing nil as function value is not allowed,
    unless explicitly documented (except for maps and slices, for which nil is a valid empty value).
* use sei-tendermint/libs/utils/scope.Run for structured concurrency (instead of waitgroup)
* use new golang features wherever possible, which is at least 1.25. In particular:
    * modern range iteration ("for range N", to iterate N times)
    * avoid capturing loop iterators ("tc := tc" is not needed)
    * use slices, maps libraries whenever they do not harm readability
    * use t.Context() in tests instead of context.Background().
* use public api of types in tests, never malform internal state of types. For read-only access,
  you can use private fields directly in case it is not accessible via public api.
* when writing tests, focus on asserting the publically visible properties, especially the API contract. Avoid asserting implementation details.
* Avoid sleeping and active polling in tests. Prefer waiting for updates (for example by using sei-tendermint/libs/utils.Watch or AtomicWatch).
* Avoid checking human readable error messages in tests. If programatic error type check is requested, use errors.Is/As/AsType instead.
* After introducing changes, you may Use `go test --count=0` to quickly check if tests compile. You may run `go test` to actually run the tests
  afterwards. Prefer running modified tests selectively and run the whole test suite only if requested or looking for failures.
