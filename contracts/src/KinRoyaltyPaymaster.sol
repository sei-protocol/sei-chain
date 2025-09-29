// SPDX-License-Identifier: UNLICENSED
pragma solidity ^0.8.20;

interface IPaymasterGas {
    function payGas(address user, uint256 gasUsed) external;
}

/// @title KinRoyaltyPaymaster
/// @notice Manages gas sponsorship for Kin-linked accounts that are eligible for royalty-funded execution.
contract KinRoyaltyPaymaster is IPaymasterGas {
    /// @notice Account allowed to maintain the allow-list and operator state.
    address public operator;

    /// @notice Tracks which Kin accounts are eligible for sponsorship.
    mapping(address => bool) public allowedUsers;

    /// @notice Emitted when the operator role is transferred to a new account.
    event OperatorUpdated(address indexed newOperator);

    /// @notice Emitted whenever user sponsorship permissions change.
    event UserPermissionSet(address indexed user, bool allowed);

    /// @notice Emitted when gas is sponsored for an allowed user.
    event GasSponsored(address indexed user, uint256 gasUsed);

    modifier onlyOperator() {
        require(msg.sender == operator, "Not operator");
        _;
    }

    /// @param _operator Initial operator responsible for maintaining allowances. Cannot be zero.
    constructor(address _operator) {
        require(_operator != address(0), "Operator zero");
        operator = _operator;
        emit OperatorUpdated(_operator);
    }

    /// @notice Updates the operator controlling sponsorship permissions.
    /// @param newOperator The new operator account.
    function setOperator(address newOperator) external onlyOperator {
        require(newOperator != address(0), "Operator zero");
        operator = newOperator;
        emit OperatorUpdated(newOperator);
    }

    /// @notice Adds or removes Kin accounts from the sponsorship allow-list.
    /// @param user The Kin-linked account to update.
    /// @param allowed Whether the user should receive gas sponsorship.
    function setUser(address user, bool allowed) external onlyOperator {
        allowedUsers[user] = allowed;
        emit UserPermissionSet(user, allowed);
    }

    /// @notice Pays for gas consumption of Kin accounts authorized for royalty sponsorship.
    /// @dev In a production deployment this function would transfer funds to cover the gas usage.
    /// @param user The Kin account whose gas should be sponsored.
    /// @param gasUsed Amount of gas (in units) consumed by the transaction.
    function payGas(address user, uint256 gasUsed) external override {
        require(allowedUsers[user], "Not allowed");
        emit GasSponsored(user, gasUsed);
    }
}
