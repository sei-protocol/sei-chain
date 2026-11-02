// SPDX-License-Identifier: MIT
pragma solidity 0.8.30;

import "./Sets.sol";

/// @notice Storage schema of the evmonly slashing precompile (0x1014), held as that
/// address's contract storage. Each state variable is an x/slashing store prefix,
/// named in its comment.
/// @dev Params are `ParamsStorage.slashing`. Keys are consensus addresses,
/// sha256(consensusPubkey)[:20]. Times are Unix nanoseconds.
contract SlashingStorage {
    struct ValidatorSigningInfo {
        uint64 startHeight;
        uint64 indexOffset;
        uint64 jailedUntil;
        bool tombstoned;
        uint64 missedBlocksCounter;
    }

    struct ValidatorMissedBlockArray {
        uint64 windowSize;
        uint64[] missedBlocks; // bit i of the window is bit i%64 of word i/64
    }

    mapping(address => ValidatorSigningInfo) internal validatorSigningInfo; // 0x01: consensus address =>
    AddressSet internal signingInfoSet; // every consensus address in validatorSigningInfo
    mapping(address => ValidatorMissedBlockArray) internal validatorMissedBlocks; // 0x02: consensus address =>
    mapping(address => bytes32) internal addrPubkeyRelation; // 0x03: consensus address => ed25519 key
}
