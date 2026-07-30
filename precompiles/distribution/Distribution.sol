// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

/// @dev The distribution precompile is deployed at a fixed address and allows EVM contracts to interact with staking rewards
address constant DISTR_PRECOMPILE_ADDRESS = 0x0000000000000000000000000000000000001007;

IDistr constant DISTR_CONTRACT = IDistr(
    DISTR_PRECOMPILE_ADDRESS
);

/// @title Distribution Module Interface
/// @notice Interface for interacting with the Cosmos SDK distribution module
/// @dev This interface allows managing staking rewards, commission, and withdrawal addresses
interface IDistr {
    event WithdrawAddressSet(address indexed delegator, address withdrawAddr);
    event DelegationRewardsWithdrawn(address indexed delegator, string validator, uint256 amount);
    event MultipleDelegationRewardsWithdrawn(address indexed delegator, string[] validators, uint256[] amounts);
    event ValidatorCommissionWithdrawn(string indexed validator, uint256 amount);

    // Transactions
    
    /// @notice Sets the withdrawal address for the caller's staking rewards
    /// @dev The caller must have a valid associated Sei address
    /// @param withdrawAddr The EVM address where rewards should be sent
    /// @return success True if the withdrawal address was set successfully
    function setWithdrawAddress(address withdrawAddr) external returns (bool success);

    /// @notice Withdraws delegation rewards from a specific validator
    /// @dev The caller must be a delegator to the specified validator
    /// @param validator The validator's Sei address (e.g., "seivaloper1...")
    /// @return success True if rewards were withdrawn successfully
    function withdrawDelegationRewards(string memory validator) external returns (bool success);

    /// @notice Withdraws delegation rewards from multiple validators in a single transaction
    /// @dev More gas efficient than calling withdrawDelegationRewards multiple times
    /// @param validators Array of validator Sei addresses
    /// @return success True if all rewards were withdrawn successfully
    function withdrawMultipleDelegationRewards(string[] memory validators) external returns (bool success);

    /// @notice Withdraws validator commission (only callable by the validator operator)
    /// @dev Only the validator operator can withdraw their commission
    /// @return success True if commission was withdrawn successfully
    function withdrawValidatorCommission() external returns (bool success);

    // Queries
    
    /// @notice Gets all pending rewards for a delegator
    /// @dev Returns rewards from all validators the address has delegated to
    /// @param delegatorAddress The EVM address of the delegator
    /// @return rewards Structured data containing all pending rewards
    function rewards(address delegatorAddress) external view returns (Rewards memory rewards);

    /// @notice Gets the distribution module parameters
    /// @return params The distribution module parameters
    function params() external view returns (DistributionParams memory params);

    /// @notice Gets the outstanding (un-withdrawn) rewards of a validator and all its delegations
    /// @param validatorAddress The validator's Sei address (e.g., "seivaloper1...")
    /// @return rewards The outstanding rewards of the validator
    function validatorOutstandingRewards(string memory validatorAddress) external view returns (Coin[] memory rewards);

    /// @notice Gets the accumulated commission of a validator
    /// @param validatorAddress The validator's Sei address (e.g., "seivaloper1...")
    /// @return commission The accumulated commission of the validator
    function validatorCommission(string memory validatorAddress) external view returns (Coin[] memory commission);

    /// @notice Gets the slash events of a validator within a height range
    /// @param validatorAddress The validator's Sei address (e.g., "seivaloper1...")
    /// @param startingHeight The starting height to query the slashes from
    /// @param endingHeight The ending height to query the slashes to
    /// @param pageKey The pagination key from a previous response (empty bytes for the first page)
    /// @return slashes The slash events of the validator
    /// @return nextKey The pagination key for the next page (empty when exhausted)
    function validatorSlashes(string memory validatorAddress, uint64 startingHeight, uint64 endingHeight, bytes memory pageKey) external view returns (Slash[] memory slashes, bytes memory nextKey);

    /// @notice Gets the pending rewards of a delegation
    /// @param delegatorAddress The EVM address of the delegator
    /// @param validatorAddress The validator's Sei address (e.g., "seivaloper1...")
    /// @return rewards The pending rewards accrued by the delegation
    function delegationRewards(address delegatorAddress, string memory validatorAddress) external view returns (Coin[] memory rewards);

    /// @notice Gets the validators a delegator is delegating to
    /// @param delegatorAddress The EVM address of the delegator
    /// @return validators The Sei addresses of the validators
    function delegatorValidators(address delegatorAddress) external view returns (string[] memory validators);

    /// @notice Gets the withdraw address of a delegator
    /// @param delegatorAddress The EVM address of the delegator
    /// @return withdrawAddress The delegator's withdraw address (bech32)
    function delegatorWithdrawAddress(address delegatorAddress) external view returns (string memory withdrawAddress);

    /// @notice Gets the coins held by the community pool
    /// @return pool The coins in the community pool
    function communityPool() external view returns (Coin[] memory pool);

    /// @notice Represents a coin/token with amount, decimals, and denomination
    /// @dev Used to represent various tokens in the Cosmos ecosystem
    struct Coin {
        /// @notice The amount of tokens (as a big integer)
        uint256 amount;
        /// @notice Number of decimal places for display purposes
        uint256 decimals;
        /// @notice Token denomination (e.g., "usei", "uatom")
        string denom;
    }

    /// @notice Represents rewards from a specific validator
    /// @dev Contains all reward coins from a single validator
    struct Reward {
        /// @notice Array of different coin types earned as rewards
        Coin[] coins;
        /// @notice The validator's Sei address that generated these rewards
        string validator_address;
    }

    /// @notice Aggregated rewards information for a delegator
    /// @dev Contains both per-validator breakdown and total rewards
    struct Rewards {
        /// @notice Array of rewards from each validator
        Reward[] rewards;
        /// @notice Total rewards summed across all validators
        Coin[] total;
    }

    /// @notice Parameters of the distribution module
    struct DistributionParams {
        /// @notice The tax rate applied to rewards for the community pool (decimal string)
        string communityTax;
        /// @notice The base reward rate for block proposers (decimal string)
        string baseProposerReward;
        /// @notice The bonus reward rate for block proposers (decimal string)
        string bonusProposerReward;
        /// @notice Whether delegators can set a separate withdraw address
        bool withdrawAddrEnabled;
    }

    /// @notice Represents a validator slash event
    struct Slash {
        /// @notice The validator distribution period when the slash occurred
        uint64 validatorPeriod;
        /// @notice The fraction of the validator's stake that was slashed (decimal string)
        string fraction;
    }
}
