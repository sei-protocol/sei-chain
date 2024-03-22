// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

address constant STAKING_PRECOMPILE_ADDRESS = 0x0000000000000000000000000000000000001005;

IStaking constant STAKING_CONTRACT = IStaking(STAKING_PRECOMPILE_ADDRESS);

enum BondStatus {
    Unbonded,
    Unbonding,
    Bonded
}

struct UnbondingDelegation {
    uint256 initialAmount;
    uint256 amount;
    uint256 creationHeight;
    uint256 completionTime;
}

struct StakingPool {
    uint256 totalShares;
    uint256 totalTokens;
    BondStatus status;
    bool jailed;
}

interface IStaking {
    // Messages
    function delegate(address validator, uint256 amount) external returns (uint256 shares);

    function redelegate(address src, address dst, uint256 amount) external returns (bool success);

    function undelegate(address validator, uint256 amount) external returns (uint256 unbondingID);

    // Queries
    function getDelegation(address delegator, address validator) external view returns (uint256 shares);

    function getStakingPool(address validator) external view returns (StakingPool memory);

    function getUnbondingDelegation(address validator, uint256 unbondingID)
        external
        view
        returns (UnbondingDelegation memory);
}
