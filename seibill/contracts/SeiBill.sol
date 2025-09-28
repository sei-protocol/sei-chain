// SPDX-License-Identifier: MIT
pragma solidity ^0.8.24;

interface IERC20 {
    function transferFrom(address sender, address recipient, uint256 amount) external returns (bool);
    function transfer(address recipient, uint256 amount) external returns (bool);
}

contract SeiBill {
    address public usdc;
    address public admin;
    mapping(address => bool) public bridgeRouters;

    struct Bill {
        address payer;
        address payee;
        uint256 amount;
        uint256 dueDate;
        bool paid;
    }

    mapping(bytes32 => Bill) public bills;

    event BillScheduled(bytes32 indexed billId, address payer, address payee, uint256 amount, uint256 dueDate);
    event BillPaid(bytes32 indexed billId, uint256 amount);
    event BridgeDeposit(bytes32 indexed billId, address indexed payer, uint256 amount, address indexed router);

    constructor(address _usdc) {
        usdc = _usdc;
        admin = msg.sender;
    }

    function setBridgeRouter(address router, bool approved) external {
        require(msg.sender == admin, "Not admin");
        bridgeRouters[router] = approved;
    }

    function scheduleBill(address payee, uint256 amount, uint256 dueDate) external returns (bytes32) {
        bytes32 billId = keccak256(abi.encodePacked(msg.sender, payee, amount, dueDate, block.timestamp));
        bills[billId] = Bill(msg.sender, payee, amount, dueDate, false);
        emit BillScheduled(billId, msg.sender, payee, amount, dueDate);
        return billId;
    }

    function payBill(bytes32 billId) external {
        Bill storage bill = bills[billId];
        require(block.timestamp >= bill.dueDate, "Too early");
        require(!bill.paid, "Already paid");
        require(msg.sender == bill.payer, "Not authorized");

        bill.paid = true;
        require(IERC20(usdc).transferFrom(msg.sender, bill.payee, bill.amount), "Transfer failed");
        emit BillPaid(billId, bill.amount);
    }

    function depositFromBridge(bytes32 billId, address payer) external {
        require(bridgeRouters[msg.sender], "Router not allowed");
        Bill storage bill = bills[billId];
        require(block.timestamp >= bill.dueDate, "Too early");
        require(!bill.paid, "Already paid");
        require(payer == bill.payer, "Wrong payer");

        bill.paid = true;
        require(IERC20(usdc).transfer(bill.payee, bill.amount), "Transfer failed");
        emit BillPaid(billId, bill.amount);
        emit BridgeDeposit(billId, payer, bill.amount, msg.sender);
    }
}
