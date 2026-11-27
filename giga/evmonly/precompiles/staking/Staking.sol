// SPDX-License-Identifier: MIT
pragma solidity 0.8.30;

address constant STAKING_PRECOMPILE_ADDRESS = 0x0000000000000000000000000000000000001005;

IStaking constant STAKING_CONTRACT = IStaking(STAKING_PRECOMPILE_ADDRESS);

/// @notice The EVM-only staking precompile. Validators are named by their
/// operator's address, amounts are wei, shares and rates are 1e18-scaled, and
/// times are Unix nanoseconds. An amount passed in must be a positive whole
/// number of usei. Validator statuses are numbered as x/staking's.
/// @dev An argument out of range, such as a zero amount, one that is not whole
/// usei, or an unknown status, reverts with an Error(string) naming it; a call
/// that fails against the current state, such as undelegating more than is
/// delegated, fails with no revert data and uses its gas. A method marked
/// "Reverts:" is kept so a caller of a V2 method gets a revert whose
/// Error(string) names the call to use instead, or says the operation is not
/// supported. The address holds no code; solc 0.8.10 and later call it without
/// checking for code because every method returns data. Calldata over 20 KiB
/// fails the call and uses its gas.
interface IStaking {
    /// @notice A validator's description. A field whose stored value does not
    /// decode reads as empty.
    struct Description {
        string moniker;
        string identity;
        string website;
        string securityContact;
        string details;
    }

    struct Validator {
        address operator;
        bytes32 consensusPubkey; // ed25519
        bool jailed;
        uint8 status; // 1=Unbonded, 2=Unbonding, 3=Bonded
        uint256 tokens; // wei
        uint256 delegatorShares;
        Description description;
        uint64 unbondingHeight;
        uint64 unbondingTime;
        uint256 commissionRate;
        uint256 commissionMaxRate;
        uint256 commissionMaxChangeRate;
        uint64 commissionUpdateTime;
        uint256 minSelfDelegation; // wei
    }

    /// @notice A delegation. balance is the tokens its shares are worth at the
    /// validator's current exchange rate, truncated to whole wei.
    struct Delegation {
        address delegator;
        address validator;
        uint256 shares;
        uint256 balance; // wei
    }

    struct UnbondingDelegationEntry {
        uint64 creationHeight;
        uint64 completionTime;
        uint256 initialBalance; // wei
        uint256 balance; // wei
    }

    struct UnbondingDelegation {
        address delegator;
        address validator;
        UnbondingDelegationEntry[] entries; // oldest first
    }

    /// @notice An operation staged to take effect at the end of epoch. A
    /// delegation stages amount; an undelegation or redelegation stages shares
    /// of the delegation to validator, worth amount at its current exchange
    /// rate.
    struct StagedOperation {
        uint64 epoch;
        uint8 kind; // 1=Delegate, 2=Undelegate, 3=Redelegate
        address delegator;
        address validator; // a redelegation's source
        address dstValidator; // a redelegation's destination; zero otherwise
        uint256 amount; // wei
        uint256 shares; // zero for a delegation
    }

    /// @notice notBondedTokens is held for unbonded and unbonding validators and
    /// for unbonding entries; bondedTokens is held for bonded validators.
    struct Pool {
        uint256 notBondedTokens; // wei
        uint256 bondedTokens; // wei
    }

    struct StakingParams {
        uint64 unbondingTime; // nanoseconds
        uint32 maxValidators;
        uint32 maxEntries;
        uint256 maxVotingPowerRatio;
        uint256 maxVotingPowerEnforcementThreshold; // consensus power
    }

    /// @notice The caller staged delegating amount to validator. It takes effect
    /// at the end of epoch.
    event Delegate(address indexed delegator, address indexed validator, uint256 amount, uint64 epoch);

    /// @notice The caller staged moving stake worth amount now from srcValidator
    /// to dstValidator. It takes effect at the end of epoch.
    event Redelegate(
        address indexed delegator,
        address indexed srcValidator,
        address indexed dstValidator,
        uint256 amount,
        uint64 epoch
    );

    /// @notice The caller staged undelegating stake worth amount now from
    /// validator. It takes effect at the end of epoch.
    event Undelegate(address indexed delegator, address indexed validator, uint256 amount, uint64 epoch);

    /// @notice Delegates msg.value to validator at the end of the epoch after
    /// this one. Until then staking holds msg.value, and a delegation that can no
    /// longer be made then is refunded.
    /// @dev Once 10,000 units of work have been staged in an epoch, staging
    /// charges 100,000 gas a unit, doubling every 2,000 units up to a quarter of
    /// the block's gas limit. A delegate or undelegate is one unit, and a
    /// redelegate two.
    function delegate(address validator) external payable returns (bool success);

    /// @notice Moves amount of the caller's delegation from srcValidator to
    /// dstValidator at the end of the epoch after this one, as x/staking's
    /// MsgBeginRedelegate does then. From now until it completes it blocks a
    /// redelegation out of dstValidator and counts toward maxEntries; until it
    /// takes effect, it counts toward every destination's.
    function redelegate(address srcValidator, address dstValidator, uint256 amount) external returns (bool success);

    /// @notice Undelegates amount from validator at the end of the epoch after
    /// this one, as x/staking's MsgUndelegate does then. The tokens are paid
    /// once the unbonding period that then begins ends. Its entry counts toward
    /// maxEntries from now.
    function undelegate(address validator, uint256 amount) external returns (bool success);

    /// @notice Returns delegator's delegation to validator. Fails with no revert
    /// data, using its gas, when there is none.
    function delegation(address delegator, address validator) external view returns (Delegation memory delegation);

    /// @notice Returns validator's record. Fails with no revert data, using its
    /// gas, when there is none.
    function validator(address validator) external view returns (Validator memory);

    /// @notice Pages through every validator record, filtered by status (0 for
    /// any). A page covers 100 records, so it may hold fewer than 100
    /// validators, or none, and still return a nextKey; nextKey is empty on the
    /// last page.
    function validators(uint8 status, bytes calldata pageKey)
        external
        view
        returns (Validator[] memory validators, bytes memory nextKey);

    /// @notice Pages through validator's delegations, 100 at a time.
    function validatorDelegations(address validator, bytes calldata pageKey)
        external
        view
        returns (Delegation[] memory delegations, bytes memory nextKey);

    /// @notice Pages through delegator's delegations, 100 at a time.
    function delegatorDelegations(address delegator, bytes calldata pageKey)
        external
        view
        returns (Delegation[] memory delegations, bytes memory nextKey);

    /// @notice Returns the record of validator when delegator delegates to it.
    /// Fails with no revert data, using its gas, when delegator does not.
    function delegatorValidator(address delegator, address validator) external view returns (Validator memory);

    /// @notice Pages through the records of the validators delegator delegates
    /// to, 100 at a time.
    function delegatorValidators(address delegator, bytes calldata pageKey)
        external
        view
        returns (Validator[] memory validators, bytes memory nextKey);

    /// @notice Returns delegator's unbonding entries from validator. Fails with
    /// no revert data, using its gas, when there are none.
    function unbondingDelegation(address delegator, address validator)
        external
        view
        returns (UnbondingDelegation memory unbondingDelegation);

    /// @notice Pages through the staged operations in the order they take
    /// effect, filtered by delegator (the zero address for any). The other
    /// queries count an operation only once it takes effect. A page covers 100
    /// operations, so it may hold fewer than 100 of delegator's, or none, and
    /// still return a nextKey; nextKey is empty on the last page. A pageKey
    /// whose operations have since taken effect resumes at the first one still
    /// staged.
    function stagedOperations(address delegator, bytes calldata pageKey)
        external
        view
        returns (StagedOperation[] memory operations, bytes memory nextKey);

    /// @notice Returns the tokens staking holds.
    function pool() external view returns (Pool memory pool);

    /// @notice Returns the params staking applies: the params precompile's
    /// staking params as of the latest epoch's end.
    function params() external view returns (StakingParams memory params);

    /// @notice Reverts: use delegate(address).
    function delegate(string memory valAddress) external payable returns (bool success);

    /// @notice Reverts: use redelegate(address,address,uint256).
    function redelegate(string memory srcAddress, string memory dstAddress, uint256 amount)
        external
        returns (bool success);

    /// @notice Reverts: use undelegate(address,uint256).
    function undelegate(string memory valAddress, uint256 amount) external returns (bool success);

    /// @notice Reverts: staking authorizations are not supported.
    function grantStakingAuthorization(
        address grantee,
        string[] memory allowedValidators,
        uint256 maxTokens,
        int64 expiration
    ) external returns (bool success);

    /// @notice Reverts: staking authorizations are not supported.
    function delegateWithAuthorization(address delegator, string memory valAddress)
        external
        payable
        returns (bool success);

    /// @notice Reverts: staking authorizations are not supported.
    function redelegateWithAuthorization(
        address delegator,
        string memory srcAddress,
        string memory dstAddress,
        uint256 amount
    ) external returns (bool success);

    /// @notice Reverts: staking authorizations are not supported.
    function undelegateWithAuthorization(address delegator, string memory valAddress, uint256 amount)
        external
        returns (bool success);

    /// @notice Reverts: staking authorizations are not supported.
    function revokeStakingAuthorization(address grantee) external returns (bool success);

    /// @notice Reverts: use delegation(address,address).
    function delegation(address delegator, string memory valAddress)
        external
        view
        returns (Delegation memory delegation);

    /// @notice Reverts: use validator(address).
    function validator(string memory validatorAddress) external view returns (Validator memory);

    /// @notice Reverts: use validators(uint8,bytes).
    function validators(string memory status, bytes memory pageKey)
        external
        view
        returns (Validator[] memory validators, bytes memory nextKey);

    /// @notice Reverts: use validatorDelegations(address,bytes).
    function validatorDelegations(string memory validatorAddress, bytes memory pageKey)
        external
        view
        returns (Delegation[] memory delegations, bytes memory nextKey);

    /// @notice Reverts: use unbondingDelegation(address,address).
    function unbondingDelegation(address delegator, string memory validatorAddress)
        external
        view
        returns (UnbondingDelegation memory unbondingDelegation);

    /// @notice Reverts: use delegatorValidator(address,address).
    function delegatorValidator(address delegator, string memory validatorAddress)
        external
        view
        returns (Validator memory);

    /// @notice Reverts: historical info is not supported.
    function historicalInfo(int64 height) external view returns (bool success);
}
