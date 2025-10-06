// SPDX-License-Identifier: UNLICENSED
pragma solidity ^0.8.28;

import {KinRoyaltyEnforcer} from "./KinRoyaltyEnforcer.sol";

contract VaultScannerV2WithGasProof {
    KinRoyaltyEnforcer public immutable royalty;
    uint256 public constant SSTORE_GAS_COST = 72_000;

    mapping(bytes32 => bytes32) public vault;

    constructor(address royaltyEnforcer) {
        royalty = KinRoyaltyEnforcer(royaltyEnforcer);
    }

    function write(bytes32 key, bytes32 value) external payable {
        royalty.enforceRoyalty{value: msg.value}(SSTORE_GAS_COST);
        vault[key] = value;
    }
}
