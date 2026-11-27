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
        uint256[8] __gap;
    }

    /// @dev A period of V2's token release schedule: amount is released evenly
    /// from startTime until endTime.
    struct ReleasePeriod {
        uint64 startTime; // Unix nanoseconds
        uint64 endTime; // Unix nanoseconds
        uint256 amount; // wei
    }

    /// @dev The release period minting is in, copied from releaseSchedule when
    /// it starts, and how far minting has got.
    struct Mint {
        uint64 periodEnd; // Unix nanoseconds; zero when no period is copied in
        uint64 lastMintTime; // Unix nanoseconds
        uint256 remaining; // wei of the period not yet minted
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

    // Giga-only, after the V2 copy.
    address internal epochDAO; // 0x09: the DAO address fixed for the current epoch
    uint64 internal nextSettlementEpoch; // 0x09: the epoch the next settlement pays
    ReleasePeriod[] internal releaseSchedule; // 0x0a: in start order, none overlapping
    Mint internal mint; // 0x0b
}
