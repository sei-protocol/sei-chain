# Trace Gas Check Discrepancy for Historical Failed Transactions

## Issue Summary

The trace gas check validation tool reports failures for certain historical failed transactions where the trace gas value is higher than the receipt gas value:

- Block 187818335: trace: 107046, receipt: 81100 (diff: 25946)
- Block 187822719: trace: 107046, receipt: 81100 (diff: 25946)
- Block 187825939: trace: 636785, receipt: 338566 (diff: 298219)

## Root Cause

This discrepancy is caused by the "double refund" bug that existed in the go-ethereum fork (sei-protocol/go-ethereum) prior to version v1.15.7-sei-14. The bug was fixed in PR #2692.

### Technical Details

In Ethereum, gas refunds are applied for certain storage operations (e.g., SSTORE clearing a slot). For **failed transactions**, refunds should NOT be applied because the transaction reverts.

The bug caused refunds to be incorrectly calculated even for failed transactions, resulting in lower gas values being stored in receipts.

**Before fix (buggy behavior):**
- Failed transaction executes
- Refund counter accumulates values during execution
- Transaction reverts, but refund was incorrectly applied
- Receipt stores lower gas value (gas_used - refund)

**After fix (correct behavior):**
- Failed transaction executes
- Refund counter accumulates values during execution
- Transaction reverts, state (including refund counter) is properly reverted
- Receipt stores correct gas value (gas_used, no refund)

## Impact

- **Historical blocks** (before the fix was deployed) have receipts with incorrect gas values for failed transactions
- **Trace replay** with current code correctly calculates gas (higher value)
- **Validation tools** comparing trace gas with receipt gas will report mismatches

## Affected Blocks

Blocks processed before the deployment of go-ethereum v1.15.7-sei-14 (sei-chain using this version since PR #2692, merged on January 12, 2026) may have this discrepancy for failed transactions.

## Resolution Options

1. **Accept as known discrepancy**: Document that historical failed transactions may have incorrect gas values in receipts
2. **Update validation tool**: Skip gas validation for failed transactions in blocks before the fix
3. **Block height cutoff**: Identify the first block processed with the fix and only validate gas for blocks after that point

## Recommendation

For the trace validation tool, add a check to skip gas comparison for failed transactions (status=0) in blocks before a specific height cutoff corresponding to when the fix was deployed.

## Related Commits

- fix double refund (#2692): `096836c5edd1e25360b4dd4d1eb261267467f8d6`
- go-ethereum v1.15.7-sei-12 -> v1.15.7-sei-14 update

## Verification

The gas difference observed (e.g., 25946) matches expected refund amounts for SSTORE operations, confirming this is the cause.
