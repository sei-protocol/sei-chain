// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

/// @title BLSCheck - EIP-2537 BLS12-381 precompile verification contract
/// @notice Calls BLS precompiles using raw staticcall per EIP-2537 spec
contract BLSCheck {
    // EIP-2537 precompile addresses
    address constant G1ADD   = address(0x0b);
    address constant G1MSM   = address(0x0c);
    address constant G2ADD   = address(0x0d);
    address constant G2MSM   = address(0x0e);
    address constant PAIRING = address(0x0f);
    address constant MAP_FP_TO_G1  = address(0x10);
    address constant MAP_FP2_TO_G2 = address(0x11);

    /// @notice Tests G1 point addition with identity points (point at infinity)
    /// @return true if precompile executes correctly
    function checkG1Add() external view returns (bool) {
        // Two G1 identity points (128 bytes each, all zeros) = 256 bytes
        bytes memory input = new bytes(256);
        (bool success, bytes memory result) = G1ADD.staticcall(input);
        return success && result.length == 128;
    }

    /// @notice Tests G1 scalar multiplication via MSM (k=1) with identity point
    /// @return true if precompile executes correctly
    function checkG1Mul() external view returns (bool) {
        // G1 identity point (128 bytes) + scalar (32 bytes) = 160 bytes
        bytes memory input = new bytes(160);
        (bool success, bytes memory result) = G1MSM.staticcall(input);
        return success && result.length == 128;
    }

    /// @notice Tests G2 point addition with identity points
    /// @return true if precompile executes correctly
    function checkG2Add() external view returns (bool) {
        // Two G2 identity points (256 bytes each) = 512 bytes
        bytes memory input = new bytes(512);
        (bool success, bytes memory result) = G2ADD.staticcall(input);
        return success && result.length == 256;
    }

    /// @notice Tests pairing check with identity pair
    /// @return true if precompile executes correctly and returns true (0x01)
    function checkPairing() external view returns (bool) {
        // G1 identity (128 bytes) + G2 identity (256 bytes) = 384 bytes
        bytes memory input = new bytes(384);
        (bool success, bytes memory result) = PAIRING.staticcall(input);
        if (!success || result.length != 32) {
            return false;
        }
        // Pairing with identity points should return true (last byte = 0x01)
        return uint8(result[31]) == 1;
    }
}
