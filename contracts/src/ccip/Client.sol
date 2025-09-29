// SPDX-License-Identifier: MIT
pragma solidity ^0.8.24;

/// @notice Minimal subset of Chainlink CCIP client structs required by the SeiKinSettlement contract.
/// @dev The real Chainlink library exposes additional fields and helper methods. This lightweight
///      version is sufficient for compilation and local testing while keeping the dependency surface
///      minimal inside this repository.
library Client {
    /// @notice Token and amount bridged alongside a CCIP message.
    struct EVMTokenAmount {
        address token;
        uint256 amount;
    }

    /// @notice Message payload delivered by the CCIP router when targeting an EVM chain.
    struct Any2EVMMessage {
        bytes32 messageId;
        uint64 sourceChainSelector;
        bytes sender;
        bytes data;
        EVMTokenAmount[] destTokenAmounts;
        address payable receiver;
        bytes extraArgs;
        uint256 feeTokenAmount;
    }
}
