// SPDX-License-Identifier: UNLICENSED
pragma solidity ^0.8.20;

interface IBeaconVerifier {
    function verifyBeaconSignature(
        address user,
        bytes32 wifiHash,
        bytes calldata sig
    ) external view returns (bool);
}
