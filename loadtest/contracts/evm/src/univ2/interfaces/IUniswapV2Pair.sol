// SPDX-License-Identifier: MIT
pragma solidity >=0.8.10;

interface IUniswapV2Pair {
    function initialize(address, address) external;

    function getReserves()
        external
        view
        returns (
            uint112,
            uint112,
            uint32
        );

    function mint(address) external returns (uint256);

    function burn(address) external returns (uint256, uint256);

    function swap(
        uint256,
        uint256,
        address
    ) external;

    function sync() external;
}
