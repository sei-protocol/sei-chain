// SPDX-License-Identifier: MIT
pragma solidity 0.8.30;

import "giga-storage/UpgradeStorage.sol";

/// @notice Writes known values into UpgradeStorage with solc's own codegen, so
/// the Go reader can be checked against the compiler's slot derivation.
contract UpgradeStorageHarness is UpgradeStorage {
    function seed() external {
        plan = Plan({
            name: "v7.0",
            height: 123_456,
            info: '{"upgradeType":"minor","binaries":{"linux/amd64":"https://example.com/seid"}}',
            __gap: [uint256(0), 0, 0, 0]
        });
        appliedHeight["v6.8"] = 42;
        appliedHeight["an upgrade name longer than thirty-one bytes"] = 43;
        lastApplied = "an upgrade name longer than thirty-one bytes";
    }
}
