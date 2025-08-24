// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

contract PaywordSeiSettlement {
    address public deployer;

    struct Payment {
        address from;
        address to;
        uint256 amount;
        bytes32 invoiceHash;
        string memo;
        uint256 timestamp;
    }

    Payment[] public settlements;

    event PaymentSettled(
        address indexed from,
        address indexed to,
        uint256 amount,
        bytes32 invoiceHash,
        string memo,
        uint256 timestamp
    );

    modifier onlyDeployer() {
        require(msg.sender == deployer, "Only deployer");
        _;
    }

    constructor() {
        deployer = msg.sender;
    }

    function settle(
        address to,
        uint256 amount,
        bytes32 invoiceHash,
        string calldata memo
    ) external payable {
        require(msg.value == amount, "Incorrect ETH sent");

        settlements.push(Payment({
            from: msg.sender,
            to: to,
            amount: amount,
            invoiceHash: invoiceHash,
            memo: memo,
            timestamp: block.timestamp
        }));

        payable(to).transfer(amount);
        emit PaymentSettled(msg.sender, to, amount, invoiceHash, memo, block.timestamp);
    }

    function getSettlementCount() external view returns (uint256) {
        return settlements.length;
    }

    function getSettlement(uint256 index) external view returns (Payment memory) {
        return settlements[index];
    }
}
