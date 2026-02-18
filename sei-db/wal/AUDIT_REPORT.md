# WAL Implementation Security & Correctness Audit

**Audit Date:** February 18, 2026  
**Scope:** `sei-db/wal/wal.go` and related files  
**Context:** Consensus-critical code usage  
**Auditor:** Automated Security Review

---

## Executive Summary

This audit identifies **critical durability and correctness issues** that make this WAL implementation **unsuitable for consensus-critical applications** in its current form. The most severe issues relate to disabled filesystem sync, potential deadlocks in async mode, and race conditions during concurrent operations.

---

## Critical Issues (Must Fix for Consensus Use)

### 1. ⛔ CRITICAL: `NoSync: true` Disables Durability Guarantees

**Location:** `wal.go:58-61`, `utils.go:24-26`

```go
log, err := open(dir, &wal.Options{
    NoSync: true,
    NoCopy: true,
})
```

**Impact:** 
- Data written to the WAL is NOT guaranteed to reach persistent storage
- A system crash (power failure, kernel panic) can lose acknowledged writes
- **For consensus code, this violates the fundamental WAL contract** - nodes may think they've persisted a vote or block proposal when they haven't

**Recommendation:**
- Remove `NoSync: true` or make it configurable with safe defaults
- Add an explicit `Sync()` method for forcing durability
- Consider making sync behavior configurable per-write for performance optimization

---

### 2. ⛔ CRITICAL: Potential Deadlock in Async Write Mode

**Location:** `wal.go:88-119`, `wal.go:124-148`

**Scenario:**
1. Async writer goroutine encounters an error (marshal failure, disk error)
2. Goroutine records error via `recordAsyncWriteErr()` and exits
3. The `writeChannel` is NOT closed
4. If channel buffer is full when error occurs, subsequent `Write()` calls will:
   - Acquire `mtx` lock
   - Check `getAsyncWriteErrLocked()` - no error yet (timing window)
   - Block on `walLog.writeChannel <- entry` (buffer full, no reader)
   - Hold `mtx` lock forever
5. `Close()` needs `mtx` → **DEADLOCK**

**Code Path:**
```go
func (walLog *WAL[T]) Write(entry T) error {
    walLog.mtx.Lock()         // Acquires lock
    defer walLog.mtx.Unlock()
    // ... error check may pass due to timing
    walLog.writeChannel <- entry  // BLOCKS if buffer full and reader exited
}
```

**Impact:** System hangs; consensus halts indefinitely

**Recommendation:**
- Use a select with closeCh check when sending to channel
- Or use a separate mutex for channel operations
- Consider closing the channel when async goroutine exits on error

---

### 3. ⛔ CRITICAL: `NoCopy: true` Causes Data Corruption Risk

**Location:** `wal.go:60`

```go
NoCopy: true,
```

**Impact:**
- The underlying tidwall/wal library will NOT copy data passed to `Write()`
- If the caller modifies the byte slice after `Write()` returns but before the data is flushed, **data corruption occurs**
- In async mode, this is particularly dangerous since write happens later

**Recommendation:**
- Remove `NoCopy: true` for safety
- Or ensure all callers understand they must not reuse buffers

---

### 4. ⛔ CRITICAL: No Synchronization for Truncate Operations

**Location:** `wal.go:150-161`

```go
func (walLog *WAL[T]) TruncateAfter(index uint64) error {
    return walLog.log.TruncateBack(index)  // No lock!
}

func (walLog *WAL[T]) TruncateBefore(index uint64) error {
    return walLog.log.TruncateFront(index)  // No lock!
}
```

**Impact:**
- Concurrent `Write()` and `TruncateAfter()` can cause undefined behavior
- The async writer reads `LastIndex()` and writes to `nextOffset` without coordination with truncation
- Comment on line 158 acknowledges this: "Need to add write lock because this would change the next write offset" - **but no lock is implemented**

**Race Scenario:**
1. Async writer: calls `NextOffset()` → gets `lastOffset = 5`
2. Concurrent: `TruncateAfter(3)` executes
3. Async writer: calls `Write(6, data)` → writes to invalid position

**Recommendation:**
- Acquire `mtx` write lock in `TruncateAfter()` and `TruncateBefore()`
- Ensure async writer drains before truncation completes

---

### 5. ⛔ CRITICAL: No Write Durability Confirmation in Async Mode

**Location:** `wal.go:98-104`

```go
if writeBufferSize > 0 {
    // ...
    walLog.writeChannel <- entry  // Returns immediately after queuing
}
```

**Impact:**
- `Write()` returns success when entry is queued, NOT when persisted
- Caller has no way to know if/when write completed
- **For consensus: a node might vote, "persist" to WAL, crash, and lose the vote**

**Recommendation:**
- Add a `WriteSync(entry T) error` method that waits for confirmation
- Or add a `Flush()` method that waits for all pending writes
- Consider a callback mechanism for write completion

---

## High Severity Issues

### 6. 🔴 HIGH: Async Writer Silently Exits on Error

**Location:** `wal.go:129-145`

```go
for entry := range ch {
    bz, err := walLog.marshal(entry)
    if err != nil {
        walLog.recordAsyncWriteErr(err)
        return  // Exits silently, entries queued after error are lost
    }
    // ...
}
```

**Impact:**
- After the first error, all subsequent entries queued in the channel are silently dropped
- The error is only reported to the *next* caller of `Write()`, not to callers whose entries were dropped

**Recommendation:**
- Drain the channel and record which entries failed
- Consider retry logic for transient errors
- Log all dropped entries

---

### 7. 🔴 HIGH: Pruning Runs Without Write Coordination

**Location:** `wal.go:213-243`

```go
func (walLog *WAL[T]) startPruning(keepRecent uint64, pruneInterval time.Duration) {
    // ...
    if err := walLog.TruncateBefore(prunePos); err != nil {
        // ...
    }
}
```

**Impact:**
- Background pruning calls `TruncateBefore()` without acquiring any locks
- Can race with reads, writes, and other truncations
- May cause `Replay()` to fail mid-operation if entries are pruned during replay

**Recommendation:**
- Coordinate pruning with the write lock
- Consider pausing pruning during critical operations

---

### 8. 🔴 HIGH: No Integrity Verification of Entries

**Location:** All read paths

**Impact:**
- No checksums or integrity verification at the application level
- Relies entirely on tidwall/wal for corruption detection
- Bit-rot or partial corruption may go undetected if tidwall/wal doesn't catch it

**Recommendation:**
- Add CRC32/SHA256 checksums to entries
- Verify checksums on read
- Consider adding version/magic bytes to detect format mismatches

---

## Medium Severity Issues

### 9. 🟡 MEDIUM: Silent Corruption Recovery May Mask Issues

**Location:** `wal.go:296-327`, `utils.go:36-62`

```go
if errors.Is(err, wal.ErrCorrupt) {
    // try to truncate corrupted tail
    // ...
}
```

**Impact:**
- Silently truncating corrupted data hides potentially serious issues
- Lost consensus data could cause chain forks or stalls
- No notification mechanism for operators

**Recommendation:**
- Log corruption events at ERROR level with details
- Consider failing by default and requiring explicit recovery mode
- Provide metrics/alerts for corruption events

---

### 10. 🟡 MEDIUM: `Replay()` Has No Cancellation Support

**Location:** `wal.go:194-211`

```go
func (walLog *WAL[T]) Replay(start uint64, end uint64, processFn func(...) error) error {
    for i := start; i <= end; i++ {
        // No context, no cancellation
    }
}
```

**Impact:**
- Long replays cannot be cancelled
- No timeout mechanism
- Could block shutdown indefinitely

**Recommendation:**
- Accept `context.Context` parameter
- Check context cancellation in loop

---

### 11. 🟡 MEDIUM: No Entry Size Limits

**Location:** `wal.go:87-119`, `wal.go:129-145`

**Impact:**
- Unbounded entry sizes can cause OOM
- Large entries could exhaust buffer memory
- No protection against malformed inputs

**Recommendation:**
- Add configurable maximum entry size
- Validate entry size before queuing/writing

---

### 12. 🟡 MEDIUM: Integer Overflow in Truncation Logic

**Location:** `utils.go:41-59`

```go
var pos int
for len(data) > 0 {
    // ...
    pos += n
}
```

**Impact:**
- On 32-bit systems or very large files, `pos` could overflow
- Would cause incorrect truncation position

**Recommendation:**
- Use `int64` for position tracking

---

## Low Severity Issues

### 13. 🟢 LOW: Unused Helper Function

**Location:** `utils.go:94-109`

```go
func channelBatchRecv[T any](ch <-chan T) []T {
    // ...
}
```

**Impact:** Dead code, no functional issue

---

### 14. 🟢 LOW: Inconsistent Error Wrapping

**Location:** Various

Some errors use `fmt.Errorf("...: %w", err)` while others use `fmt.Errorf("... %w", err)`. Inconsistent formatting.

---

## Recommendations for Consensus-Critical Use

### Immediate Actions Required

1. **Enable fsync by default** - Remove `NoSync: true`
2. **Add `WriteSync()` method** - Provide durability confirmation
3. **Fix truncation locking** - Add proper synchronization
4. **Fix async deadlock** - Use select with timeout/cancel
5. **Remove `NoCopy: true`** - Eliminate buffer reuse corruption risk

### Architectural Improvements

1. **Add checksums** - CRC32 at minimum for all entries
2. **Add write confirmation** - Callback or sync mechanism for async mode
3. **Add context support** - For cancellation and timeouts
4. **Add metrics** - Write latency, queue depth, errors
5. **Add explicit Sync()/Flush()** - Force durability when needed

### Testing Gaps

1. No crash recovery tests with power-loss simulation
2. No fuzzing for corruption handling
3. Limited concurrent stress testing
4. No tests for the deadlock scenario described above

---

## Conclusion

**This WAL implementation is NOT safe for consensus-critical use in its current form.** The combination of disabled fsync, async mode deadlock potential, and missing synchronization for truncations creates unacceptable risk for blockchain consensus where data integrity and durability are paramount.

The implementation appears designed for performance-oriented use cases where some data loss is acceptable. For consensus use, significant hardening is required.

---

## Appendix: Affected Code Locations Summary

| Issue | File | Lines | Severity |
|-------|------|-------|----------|
| NoSync | wal.go | 58-61 | Critical |
| Deadlock | wal.go | 88-119, 245-262 | Critical |
| NoCopy | wal.go | 60 | Critical |
| Truncate race | wal.go | 150-161 | Critical |
| No durability confirm | wal.go | 98-104 | Critical |
| Silent drop | wal.go | 129-145 | High |
| Pruning race | wal.go | 213-243 | High |
| No checksums | All | N/A | High |
| Silent recovery | wal.go | 296-327 | Medium |
| No cancellation | wal.go | 194-211 | Medium |
| No size limits | wal.go | 87-119 | Medium |
| Integer overflow | utils.go | 41-59 | Medium |
