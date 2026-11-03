// SPDX-License-Identifier: MIT
pragma solidity 0.8.30;

/// @notice Storage schema of the evmonly distribution precompile (0x1007), held as
/// that address's contract storage. Each state variable is an x/distribution
/// store prefix, named in its comment.
/// @dev Params are `ParamsStorage.distribution`. Outstanding rewards and the
/// community pool sit in the balance of `DistributionEscrow`, a derived address
/// in `distribution/layout.go`. Amounts and stakes are wei; reward ratios and
/// slash fractions are 1e18-scaled.
contract DistributionStorage {
    struct ValidatorHistoricalRewards {
        uint256 cumulativeRewardRatio;
        uint32 referenceCount;
    }

    struct ValidatorCurrentRewards {
        uint256 rewards;
        uint64 period;
    }

    struct DelegatorStartingInfo {
        uint64 previousPeriod;
        uint64 height;
        uint256 stake;
    }

    struct ValidatorSlashEvent {
        uint64 height;
        uint64 validatorPeriod;
        uint256 fraction;
    }

    uint256 internal communityPool; // 0x00: FeePool.community_pool
    address internal previousProposer; // 0x01: operator
    mapping(address => uint256) internal validatorOutstandingRewards; // 0x02: validator =>
    mapping(address => address) internal delegatorWithdrawAddr; // 0x03: delegator => payee; zero is the delegator
    mapping(address => mapping(address => DelegatorStartingInfo)) internal delegatorStartingInfo; // 0x04: validator => delegator =>
    mapping(address => mapping(uint64 => ValidatorHistoricalRewards)) internal validatorHistoricalRewards; // 0x05: validator => period =>
    mapping(address => ValidatorCurrentRewards) internal validatorCurrentRewards; // 0x06: validator =>
    mapping(address => uint256) internal validatorAccumulatedCommission; // 0x07: validator =>
    mapping(address => ValidatorSlashEvent[]) internal validatorSlashEvents; // 0x08: validator => ascending by height
}
