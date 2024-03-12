// SPDX-License-Identifier: MIT
pragma solidity >=0.8.10;

interface IERC20 {
    function mint(address to, uint256 amount) external;

    function balanceOf(address) external view returns (uint256);

    function transfer(address to, uint256 amount) external returns (bool);

    function transferFrom(
        address from,
        address to,
        uint256 amount
    ) external returns (bool);

    function approve(address spender, uint256 amount) external returns (bool);
}
