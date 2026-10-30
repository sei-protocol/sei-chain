// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

/// Emits many small logs in one transaction, for specs that exercise the RPC's log limits.
/// Each log names its batch (indexed, so a topic filter can select one batch) and its
/// position in the batch, so a spec can check a result set for gaps and duplicates.
contract LogBurst {
    event Burst(uint256 indexed batch, uint256 seq);

    function burst(uint256 batch, uint256 count) external {
        for (uint256 i = 0; i < count; i++) {
            emit Burst(batch, i);
        }
    }
}
