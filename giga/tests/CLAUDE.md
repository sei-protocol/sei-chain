# State Tests Documentation

## Overview

This directory contains state tests that compare Giga vs V2 EVM execution.

- **Test data location:** `giga/tests/data/`
- **Skip list:** `giga/tests/data/skip_list.json`

## Running Tests

### Basic Commands

```bash
# Run all non-skipped tests
go test -v -run TestGigaVsV2_StateTests ./giga/tests/...

# Run specific category
STATE_TEST_DIR=stExample go test -v -run TestGigaVsV2_StateTests ./giga/tests/...

# Run specific test within category
STATE_TEST_DIR=stExample STATE_TEST_NAME=add11 go test -v -run TestGigaVsV2_StateTests ./giga/tests/...

# Ignore skip list (run skipped tests)
IGNORE_SKIP_LIST=true STATE_TEST_DIR=stCreate2 go test -v -run TestGigaVsV2_StateTests ./giga/tests/...

# Use regular KVStore (for debugging)
USE_REGULAR_STORE=true STATE_TEST_DIR=stChainId go test -v -run TestGigaVsV2_StateTests ./giga/tests/...
```

## Running Tests in Parallel (Best Practices)

### Critical Learnings

- Running too many parallel test processes causes PebbleDB panics
- Limit to **2-4 parallel test processes** maximum
- Use `-timeout 20m` or longer for large categories
- Categories that timeout at 20min: stPreCompiledContracts, stStaticCall, stBadOpcode, stEIP1559, stQuadraticComplexityTest, stTimeConsuming

### Recommended Approach

```bash
# Run 2-3 categories in parallel with timeouts
STATE_TEST_DIR=stCategory1 go test -v -run TestGigaVsV2_StateTests ./giga/tests/... -timeout 30m 2>&1 | tee /tmp/results_stCategory1.log &
STATE_TEST_DIR=stCategory2 go test -v -run TestGigaVsV2_StateTests ./giga/tests/... -timeout 30m 2>&1 | tee /tmp/results_stCategory2.log &
wait
```

## Extracting Failing Test Names

### From test output logs

```bash
# Count passes and failures
grep -e "PASS:" results.log | grep "stCategory" | wc -l
grep -e "FAIL:" results.log | grep "stCategory" | wc -l

# List specific failing tests (shows test name)
grep -e "FAIL:" results.log | grep "stCategory"

# Check for specific failure types in output
grep "gas_mismatch\|result_code\|state_mismatch\|balance_mismatch" results.log
```

## Skip List Format

**File:** `giga/tests/data/skip_list.json`

```json
{
  "skipped_tests": {
    "category/relPath.json/FullTestNameFromJSON/index": "reason"
  },
  "skipped_categories": [
    "stTimeConsuming"
  ],
  "skipped_category_reasons": {
    "stCategory": "ANALYZED: X/Y tests pass. N failures: type1(count), type2(count)"
  }
}
```

### Test Name Format for `skipped_tests`

The test name key must match the exact format used internally. Get the correct format from the test summary output:

```bash
# Run tests and look for failure summary lines starting with "- category/..."
grep -E "^\s+- category/" /tmp/results.log
```

**Format:** `category/relPath/FullJSONKey/index`

- `category` - The STATE_TEST_DIR value (e.g., `Shanghai`, `stTransactionTest`)
- `relPath` - Relative path from category dir to the .json file (e.g., `subdir/test.json` or just `test.json`)
- `FullJSONKey` - The full key from inside the JSON file (includes `GeneralStateTests/...`)
- `index` - Optional subtest index (e.g., `/5`) if the test has multiple post states

**Examples:**

```json
{
  "skipped_tests": {
    "Shanghai/stEIP3651-warmcoinbase/coinbaseWarmAccountCallGas.json/GeneralStateTests/Shanghai/stEIP3651-warmcoinbase/coinbaseWarmAccountCallGas.json::coinbaseWarmAccountCallGas-fork_[Cancun-Prague]-d[0-7]g0v0/5": "gas_mismatch",
    "stTransactionTest/HighGasPriceParis.json/GeneralStateTests/stTransactionTest/HighGasPriceParis.json::HighGasPriceParis-fork_[Cancun-Prague]-d0g0v0": "fee_out_of_bound"
  }
}
```

### Per-test vs Per-category skipping

- Use `skipped_tests` for specific failing tests (allows passing tests to run)
- Use `skipped_categories` only for categories not yet analyzed or with extensive failures
- `skipped_category_reasons` tracks analysis status (use "ENABLED:" prefix for categories with individual test skips)

## Failure Types

| Type | Description |
|------|-------------|
| `result_code` | Transaction success/failure mismatch |
| `gas_mismatch` | Gas calculation differences |
| `state_mismatch` | Storage value differences |
| `balance_mismatch` | Balance differences |
| `code_mismatch` | Contract code differences |
| `nonce_mismatch` | Account nonce differences |
| `v2_error` | V2 execution error |
| `giga_error` | Giga execution error |

## Test Categories Status

### Categories with 100% pass rate (removed from skip list)

- stExample, stSLoadTest, stChainId, stCodeCopyTest, stExpectSection, stEIP158Specific
- stLogTests, stShift, stHomesteadSpecific, stAttackTest, stRecursiveCreate

### Categories enabled with individual test skips

- Shanghai (26/27 passing, 1 skipped)
- stArgsZeroOneBalance (91/96 passing, 5 skipped)
- stTransactionTest (248/259 passing, 11 skipped)
- stSpecialTest (21/22 passing, 1 skipped)
- stSolidityTest (21/23 passing, 2 skipped)
- stNonZeroCallsTest (21/24 passing, 3 skipped)

### Categories needing longer timeout (>20min)

- stPreCompiledContracts, stStaticCall, stBadOpcode, stEIP1559
- stQuadraticComplexityTest, stTimeConsuming

## Workflow for Analyzing New Categories

1. **Run test with `IGNORE_SKIP_LIST=true`:**
   ```bash
   IGNORE_SKIP_LIST=true STATE_TEST_DIR=stNewCategory go test -v -run TestGigaVsV2_StateTests ./giga/tests/... -timeout 30m 2>&1 | tee /tmp/results.log
   ```

2. **Extract results:**
   ```bash
   PASSES=$(grep -e "PASS:" /tmp/results.log | grep "stNewCategory" | wc -l)
   FAILS=$(grep -e "FAIL:" /tmp/results.log | grep "stNewCategory" | wc -l)
   echo "Pass: $PASSES, Fail: $FAILS"
   ```

3. **If 100% pass:** Remove from `skipped_categories`

4. **If failures:** Extract failing test names from summary output and add to `skipped_tests`:
   ```bash
   # Get exact test names from the summary section
   grep -E "^\s+- stNewCategory/" /tmp/results.log
   ```

5. **Update skip_list.json:**
   - Add failing tests to `skipped_tests` with failure reason
   - Remove category from `skipped_categories`
   - Update `skipped_category_reasons` with "ENABLED:" prefix and summary

6. **Verify tests pass:**
   ```bash
   STATE_TEST_DIR=stNewCategory go test -v -run TestGigaVsV2_StateTests ./giga/tests/... -timeout 10m
   ```

7. **Commit and push after each batch:**
   ```bash
   git add giga/tests/data/skip_list.json
   git commit -m "Enable stNewCategory state tests (X/Y passing)"
   git push
   ```
