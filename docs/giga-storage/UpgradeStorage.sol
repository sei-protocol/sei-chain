// SPDX-License-Identifier: MIT
pragma solidity 0.8.30;

/// @notice Storage schema of the evmonly upgrade precompile (0x1015), held as that
/// address's contract storage.
/// @dev Heights are EVM block numbers. Once a public network runs this, the
/// layout only grows: Plan takes new fields from its `__gap`, and new state
/// variables are appended.
contract UpgradeStorage {
    /// @dev A scheduled software upgrade. A height of 0 means none is scheduled.
    struct Plan {
        string name;
        uint64 height;
        string info; // V2's Plan.Info; {"upgradeType":"minor"} marks a minor release
        uint256[4] __gap;
    }

    Plan internal plan;
    /// @dev The block each applied upgrade ran at, by name. Names the chain
    /// started past are recorded at its first block.
    mapping(string => uint64) internal appliedHeight;
    /// @dev The name of the upgrade applied last.
    string internal lastApplied;
}
