// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

/**
 * @title SstoreRefundTest
 * @notice Contract to reproduce the SSTORE double-refund bug in sei-protocol/go-ethereum
 * 
 * The bug is in case 2.2.2.1 ("reset to original inexistent slot") of makeGasSStoreFunc
 * in core/vm/operations_acl.go. When SeiSstoreSetGasEIP2200 is non-nil, BOTH refund
 * calculations run instead of just one, causing a double-refund.
 * 
 * Pattern to trigger case 2.2.2.1:
 *   1. Slot is empty before tx (original = 0)
 *   2. SSTORE sets slot to non-zero (current = X)
 *   3. SSTORE resets slot to 0 (value = 0 = original)
 * 
 * Expected refund (correct): SstoreSetGas - 100 = 19,900
 * Actual refund (buggy):     (SstoreSetGas - 800) + (SstoreSetGas - 100) = 39,100
 */
contract SstoreRefundTest {
    // Storage slots - each starts at 0
    uint256 public slot0;
    uint256 public slot1;
    uint256 public slot2;
    uint256 public slot3;
    uint256 public slot4;

    /**
     * @notice Triggers case 2.2.2.1 exactly ONCE
     * @dev Pattern: 0 -> 1 -> 0 (set then reset to original)
     * 
     * Expected refund delta (buggy vs fixed): 19,200 (extra refund from double-add)
     */
    function triggerSingleRefund() external {
        triggerSingleRefundWithGas(0);
    }

    /**
     * @notice Triggers case 2.2.2.1 with extra gas burn to exceed refund cap
     * @param loops Number of iterations to burn gas
     */
    function triggerSingleRefundWithGas(uint256 loops) public {
        // External self-calls force the intermediate state to be observable.
        this.setSlot0(1); // Case 2.1.1: create slot (0 -> non-zero)

        uint256 acc;
        for (uint256 i = 0; i < loops; i++) {
            acc = uint256(keccak256(abi.encodePacked(acc, i)));
        }
        // Prevent the optimizer from discarding the loop.
        require(acc != type(uint256).max, "burn");

        this.setSlot0(0); // Case 2.2.2.1: reset to original inexistent (triggers bug)
    }

    /**
     * @notice Triggers case 2.2.2.1 multiple times for more pronounced effect
     * @dev Does 5 independent set-reset cycles on different slots
     * 
     * Expected refund delta: 5 * 19,200 = 96,000
     */
    function triggerMultipleRefunds() external {
        // Each cycle triggers the double-refund bug.
        this.setSlot0(1); this.setSlot0(0); // +19,200 extra refund
        this.setSlot1(1); this.setSlot1(0); // +19,200 extra refund
        this.setSlot2(1); this.setSlot2(0); // +19,200 extra refund
        this.setSlot3(1); this.setSlot3(0); // +19,200 extra refund
        this.setSlot4(1); this.setSlot4(0); // +19,200 extra refund
    }

    /**
     * @notice For comparison - triggers case 2.1.2b (delete slot) instead
     * @dev Pattern: pre-set slot to non-zero, then clear in new tx
     * This does NOT trigger the bug since original == current
     */
    function setupForClearRefund() external {
        slot0 = 999;  // Set to non-zero (will be committed)
    }

    function triggerClearRefund() external {
        slot0 = 0;  // Case 2.1.2b: delete slot (original == current != 0, value == 0)
        // This refund uses clearingRefund, not the buggy double-add
    }

    /**
     * @notice Complex pattern with multiple SSTORE operations
     * @dev Mixes different SSTORE cases
     */
    function complexPattern() external {
        // Start fresh - all slots are 0.
        this.setSlot0(1); // 2.1.1: create
        this.setSlot0(2); // 2.2: dirty update (original=0, current=1, value=2)
        this.setSlot0(0); // 2.2.2.1: reset to original inexistent ← BUG TRIGGERS

        this.setSlot1(100); // 2.1.1: create
        this.setSlot1(0);   // 2.2.2.1: reset to original inexistent ← BUG TRIGGERS

        this.setSlot2(42);  // 2.1.1: create
        this.setSlot2(42);  // 1: noop (current == value)
        this.setSlot2(0);   // 2.2.2.1: reset to original inexistent ← BUG TRIGGERS
    }

    // External setters to prevent the compiler from optimizing away intermediate SSTOREs.
    function setSlot0(uint256 value) external { slot0 = value; }
    function setSlot1(uint256 value) external { slot1 = value; }
    function setSlot2(uint256 value) external { slot2 = value; }
    function setSlot3(uint256 value) external { slot3 = value; }
    function setSlot4(uint256 value) external { slot4 = value; }

    /**
     * @notice Reset all slots to 0 for clean test runs
     */
    function resetSlots() external {
        slot0 = 0;
        slot1 = 0;
        slot2 = 0;
        slot3 = 0;
        slot4 = 0;
    }
}
