// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

address constant STAKING_PRECOMPILE_ADDRESS = 0x0000000000000000000000000000000000001005;

IStaking constant STAKING_CONTRACT = IStaking(STAKING_PRECOMPILE_ADDRESS);

interface IStaking {
    // Events

    /**
     * @notice Emitted when tokens are delegated to a validator
     * @param delegator The address of the delegator
     * @param validator The validator address
     * @param amount The amount delegated in base units
     */
    event Delegate(address indexed delegator, string validator, uint256 amount);

    /**
     * @notice Emitted when tokens are redelegated from one validator to another
     * @param delegator The address of the delegator
     * @param srcValidator The source validator address
     * @param dstValidator The destination validator address
     * @param amount The amount redelegated in base units
     */
    event Redelegate(
        address indexed delegator,
        string srcValidator,
        string dstValidator,
        uint256 amount
    );

    /**
     * @notice Emitted when tokens are undelegated from a validator
     * @param delegator The address of the delegator
     * @param validator The validator address
     * @param amount The amount undelegated in base units
     */
    event Undelegate(
        address indexed delegator,
        string validator,
        uint256 amount
    );

    /**
     * @notice Emitted when a new validator is created
     * @param creator The address of the validator creator
     * @param validatorAddress The validator address
     * @param moniker The validator moniker
     */
    event ValidatorCreated(
        address indexed creator,
        string validatorAddress,
        string moniker
    );

    /**
     * @notice Emitted when a validator is edited
     * @param editor The address of the validator editor
     * @param validatorAddress The validator address
     * @param moniker The new validator moniker
     */
    event ValidatorEdited(
        address indexed editor,
        string validatorAddress,
        string moniker
    );

    /**
     * @notice Emitted when rewards are withdrawn during redelegation/undelegation
     * @param delegator The address of delegator
     * @param validator The validator address
     * @param amount The amount withdrawn
     */
    event DelegationRewardsWithdrawn(
        address indexed delegator,
        string validator,
        uint256 amount
    );

    // Transactions

    /**
     * @notice Delegate tokens to a validator
     * @param valAddress The validator address to delegate to
     * @dev Must send value (SEI) with this transaction for delegation amount
     * @return success True if delegation was successful
     */
    function delegate(
        string memory valAddress
    ) external payable returns (bool success);

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
    ) external payable returns (bool success);

    /**
     * @notice Edit an existing validator's parameters
     * @param moniker New validator display name
     * @param commissionRate New commission rate (e.g. "0.10" for 10%)
     *                      Pass empty string "" to not change commission rate
     *                      Note: Commission can only be changed once per 24 hours
     * @param minSelfDelegation New minimum self-delegation amount
     *                         Pass 0 to not change minimum self-delegation
     *                         Note: Can only increase, cannot decrease below current value
     * @return success True if validator edit was successful
     */
    function editValidator(
        string memory moniker,
        string memory commissionRate,
        uint256 minSelfDelegation
    ) external returns (bool success);

    /**
     * @notice Unjail a jailed validator
     * @dev The validator must have been jailed and the jail period must have concluded.
     *      The validator must also meet the minimum self-delegation requirement.
     * @param validatorAddress The bech32-encoded validator address to unjail
     * @return success True if the unjail was successful
     */
    function unjail(
        string memory validatorAddress
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
    ) external view returns (Delegation memory delegation);

    function validators(
        string memory status,
        bytes memory nextKey
    ) external view returns (ValidatorsResponse memory response);

    /**
     * @notice Get validator information for a given validator address
     * @param validatorAddress The validator address
     * @return validator Validator details
     */
    function validator(
        string memory validatorAddress
    ) external view returns (Validator memory validator);

    /**
     * @notice Get delegations for a validator
     * @param validatorAddress The validator address
     * @param nextKey Pagination key
     * @return response Delegations response with pagination
     */
    function validatorDelegations(
        string memory validatorAddress,
        bytes memory nextKey
    ) external view returns (DelegationsResponse memory response);

    /**
     * @notice Get unbonding delegations for a validator
     * @param validatorAddress The validator address
     * @param nextKey Pagination key
     * @return response Unbonding delegations response with pagination
     */
    function validatorUnbondingDelegations(
        string memory validatorAddress,
        bytes memory nextKey
    ) external view returns (UnbondingDelegationsResponse memory response);

    /**
     * @notice Get unbonding delegation information for a delegator and validator pair
     * @param delegator The delegator's address
     * @param validatorAddress The validator address
     * @return unbondingDelegation Unbonding delegation details
     */
    function unbondingDelegation(
        address delegator,
        string memory validatorAddress
    ) external view returns (UnbondingDelegation memory unbondingDelegation);

    /**
     * @notice Get all delegations for a delegator
     * @param delegator The delegator's address
     * @param nextKey Pagination key
     * @return response Delegations response with pagination
     */
    function delegatorDelegations(
        address delegator,
        bytes memory nextKey
    ) external view returns (DelegationsResponse memory response);

    /**
     * @notice Get validator information for a delegator and validator pair
     * @param delegator The delegator's address
     * @param validatorAddress The validator address
     * @return validator Validator details
     */
    function delegatorValidator(
        address delegator,
        string memory validatorAddress
    ) external view returns (Validator memory validator);

    /**
     * @notice Get all unbonding delegations for a delegator
     * @param delegator The delegator's address
     * @param nextKey Pagination key
     * @return response Unbonding delegations response with pagination
     */
    function delegatorUnbondingDelegations(
        address delegator,
        bytes memory nextKey
    ) external view returns (UnbondingDelegationsResponse memory response);

    /**
     * @notice Get redelegations
     * @param delegator The delegator's address (empty string for all)
     * @param srcValidator The source validator address (empty string for all)
     * @param dstValidator The destination validator address (empty string for all)
     * @param nextKey Pagination key
     * @return response Redelegations response with pagination
     */
    function redelegations(
        string memory delegator,
        string memory srcValidator,
        string memory dstValidator,
        bytes memory nextKey
    ) external view returns (RedelegationsResponse memory response);

    /**
     * @notice Get all validators for a delegator
     * @param delegator The delegator's address
     * @param nextKey Pagination key
     * @return response Validators response with pagination
     */
    function delegatorValidators(
        address delegator,
        bytes memory nextKey
    ) external view returns (ValidatorsResponse memory response);

    /**
     * @notice Get historical info for a given height
     * @param height The block height
     * @return historicalInfo Historical info
     */
    function historicalInfo(
        int64 height
    ) external view returns (HistoricalInfo memory historicalInfo);

    /**
     * @notice Get pool information
     * @return pool Pool details
     */
    function pool() external view returns (Pool memory pool);

    /**
     * @notice Get staking parameters
     * @return params Staking parameters
     */
    function params() external view returns (Params memory params);

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

    struct Validator {
        string operatorAddress;
        bytes consensusPubkey;
        bool jailed;
        int32 status;
        string tokens;
        string delegatorShares;
        string description;
        int64 unbondingHeight;
        int64 unbondingTime;
        string commissionRate;
        string commissionMaxRate;
        string commissionMaxChangeRate;
        int64 commissionUpdateTime;
        string minSelfDelegation;
    }

    struct ValidatorsResponse {
        Validator[] validators;
        bytes nextKey;
    }

    struct DelegationsResponse {
        Delegation[] delegations;
        bytes nextKey;
    }

    struct UnbondingDelegationEntry {
        int64 creationHeight;
        int64 completionTime;
        string initialBalance;
        string balance;
    }

    struct UnbondingDelegation {
        string delegatorAddress;
        string validatorAddress;
        UnbondingDelegationEntry[] entries;
    }

    struct UnbondingDelegationsResponse {
        UnbondingDelegation[] unbondingDelegations;
        bytes nextKey;
    }

    struct RedelegationEntry {
        int64 creationHeight;
        int64 completionTime;
        string initialBalance;
        string sharesDst;
    }

    struct Redelegation {
        string delegatorAddress;
        string validatorSrcAddress;
        string validatorDstAddress;
        RedelegationEntry[] entries;
    }

    struct RedelegationsResponse {
        Redelegation[] redelegations;
        bytes nextKey;
    }

    struct HistoricalInfo {
        int64 height;
        Validator[] validators;
    }

    struct Pool {
        string notBondedTokens;
        string bondedTokens;
    }

    struct Params {
        uint64 unbondingTime;
        uint32 maxValidators;
        uint32 maxEntries;
        uint32 historicalEntries;
        string bondDenom;
        string minCommissionRate;
        string maxVotingPowerRatio;
        string maxVotingPowerEnforcementThreshold;
    }
}
