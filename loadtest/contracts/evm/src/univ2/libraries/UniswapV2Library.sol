// SPDX-License-Identifier: MIT
pragma solidity >=0.8.10;

import "../interfaces/IUniswapV2Pair.sol";
import "../interfaces/IUniswapV2Factory.sol";

/// @title UniswapV2Library
/// @author Uniswap Labs
/// @notice Provides common functionality for UniswapV2 Contracts
library UniswapV2Library {
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
        IUniswapV2Pair pair = IUniswapV2Pair(IUniswapV2Factory(factory).pairs(token0, token1));
        (uint112 reserve0, uint112 reserve1, ) = pair.getReserves();
        (reserveA, reserveB) = tokenA == token0
            ? (reserve0, reserve1)
            : (reserve1, reserve0);
    }

        // given an input amount of an asset and pair reserves, returns the maximum output amount of the other asset
    function getAmountOut(uint amountIn, uint reserveIn, uint reserveOut) internal pure returns (uint amountOut) {
        require(amountIn > 0, 'UniswapV2Library: INSUFFICIENT_INPUT_AMOUNT');
        require(reserveIn > 0 && reserveOut > 0, 'UniswapV2Library: INSUFFICIENT_LIQUIDITY');
        uint amountInWithFee = amountIn * 997;
        uint numerator = amountInWithFee * reserveOut;
        uint denominator = reserveIn * 1000 + amountInWithFee;
        amountOut = numerator / denominator;
    }

        // performs chained getAmountOut calculations on any number of pairs
    function getAmountsOut(address factory, uint amountIn, address[] memory path) internal view returns (uint[] memory amounts) {
        require(path.length >= 2, 'UniswapV2Library: INVALID_PATH');
        amounts = new uint[](path.length);
        amounts[0] = amountIn;
        for (uint i; i < path.length - 1; i++) {
            (uint reserveIn, uint reserveOut) = getReserves(factory, path[i], path[i + 1]);
            amounts[i + 1] = getAmountOut(amounts[i], reserveIn, reserveOut);
        }
    }

    function pairFor(address factory, address tokenA, address tokenB) internal view returns (address) {
        (address token1, address token2) = UniswapV2Library.sortPairs(tokenA, tokenB);
        return IUniswapV2Factory(factory).pairs(token1, token2);
    }


    // somehow doesn't work as expected--don't use

    // // calculates the CREATE2 address for a pair without making any external calls
    // function pairFor(
    //     address factory,
    //     address tokenA,
    //     address tokenB
    // ) internal pure returns (address pair) {
    //     pair = address(
    //         uint160(
    //             uint256(
    //                 keccak256(
    //                     abi.encodePacked(
    //                         hex"ff",
    //                         factory,
    //                         keccak256(abi.encodePacked(tokenA, tokenB)),
    //                         hex"c302b13384af22f2ca10ffae7c2446a6fb5da0a895f0e211d72f313408acf32a" // init code hash
    //                     )
    //                 )
    //             )
    //         )
    //     );
    // }
}
