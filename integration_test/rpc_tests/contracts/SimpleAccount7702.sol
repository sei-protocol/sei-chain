// SPDX-License-Identifier: MIT
pragma solidity ^0.8.28;

/**
 * Minimal EIP-7702 delegation target. An EOA delegates to this implementation via
 * a type-4 SetCode transaction, after which calls to the EOA execute this code in
 * the EOA's context (so `address(this)` is the EOA). `executeBatch` lets the
 * delegated account perform a list of calls atomically — enough for the RPC
 * suite's 7702 parity specs.
 */
contract SimpleAccount7702 {
    struct Call {
        address target;
        uint256 value;
        bytes data;
    }

    event BatchExecuted(uint256 count);

    function executeBatch(Call[] calldata calls) external payable {
        for (uint256 i = 0; i < calls.length; i++) {
            Call calldata c = calls[i];
            (bool ok, bytes memory ret) = c.target.call{value: c.value}(c.data);
            if (!ok) {
                assembly {
                    revert(add(ret, 0x20), mload(ret))
                }
            }
        }
        emit BatchExecuted(calls.length);
    }

    receive() external payable {}
}
