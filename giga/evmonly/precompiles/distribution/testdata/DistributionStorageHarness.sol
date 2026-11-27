// SPDX-License-Identifier: MIT
pragma solidity 0.8.30;

import "giga-storage/DistributionStorage.sol";

/// @notice Writes known values into DistributionStorage with solc's own codegen,
/// so the Go reader can be checked against the compiler's slot derivation.
contract DistributionStorageHarness is DistributionStorage {
    function seed(address val, address other, address delegator, address payee) external {
        communityPool = 11;
        previousProposer = other;
        validatorOutstandingRewards[val] = 21;
        delegatorWithdrawAddr[delegator] = payee;
        delegatorStartingInfo[val][delegator] = DelegatorStartingInfo({previousPeriod: 31, height: 32, stake: 33e18});
        validatorHistoricalRewards[val][31] = ValidatorHistoricalRewards({cumulativeRewardRatio: 41e18, referenceCount: 42});
        validatorCurrentRewards[val] = ValidatorCurrentRewards({rewards: 51, period: 52});
        validatorAccumulatedCommission[val] = 61;
        validatorSlashEvents[val].push();
        ValidatorSlashEvent storage first = validatorSlashEvents[val][0];
        first.height = 71;
        first.validatorPeriod = 72;
        first.fraction = 0.05e18;
        validatorSlashEvents[val].push();
        ValidatorSlashEvent storage second = validatorSlashEvents[val][1];
        second.height = 73;
        second.validatorPeriod = 74;
        second.fraction = 0.5e18;
        epochDAO = other;
        nextSettlementEpoch = 81;
        releaseSchedule.push(ReleasePeriod({startTime: 91, endTime: 92, amount: 93e18}));
        releaseSchedule.push(ReleasePeriod({startTime: 94, endTime: 95, amount: 96e18}));
        mint = Mint({periodEnd: 101, lastMintTime: 102, remaining: 103e18});
    }
}
