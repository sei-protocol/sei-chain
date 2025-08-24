// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

contract PaywordSettlement {
    struct Payment {
        address payer;
        address recipient;
        uint256 amount;
        bytes32 payword;
        uint256 timestamp;
    }

    mapping(bytes32 => Payment) public payments;

    event PaymentSettled(address indexed payer, address indexed recipient, uint256 amount, bytes32 payword);

    function settle(address recipient, uint256 amount, bytes32 payword) external payable {
        require(msg.value == amount, "Incorrect value");
        payments[payword] = Payment(msg.sender, recipient, amount, payword, block.timestamp);
        payable(recipient).transfer(amount);
        emit PaymentSettled(msg.sender, recipient, amount, payword);
    }

    function getPayment(bytes32 payword) external view returns (Payment memory) {
        return payments[payword];
    }
}
