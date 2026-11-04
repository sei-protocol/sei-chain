// SPDX-License-Identifier: MIT
pragma solidity 0.8.30;

address constant STAKING_PRECOMPILE_ADDRESS = 0x0000000000000000000000000000000000001005;

IStaking constant STAKING_CONTRACT = IStaking(STAKING_PRECOMPILE_ADDRESS);

/// @notice The EVM-only staking precompile. Validators are named by their
/// operator's address, amounts are wei and must be a positive whole number of
/// usei, and times are Unix nanoseconds.
/// @dev An argument out of range, such as a zero amount or one that is not whole
/// usei, reverts with an Error(string) naming it; a call that fails against the
/// current state, such as undelegating more than is delegated, fails with no
/// revert data and uses its gas. A method marked "Reverts:" is kept so a caller
/// of a V2 method gets a revert whose Error(string) names the call to use
/// instead, or says the operation is not supported. The address holds no code;
/// solc 0.8.10 and later call it without checking for code because every method
/// returns data. Calldata over 20 KiB fails the call and uses its gas.
interface IStaking {
    /// @notice The caller delegated amount to validator for shares, 1e18-scaled.
    event Delegate(address indexed delegator, address indexed validator, uint256 amount, uint256 shares);

    /// @notice The caller moved amount from srcValidator to dstValidator. The
    /// stake moved at once; completionTime is when the move stops counting toward
    /// maxEntries and stops blocking a redelegation out of dstValidator, or 0 when
    /// it never did, because srcValidator was unbonded.
    event Redelegate(
        address indexed delegator,
        address indexed srcValidator,
        address indexed dstValidator,
        uint256 amount,
        uint64 completionTime
    );

    /// @notice The caller undelegated from validator. amount is the tokens its
    /// shares were worth, paid to the caller at the first block at or after
    /// completionTime.
    event Undelegate(address indexed delegator, address indexed validator, uint256 amount, uint64 completionTime);

    /// @notice Delegates msg.value to validator.
    function delegate(address validator) external payable returns (bool success);

    /// @notice Moves amount of the caller's delegation from srcValidator to
    /// dstValidator, as x/staking's MsgBeginRedelegate does.
    function redelegate(address srcValidator, address dstValidator, uint256 amount) external returns (bool success);

    /// @notice Undelegates amount from validator, as x/staking's MsgUndelegate
    /// does. The tokens are paid once the unbonding period ends.
    function undelegate(address validator, uint256 amount) external returns (bool success);

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
}
