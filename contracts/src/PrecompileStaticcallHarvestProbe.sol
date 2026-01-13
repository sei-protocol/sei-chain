// SPDX-License-Identifier: MIT
pragma solidity ^0.8.28;

interface IERC20View {
    function totalSupply() external view returns (uint256);
    function balanceOf(address owner) external view returns (uint256);
}

interface IERC20Transfer {
    function transfer(address to, uint256 amount) external returns (bool);
}

contract PrecompileStaticcallHarvestProbe {
    address public immutable pointer;
    uint256 public immutable iterations;
    bool public immutable doTransfer;

    bytes32 private constant HARVEST_TOPIC =
        0xc9695243a805adb74c91f28311176c65b417e842d5699893cef56d18bfa48cba;

    constructor(address pointer_, uint256 iterations_, bool doTransfer_) {
        require(pointer_ != address(0), "pointer required");
        require(iterations_ > 0, "iterations must be > 0");
        pointer = pointer_;
        iterations = iterations_;
        doTransfer = doTransfer_;
    }

    function harvest() external returns (uint256) {
        uint256 total = IERC20View(pointer).totalSupply();
        uint256 lastBalance = 0;
        for (uint256 i = 0; i < iterations; i++) {
            lastBalance = IERC20View(pointer).balanceOf(address(this));
            total += lastBalance;
        }
        if (total == 0) {
            return 0;
        }
        if (doTransfer && lastBalance > 0) {
            require(IERC20Transfer(pointer).transfer(msg.sender, lastBalance), "transfer failed");
        }
        _emitHarvestLog(block.timestamp, iterations, lastBalance);
        return lastBalance;
    }

    function _emitHarvestLog(uint256 ts, uint256 count, uint256 amount) private {
        assembly {
            let ptr := mload(0x40)
            mstore(ptr, ts)
            mstore(add(ptr, 0x20), count)
            mstore(add(ptr, 0x40), amount)
            log1(ptr, 0x60, HARVEST_TOPIC)
        }
    }
}
