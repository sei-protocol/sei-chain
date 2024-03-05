// SPDX-License-Identifier: MIT
pragma solidity >=0.8.10;

interface IUniswapV2Factory {
    function pairs(address tokenA, address tokenB)
        external
        view
        returns (address);

    function createPair(address, address) external returns (address);
}
