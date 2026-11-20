// SPDX-License-Identifier: MIT
pragma solidity 0.8.30;

import "giga-storage/ParamsStorage.sol";

/// @notice Writes known values into ParamsStorage with solc's own codegen, so
/// the Go reader can be checked against the compiler's slot derivation.
contract ParamsStorageHarness is ParamsStorage {
    function seed() external {
        gov = Gov({
            maxDepositPeriod: 172_800e9,
            votingPeriod: 259_200e9,
            expeditedVotingPeriod: 86_400e9 + 1,
            minDeposit: 3_500_000_000,
            minExpeditedDeposit: 7_000_000_000,
            quorum: 0.334e18,
            threshold: 0.5e18,
            vetoThreshold: 0.334e18,
            expeditedQuorum: 0.667e18,
            expeditedThreshold: 0.667e18,
            __gap: [uint256(0), 0, 0, 0, 0, 0, 0, 0]
        });
        staking = Staking({
            unbondingTime: 1_814_400e9,
            maxValidators: 35,
            maxEntries: 7,
            maxVotingPowerRatio: 0.2e18,
            maxVotingPowerEnforcementThreshold: 1_000_001,
            __gap: [uint256(0), 0, 0, 0, 0, 0, 0, 0]
        });
        // V2's x/evm defaults (x/evm/types/params.go), and a lag V2 lacks. The evm subspace's
        // genesis values and the base fee rule they set are in docs/giga-storage/params-design.md.
        evm = EVM({
            maxDynamicBaseFeeUpwardAdjustment: 0.0189e18,
            maxDynamicBaseFeeDownwardAdjustment: 0.0039e18,
            minFeePerGas: 1e9 * 1e18,
            baseFeeLag: 3,
            __gap: [uint256(0), 0, 0, 0, 0, 0]
        });
    }
}
