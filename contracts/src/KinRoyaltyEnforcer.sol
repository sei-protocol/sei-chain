// SPDX-License-Identifier: UNLICENSED
pragma solidity ^0.8.28;

contract KinRoyaltyEnforcer {
    address public royaltySink;
    uint256 public constant SSTORE_GAS_COST = 72_000;

    event RoyaltyPaid(address indexed from, uint256 amount);

    constructor(address sink) {
        royaltySink = sink;
    }

    function enforceRoyalty(uint256 gasUsed) external payable {
        require(gasUsed >= SSTORE_GAS_COST, "not a storage-heavy action");

        uint256 expected = (gasUsed * tx.gasprice) / 10;
        require(msg.value >= expected, "royalty underpaid");

        (bool sent, ) = royaltySink.call{value: expected}("");
        require(sent, "royalty payment failed");

        emit RoyaltyPaid(msg.sender, expected);
    }

    function updateSink(address newSink) external {
        require(msg.sender == royaltySink, "not authorized");
        royaltySink = newSink;
    }
}
