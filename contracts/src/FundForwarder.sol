// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

/**
 * @title FundForwarder
 * @notice A simple contract that forwards all its native token balance to a configured destination address.
 * @dev The destination address is set at deployment and cannot be changed.
 *      Anyone can call SendFunds() to trigger the transfer (permissionless).
 *      This contract does NOT have receive() or fallback() functions, so it cannot receive
 *      funds via direct EVM transfers. It can only receive funds via Cosmos bank send,
 *      which operates at the Cosmos layer and bypasses EVM payability checks.
 */
contract FundForwarder {
    address payable public destinationAddress;

    event FundsSent(address indexed destination, uint256 amount);

    /**
     * @notice Constructor sets the destination address for fund forwarding
     * @param _destinationAddress The address that will receive funds when SendFunds() is called
     */
    constructor(address payable _destinationAddress) {
        require(_destinationAddress != address(0), "Destination cannot be zero address");
        destinationAddress = _destinationAddress;
    }

    // NOTE: No receive() or fallback() functions - this contract intentionally
    // rejects direct EVM transfers. Funds can only be sent via Cosmos bank send.

    /**
     * @notice Sends the entire contract balance to the configured destination address
     * @dev This function is permissionless - anyone can call it
     */
    function SendFunds() external {
        uint256 balance = address(this).balance;
        require(balance > 0, "No funds to send");

        (bool success, ) = destinationAddress.call{value: balance}("");
        require(success, "Transfer failed");

        emit FundsSent(destinationAddress, balance);
    }

    /**
     * @notice Returns the current balance of the contract
     * @return The balance in wei
     */
    function getBalance() external view returns (uint256) {
        return address(this).balance;
    }
}
