// SPDX-License-Identifier: UNLICENSED
pragma solidity ^0.8.20;

contract OmegaPulseWatcher {
    event PulseSignal(bytes32 indexed id, address indexed sender, uint256 timestamp, string tag);

    address public immutable vaultScanner;
    address public pulseOperator;

    modifier onlyPulseOperator() {
        require(msg.sender == pulseOperator, "Not authorized");
        _;
    }

    constructor(address _vaultScanner, address _pulseOperator) {
        vaultScanner = _vaultScanner;
        pulseOperator = _pulseOperator;
    }

    function sendPulse(bytes32 id, string memory tag) external onlyPulseOperator {
        emit PulseSignal(id, msg.sender, block.timestamp, tag);
    }

    function updatePulseOperator(address newOp) external onlyPulseOperator {
        pulseOperator = newOp;
    }
}
