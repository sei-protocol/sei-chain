// SPDX-License-Identifier: MIT
pragma solidity 0.8.30;

address constant UPGRADE_PRECOMPILE_ADDRESS = 0x0000000000000000000000000000000000001015;

IUpgrade constant UPGRADE_CONTRACT = IUpgrade(UPGRADE_PRECOMPILE_ADDRESS);

/// @notice Reads the chain's software upgrade plan and the upgrades it has
/// applied. Plans are scheduled and cancelled only through gov proposals.
/// Heights are uint64 where V2 returned int64, with the same selectors and the
/// same encoding for every height. The address holds no code; solc 0.8.10 and
/// later call it without checking for code because every method returns data.
/// Calldata over 20 KiB fails the call and uses its gas.
interface IUpgrade {
    struct Plan {
        string name;
        uint64 height;
        string info;
    }

    struct ModuleVersion {
        string name;
        uint64 version;
    }

    /// @notice Returns the scheduled upgrade plan, or a zero-valued plan when
    /// none is scheduled. height is an EVM block number; the plan takes effect
    /// at the first block at or after it.
    function currentPlan() external view returns (Plan memory plan);

    /// @notice Returns the block the named upgrade was applied at, or 0 when it
    /// has not been applied. An upgrade the chain started past reports the
    /// chain's first block. A name longer than 128 bytes reverts.
    function appliedPlan(string calldata name) external view returns (uint64 height);

    /// @notice Reverts: this chain has no Cosmos modules.
    function moduleVersions(string calldata moduleName) external view returns (ModuleVersion[] memory versions);
}
