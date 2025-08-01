// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

/// @title Distribution Precompile Interface
/// @notice This interface provides access to Cosmos SDK distribution module functionality
/// @dev The distribution precompile is deployed at a fixed address and allows EVM contracts to interact with staking rewards
address constant DISTR_PRECOMPILE_ADDRESS = 0x0000000000000000000000000000000000001007;

/// @notice Global instance of the distribution precompile contract
IDistr constant DISTR_CONTRACT = IDistr(
    DISTR_PRECOMPILE_ADDRESS
);

/// @title Distribution Module Interface
/// @notice Interface for interacting with the Cosmos SDK distribution module
/// @dev This interface allows managing staking rewards, commission, and withdrawal addresses
interface IDistr {
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
    function rewards(address delegatorAddress) external view returns (Rewards rewards);

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
}
