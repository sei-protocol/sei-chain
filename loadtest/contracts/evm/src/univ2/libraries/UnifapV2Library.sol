// SPDX-License-Identifier: MIT
pragma solidity >=0.8.10;

import "../interfaces/IUnifapV2Pair.sol";

/// @title UnifapV2Library
/// @author Uniswap Labs
/// @notice Provides common functionality for UnifapV2 Contracts
library UnifapV2Library {
    function sortPairs(address token0, address token1)
        internal
        pure
        returns (address, address)
    {
        return token0 < token1 ? (token0, token1) : (token1, token0);
    }

    function quote(
        uint256 amount0,
        uint256 reserve0,
        uint256 reserve1
    ) internal pure returns (uint256) {
        return (amount0 * reserve1) / reserve0;
    }

    function getReserves(
        address factory,
        address tokenA,
        address tokenB
    ) internal view returns (uint112 reserveA, uint112 reserveB) {
        (address token0, address token1) = sortPairs(tokenA, tokenB);
        (uint112 reserve0, uint112 reserve1, ) = IUnifapV2Pair(
            pairFor(factory, token0, token1)
        ).getReserves();
        (reserveA, reserveB) = tokenA == token0
            ? (reserve0, reserve1)
            : (reserve1, reserve0);
    }

    // calculates the CREATE2 address for a pair without making any external calls
    function pairFor(
        address factory,
        address tokenA,
        address tokenB
    ) internal pure returns (address pair) {
        pair = address(
            uint160(
                uint256(
                    keccak256(
                        abi.encodePacked(
                            hex"ff",
                            factory,
                            keccak256(abi.encodePacked(tokenA, tokenB)),
                            hex"c302b13384af22f2ca10ffae7c2446a6fb5da0a895f0e211d72f313408acf32a" // init code hash
                        )
                    )
                )
            )
        );
    }
}
