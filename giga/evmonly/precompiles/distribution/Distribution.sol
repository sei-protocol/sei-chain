// SPDX-License-Identifier: MIT
pragma solidity 0.8.30;

address constant DISTRIBUTION_PRECOMPILE_ADDRESS = 0x0000000000000000000000000000000000001007;

IDistribution constant DISTRIBUTION_CONTRACT = IDistribution(DISTRIBUTION_PRECOMPILE_ADDRESS);

/// @notice The EVM-only distribution precompile. Validators are named by their
/// operator's address and amounts are wei. A delegation's rewards are owed up to
/// the latest settlement, and staking pays them out whenever it changes the
/// delegation.
/// @dev An argument out of range, such as the zero address or a repeated
/// validator, reverts with an Error(string) naming it; a call that fails
/// against the current state, such as withdrawing from a validator the caller
/// does not delegate to, fails with no revert data and uses its gas. A method
/// marked "Reverts:" is kept so a caller of a V2 method gets a revert whose
/// Error(string) names the call to use instead, or says the operation is not
/// supported. The address holds no code; solc 0.8.10 and later call it without
/// checking for code because every method returns data. Calldata over 20 KiB
/// fails the call and uses its gas.
interface IDistribution {
    /// @notice The rewards one delegation is owed.
    struct DelegationReward {
        address validator;
        uint256 amount; // wei
    }

    /// @notice The rewards each of a delegator's delegations is owed, and their
    /// sum.
    struct Rewards {
        DelegationReward[] rewards;
        uint256 total; // wei
    }

    struct DistributionParams {
        bool withdrawAddrEnabled;
        address daoAddress;
    }

    /// @notice V2's coin, kept for the signatures of the methods that revert.
    struct Coin {
        uint256 amount;
        uint256 decimals;
        string denom;
    }

    /// @notice V2's slash event, kept for the signature of a method that
    /// reverts.
    struct Slash {
        uint64 validatorPeriod;
        string fraction;
    }

    /// @notice delegator's rewards are paid to withdrawAddr from now on.
    event WithdrawAddressSet(address indexed delegator, address withdrawAddr);

    /// @notice amount of delegator's rewards from validator were paid to its
    /// withdraw address.
    event DelegationRewardsWithdrawn(address indexed delegator, address indexed validator, uint256 amount);

    /// @notice amount of validator's commission was paid to its operator's
    /// withdraw address.
    event ValidatorCommissionWithdrawn(address indexed validator, uint256 amount);

    /// @notice Pays the caller's rewards to withdrawAddr from now on. Fails when
    /// withdrawAddrEnabled is off, or when withdrawAddr is an escrow of the
    /// staking or distribution precompile or the fee collector.
    function setWithdrawAddress(address withdrawAddr) external returns (bool success);

    /// @notice Pays the rewards the caller's delegation to validator is owed to
    /// the caller's withdraw address.
    function withdrawDelegationRewards(address validator) external returns (bool success);

    /// @notice Pays the rewards the caller's delegation to each of validators is
    /// owed to the caller's withdraw address.
    function withdrawMultipleDelegationRewards(address[] calldata validators) external returns (bool success);

    /// @notice Pays the commission of the caller's validator to the caller's
    /// withdraw address. Fails when there is none.
    function withdrawValidatorCommission() external returns (bool success);

    /// @notice Returns the rewards each of delegator's delegations is owed.
    function rewards(address delegator) external view returns (Rewards memory rewards);

    /// @notice Returns the rewards delegator's delegation to validator is owed.
    /// Fails with no revert data, using its gas, when there is no such
    /// delegation.
    function delegationRewards(address delegator, address validator) external view returns (uint256 rewards);

    /// @notice Returns the rewards validator and its delegations have been
    /// credited and not yet withdrawn, commission included.
    function validatorOutstandingRewards(address validator) external view returns (uint256 rewards);

    /// @notice Returns validator's commission not yet withdrawn.
    function validatorCommission(address validator) external view returns (uint256 commission);

    /// @notice Returns the account delegator's rewards are paid to.
    function delegatorWithdrawAddress(address delegator) external view returns (address withdrawAddress);

    /// @notice Returns the params precompile's distribution params.
    function params() external view returns (DistributionParams memory params);

    /// @notice Reverts: use withdrawDelegationRewards(address).
    function withdrawDelegationRewards(string memory validator) external returns (bool success);

    /// @notice Reverts: use withdrawMultipleDelegationRewards(address[]).
    function withdrawMultipleDelegationRewards(string[] memory validators) external returns (bool success);

    /// @notice Reverts: use validatorOutstandingRewards(address).
    function validatorOutstandingRewards(string memory validatorAddress) external view returns (Coin[] memory rewards);

    /// @notice Reverts: use validatorCommission(address).
    function validatorCommission(string memory validatorAddress) external view returns (Coin[] memory commission);

    /// @notice Reverts: slash events are not served.
    function validatorSlashes(
        string memory validatorAddress,
        uint64 startingHeight,
        uint64 endingHeight,
        bytes memory pageKey
    ) external view returns (Slash[] memory slashes, bytes memory nextKey);

    /// @notice Reverts: use delegationRewards(address,address).
    function delegationRewards(address delegatorAddress, string memory validatorAddress)
        external
        view
        returns (Coin[] memory rewards);

    /// @notice Reverts: use staking's delegatorValidators(address,bytes).
    function delegatorValidators(address delegatorAddress) external view returns (string[] memory validators);

    /// @notice Reverts: the community pool is retired; what it would hold goes
    /// to the DAO.
    function communityPool() external view returns (Coin[] memory pool);
}
