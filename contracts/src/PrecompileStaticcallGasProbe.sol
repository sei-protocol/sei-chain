// SPDX-License-Identifier: MIT
pragma solidity ^0.8.28;

interface IERC20View {
    function totalSupply() external view returns (uint256);
    function balanceOf(address owner) external view returns (uint256);
}

contract PrecompileStaticcallGasProbe {
    event ProbeTotalSupply(address indexed pointer, uint256 supply);
    event ProbeBalance(address indexed pointer, address indexed owner, uint256 balance);
    event ProbeBalanceLoop(address indexed pointer, address indexed owner, uint256 total, uint256 iterations);

    function probeTotalSupply(address pointer) external returns (uint256) {
        uint256 supply = IERC20View(pointer).totalSupply();
        emit ProbeTotalSupply(pointer, supply);
        return supply;
    }

    function probeBalance(address pointer, address owner) external returns (uint256) {
        uint256 balance = IERC20View(pointer).balanceOf(owner);
        emit ProbeBalance(pointer, owner, balance);
        return balance;
    }

    function probeBalanceLoop(address pointer, address owner, uint256 iterations) external returns (uint256) {
        require(iterations > 0, "iterations must be > 0");
        uint256 total = 0;
        for (uint256 i = 0; i < iterations; i++) {
            total += IERC20View(pointer).balanceOf(owner);
        }
        emit ProbeBalanceLoop(pointer, owner, total, iterations);
        return total;
    }
}
