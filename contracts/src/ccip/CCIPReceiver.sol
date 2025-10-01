// SPDX-License-Identifier: MIT
pragma solidity ^0.8.24;

import {Client} from "./Client.sol";

/// @notice Simplified version of the Chainlink CCIP receiver utility.
/// @dev The full Chainlink implementation includes fee payment and interface detection. For
///      settlement tests inside this repository we only need router validation and the hook.
abstract contract CCIPReceiver {
    /// @notice Thrown when a call does not originate from the configured CCIP router.
    error InvalidRouter(address sender);

    /// @notice Thrown when attempting to configure the receiver with the zero address router.
    error ZeroAddress();

    address private immutable i_router;

    constructor(address router) {
        if (router == address(0)) {
            revert ZeroAddress();
        }
        i_router = router;
    }

    /// @return router The Chainlink CCIP router permitted to call {ccipReceive}.
    function ccipRouter() public view returns (address router) {
        router = i_router;
    }

    /// @notice Entry point invoked by the CCIP router.
    /// @param message The CCIP message that was delivered to this chain.
    function ccipReceive(Client.Any2EVMMessage calldata message) external virtual {
        if (msg.sender != i_router) {
            revert InvalidRouter(msg.sender);
        }
        _ccipReceive(message);
    }

    /// @notice Implement settlement logic inside inheriting contracts.
    function _ccipReceive(Client.Any2EVMMessage memory message) internal virtual;
}
