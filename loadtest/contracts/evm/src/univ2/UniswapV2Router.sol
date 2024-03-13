// SPDX-License-Identifier: MIT
pragma solidity >=0.8.10;

import "./interfaces/IUniswapV2Factory.sol";
import "./interfaces/IUniswapV2Pair.sol";
import "./interfaces/IERC20.sol";
import "./libraries/UniswapV2Library.sol";

contract UniswapV2Router {
    // ========= Custom Errors =========

    error Expired();
    error SafeTransferFromFailed();
    error InsufficientAmountA();
    error InsufficientAmountB();

    // ========= State Variables =========

    IUniswapV2Factory public immutable factory;

    // ========= Constructor =========

    constructor(address _factory) {
        factory = IUniswapV2Factory(_factory);
    }

    // ========= Modifiers =========
    modifier check(uint256 deadline) {
        if (block.timestamp > deadline) revert Expired();
        _;
    }

    // ========= Public Functions =========

    /// @notice Add liquidity to token pool pair
    /// @dev Creates pair if not already created
    /// @param tokenA The first token
    /// @param tokenB The second token
    /// @param amountADesired The amount of tokenA desired
    /// @param amountBDesired The amount of tokenB desired
    /// @param amountAMin The minimum amount of tokenA to transfer
    /// @param amountBMin The minimum amount of tokenB to transfer
    /// @param to The address to transfer liquidity to
    /// @param deadline The deadline for the transaction
    /// @return amountA Amount of tokenA to transfer
    /// @return amountB Amount of tokenB to transfer
    /// @return liquidity Amount of liquidity transfered
    function addLiquidity(
        address tokenA,
        address tokenB,
        uint256 amountADesired,
        uint256 amountBDesired,
        uint256 amountAMin,
        uint256 amountBMin,
        address to,
        uint256 deadline
    )
        public
        check(deadline)
        returns (
            uint256 amountA,
            uint256 amountB,
            uint256 liquidity
        )
    {
        (amountA, amountB) = _computeLiquidityAmounts(
            tokenA,
            tokenB,
            amountADesired,
            amountBDesired,
            amountAMin,
            amountBMin
        );
        address pair = factory.pairs(tokenA, tokenB);
        _safeTransferFrom(tokenA, msg.sender, pair, amountA);
        _safeTransferFrom(tokenB, msg.sender, pair, amountB);
        liquidity = IUniswapV2Pair(pair).mint(to);
    }

    /// @notice Remove liquidity from token pool pair
    /// @param tokenA The first token
    /// @param tokenB The second token
    /// @param liquidity The amount of liquidity token to remove
    /// @param amountAMin The minimum amount of tokenA needed
    /// @param amountBMin The minimum amount of tokenB needed
    /// @param to The address to transfer pair contracts to
    /// @param deadline The deadline for the transaction
    function removeLiquidity(
        address tokenA,
        address tokenB,
        uint256 liquidity,
        uint256 amountAMin,
        uint256 amountBMin,
        address to,
        uint256 deadline
    ) public check(deadline) returns (uint256 amountA, uint256 amountB) {
        address pair = factory.pairs(tokenA, tokenB);
        _safeTransferFrom(address(pair), msg.sender, address(pair), liquidity);
        (uint256 amount0, uint256 amount1) = IUniswapV2Pair(pair).burn(to);
        (address token0, ) = UniswapV2Library.sortPairs(tokenA, tokenB);
        (amountA, amountB) = token0 == tokenA
            ? (amount0, amount1)
            : (amount1, amount0);
        if (amountA < amountAMin) revert InsufficientAmountA();
        if (amountB < amountBMin) revert InsufficientAmountB();
    }

    // ========= Internal Functions =========

    /// @notice computes token amounts according to marginal prices to be transfered
    /// @dev Creates a token pool pair if not already created
    function _computeLiquidityAmounts(
        address tokenA,
        address tokenB,
        uint256 amountADesired,
        uint256 amountBDesired,
        uint256 amountAMin,
        uint256 amountBMin
    ) internal returns (uint256 amountA, uint256 amountB) {
        if (factory.pairs(tokenA, tokenB) == address(0)) {
            factory.createPair(tokenA, tokenB);
        }

        // require(false, string(abi.encodePacked(factory)));

        (uint112 reserveA, uint112 reserveB) = UniswapV2Library.getReserves(
            address(factory),
            tokenA,
            tokenB
        );
        // require(false, "after getReserves");
        if (reserveA == 0 && reserveB == 0) {
            (amountA, amountB) = (amountADesired, amountBDesired);
        } else {
            amountB = UniswapV2Library.quote(amountADesired, reserveA, reserveB);
            if (amountB <= amountBDesired) {
                if (amountB < amountBMin) {
                    require(false, "InsufficientAmountB");
                    revert InsufficientAmountB();
                }
                amountA = amountADesired;
            } else {
                amountA = UniswapV2Library.quote(
                    amountBDesired,
                    reserveB,
                    reserveA
                );
                assert(amountA <= amountADesired);

                if (amountA < amountAMin) {
                    require(false, "InsufficientAmountA");
                    revert InsufficientAmountA();
                }
                amountB = amountBDesired;
            }
        }
    }

    function _safeTransferFrom(
        address token,
        address from,
        address to,
        uint256 amount
    ) internal returns (bool success) {
        success = IERC20(token).transferFrom(from, to, amount);
        if (!success) {
            require(false, "SafeTransferFromFailed");
            revert SafeTransferFromFailed();
        }
    }

    // **** SWAP ****
    // requires the initial amount to have already been sent to the first pair
    function _swap(uint[] memory amounts, address[] memory path, address _to) internal virtual {
        for (uint i; i < path.length - 1; i++) {
            (address input, address output) = (path[i], path[i + 1]);
            (address token0,) = UniswapV2Library.sortPairs(input, output);
            uint amountOut = amounts[i + 1];
            (uint amount0Out, uint amount1Out) = input == token0 ? (uint(0), amountOut) : (amountOut, uint(0));

            address to = i < path.length - 2 ? UniswapV2Library.pairFor(address(factory), output, path[i + 2]) : _to;
            IUniswapV2Pair(UniswapV2Library.pairFor(address(factory), input, output)).swap(
                amount0Out, amount1Out, to
            );
        }
    }

    function swapExactTokensForTokens(
        uint amountIn,
        uint amountOutMin,
        address[] calldata path,
        address to,
        uint deadline
    ) external check(deadline) returns (uint[] memory amounts) {
        amounts = UniswapV2Library.getAmountsOut(address(factory), amountIn, path);
        require(amounts[amounts.length - 1] >= amountOutMin, 'UniswapV2Router: INSUFFICIENT_OUTPUT_AMOUNT');
        (address token0, address token1) = UniswapV2Library.sortPairs(path[0], path[1]);
        address pair = factory.pairs(token0, token1);
        _safeTransferFrom(
            path[0], msg.sender, pair, amounts[0]
        );
        _swap(amounts, path, to);
    }
}
