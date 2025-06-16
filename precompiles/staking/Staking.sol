// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

address constant STAKING_PRECOMPILE_ADDRESS = 0x0000000000000000000000000000000000001005;

IStaking constant STAKING_CONTRACT = IStaking(
    STAKING_PRECOMPILE_ADDRESS
);

interface IStaking {
    // Transactions
    
    /**
     * @notice Delegate tokens to a validator
     * @param valAddress The validator address to delegate to
     * @dev Must send value (SEI) with this transaction for delegation amount
     * @return success True if delegation was successful
     */
    function delegate(
        string memory valAddress
    ) payable external returns (bool success);

    /**
     * @notice Redelegate tokens from one validator to another
     * @param srcAddress The source validator address
     * @param dstAddress The destination validator address  
     * @param amount Amount to redelegate in base units
     * @return success True if redelegation was successful
     */
    function redelegate(
        string memory srcAddress,
        string memory dstAddress,
        uint256 amount
    ) external returns (bool success);

    /**
     * @notice Undelegate tokens from a validator
     * @param valAddress The validator address to undelegate from
     * @param amount Amount to undelegate in base units
     * @return success True if undelegation was successful
     */
    function undelegate(
        string memory valAddress,
        uint256 amount
    ) external returns (bool success);

    /**
     * @notice Create a new validator. Delegation amount must be provided as value in wei
     * @param pubKeyHex Ed25519 public key in hex format (64 characters)
     * @param moniker Validator display name
     * @param commissionRate Initial commission rate (e.g. "0.05" for 5%)
     * @param commissionMaxRate Maximum commission rate (e.g. "0.20" for 20%)
     * @param commissionMaxChangeRate Maximum commission change rate per day (e.g. "0.01" for 1%)
     * @param minSelfDelegation Minimum self-delegation amount in base units
     * @return success True if validator creation was successful
     */
    function createValidator(
        string memory pubKeyHex,
        string memory moniker,
        string memory commissionRate,
        string memory commissionMaxRate,
        string memory commissionMaxChangeRate,
        uint256 minSelfDelegation
    ) payable external returns (bool success);

    /**
     * @notice Edit an existing validator's parameters
     * @param moniker New validator display name
     * @param commissionRate New commission rate (e.g. "0.10" for 10%) 
     *                      Pass empty string "" to not change commission rate
     *                      Note: Commission can only be changed once per 24 hours
     * @param minSelfDelegation New minimum self-delegation amount in base units
     *                         Pass 0 to not change minimum self-delegation
     *                         Note: Can only increase, cannot decrease below current value
     * @return success True if validator edit was successful
     */
    function editValidator(
        string memory moniker,
        string memory commissionRate,
        uint256 minSelfDelegation
    ) external returns (bool success);

    // Queries
    
    /**
     * @notice Get delegation information for a delegator and validator pair
     * @param delegator The delegator's address  
     * @param valAddress The validator address
     * @return delegation Delegation details including balance and shares
     */
    function delegation(
        address delegator,
        string memory valAddress
    ) external view returns (Delegation delegation);

    struct Delegation {
        Balance balance;
        DelegationDetails delegation;
    }

    struct Balance {
        uint256 amount;
        string denom;
    }

    struct DelegationDetails {
        string delegator_address;
        uint256 shares;
        uint256 decimals;
        string validator_address;
    }
}