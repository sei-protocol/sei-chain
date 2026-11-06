// SPDX-License-Identifier: MIT
pragma solidity 0.8.30;

address constant SLASHING_PRECOMPILE_ADDRESS = 0x0000000000000000000000000000000000001014;

ISlashing constant SLASHING_CONTRACT = ISlashing(SLASHING_PRECOMPILE_ADDRESS);

/// @notice The EVM-only slashing precompile. Validators are named by their
/// operator's address.
/// @dev A call that fails against the current state, such as unjailing a
/// validator that is not jailed, fails with no revert data and uses its gas. A
/// method marked "Reverts:" is kept so a caller of a V2 method gets a revert
/// whose Error(string) says the operation is not supported. The address holds no
/// code; solc 0.8.10 and later call it without checking for code because every
/// method returns data. Calldata over 20 KiB fails the call and uses its gas.
interface ISlashing {
    /// @notice validator's operator unjailed it.
    event Unjail(address indexed validator);

    /// @notice Unjails the caller's validator, as x/slashing's MsgUnjail does.
    /// The caller's own delegation to it must be worth at least its minimum
    /// self-delegation. It rejoins the bonded set at an EndBlock that ranks it
    /// there.
    function unjail() external returns (bool success);

    /// @notice Reverts: unjail authorizations are not supported.
    function grantUnjailAuthorization(address grantee, int64 expiration) external returns (bool success);

    /// @notice Reverts: unjail authorizations are not supported.
    function unjailWithAuthorization(address validatorOperator) external returns (bool success);

    /// @notice Reverts: unjail authorizations are not supported.
    function revokeUnjailAuthorization(address grantee) external returns (bool success);
}
