// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

address constant UPGRADE_PRECOMPILE_ADDRESS = 0x0000000000000000000000000000000000001015;

IUpgrade constant UPGRADE_CONTRACT = IUpgrade(UPGRADE_PRECOMPILE_ADDRESS);

interface IUpgrade {
    // Queries
    // Returns the currently scheduled upgrade plan, or a zero-valued plan if
    // no upgrade is scheduled.
    function currentPlan() external view returns (Plan memory plan);

    // Returns the block height at which the named upgrade was applied, or 0
    // if it has not been applied.
    function appliedPlan(
        string memory name
    ) external view returns (int64 height);

    // Returns the upgraded consensus state stored for the given last height,
    // or empty bytes if none is stored.
    function upgradedConsensusState(
        int64 lastHeight
    ) external view returns (bytes memory consensusState);

    // Returns the consensus versions of app modules. An empty moduleName
    // returns all modules; a specific moduleName returns just that module.
    function moduleVersions(
        string memory moduleName
    ) external view returns (ModuleVersion[] memory versions);

    // Structs
    struct Plan {
        string name;
        int64 height;
        string info;
    }

    struct ModuleVersion {
        string name;
        uint64 version;
    }
}
