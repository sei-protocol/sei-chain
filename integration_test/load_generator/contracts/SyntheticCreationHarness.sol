// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

/**
 * Performs a bounded CREATE/CREATE2 with safe generated initcode. The generated
 * runtime is STOP-filled and matches source code-size pressure without executing
 * untrusted Pacific initcode.
 */
contract SyntheticCreationHarness {
    uint256 public constant MAX_RUNTIME_BYTES = 24_576;
    uint256 public constant MAX_INITCODE_BYTES = 49_152;
    uint256 public constant MAX_STORES = 16;
    uint256 public constant MAX_GAS_BURN = 100_000;
    uint256 private entered;

    error InvalidCreationShape();
    error CreationFailed();
    error Reentrant();

    event SyntheticContractCreated(
        address indexed created,
        bool indexed usedCreate2,
        uint256 runtimeBytes,
        uint256 initcodeBytes,
        bytes32 salt
    );

    function deploy(
        uint16 runtimeBytes,
        uint16 stores,
        uint32 gasBurn,
        uint32 requestedInitcodeBytes,
        bool useCreate2,
        bytes32 salt
    ) external payable returns (address created) {
        if (entered != 0) revert Reentrant();
        if (
            runtimeBytes == 0 ||
            runtimeBytes > MAX_RUNTIME_BYTES ||
            stores > MAX_STORES ||
            gasBurn > MAX_GAS_BURN
        ) revert InvalidCreationShape();

        entered = 1;
        bytes32 digest = keccak256(abi.encodePacked(msg.sender, salt, runtimeBytes));
        for (uint256 i; i < stores; i++) {
            bytes32 key = keccak256(abi.encodePacked("synthetic-create", salt, i));
            bytes32 value = keccak256(abi.encodePacked(digest, block.number, i));
            assembly {
                sstore(key, value)
            }
        }
        uint256 startGas = gasleft();
        while (startGas - gasleft() < gasBurn && gasleft() > 150_000) {
            digest = keccak256(abi.encodePacked(digest));
        }

        bytes memory initcode = _initcode(runtimeBytes, requestedInitcodeBytes);
        if (useCreate2) {
            assembly {
                created := create2(callvalue(), add(initcode, 0x20), mload(initcode), salt)
            }
        } else {
            assembly {
                created := create(callvalue(), add(initcode, 0x20), mload(initcode))
            }
        }
        if (created == address(0)) revert CreationFailed();
        entered = 0;
        emit SyntheticContractCreated(created, useCreate2, runtimeBytes, initcode.length, salt);
    }

    function _initcode(
        uint256 runtimeBytes,
        uint256 requestedInitcodeBytes
    ) private pure returns (bytes memory code) {
        uint256 headerBytes = 15;
        uint256 minimum = headerBytes + runtimeBytes;
        uint256 length = requestedInitcodeBytes < minimum ? minimum : requestedInitcodeBytes;
        if (length > MAX_INITCODE_BYTES) revert InvalidCreationShape();
        uint256 runtimeOffset = length - runtimeBytes;
        code = new bytes(length);

        // PUSH2 runtimeBytes; PUSH2 runtimeOffset; PUSH1 0; CODECOPY;
        // PUSH2 runtimeBytes; PUSH1 0; RETURN. Remaining bytes are STOP.
        code[0] = 0x61;
        code[1] = bytes1(uint8(runtimeBytes >> 8));
        code[2] = bytes1(uint8(runtimeBytes));
        code[3] = 0x61;
        code[4] = bytes1(uint8(runtimeOffset >> 8));
        code[5] = bytes1(uint8(runtimeOffset));
        code[6] = 0x60;
        code[7] = 0x00;
        code[8] = 0x39;
        code[9] = 0x61;
        code[10] = bytes1(uint8(runtimeBytes >> 8));
        code[11] = bytes1(uint8(runtimeBytes));
        code[12] = 0x60;
        code[13] = 0x00;
        code[14] = 0xf3;
    }
}
