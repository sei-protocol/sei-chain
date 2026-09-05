// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

/**
 * A synthetic workload target. It can reproduce calldata size, approximate
 * execution gas, emit logs, and optionally perform bounded state writes.
 */
contract ProfileLoadHarness {
    mapping(bytes32 => bytes32) public workloadState;

    event Workload(bytes32 indexed digest, uint256 targetGas, uint256 actualGas, bytes payload);

    function run(
        uint256 targetExecutionGas,
        uint256 stateWrites,
        uint256 salt,
        bytes calldata payload
    ) external {
        _run(targetExecutionGas, stateWrites, salt, payload);
    }

    /**
     * Shape-replay entrypoint. Callers can preserve an observed four-byte
     * selector and exact calldata length while placing target gas and state
     * write controls in the next two ABI words:
     *
     * selector | targetExecutionGas | stateWrites | arbitrary padding
     *
     * Short payloads still replay their exact byte length and use a bounded
     * calldata-derived gas target.
     */
    fallback() external payable {
        uint256 targetExecutionGas;
        uint256 stateWrites;
        if (msg.data.length >= 68) {
            assembly {
                targetExecutionGas := calldataload(4)
                stateWrites := calldataload(36)
            }
        } else {
            targetExecutionGas = msg.data.length * 64;
        }
        _run(targetExecutionGas, stateWrites, uint256(keccak256(msg.data)), msg.data);
    }

    receive() external payable {}

    function _run(
        uint256 targetExecutionGas,
        uint256 stateWrites,
        uint256 salt,
        bytes calldata payload
    ) internal {
        uint256 startGas = gasleft();
        bytes32 digest = keccak256(abi.encodePacked(msg.sender, salt, payload));

        for (uint256 i = 0; i < stateWrites && gasleft() > 80_000; i++) {
            bytes32 key = keccak256(abi.encodePacked(msg.sender, salt, i));
            workloadState[key] = digest;
        }

        // Keep mutating a value consumed by the event so the optimizer cannot
        // remove the loop. Leave enough gas to emit the event and return.
        while (startGas - gasleft() < targetExecutionGas && gasleft() > 80_000) {
            digest = keccak256(abi.encodePacked(digest, salt));
        }

        emit Workload(digest, targetExecutionGas, startGas - gasleft(), payload);
    }
}
