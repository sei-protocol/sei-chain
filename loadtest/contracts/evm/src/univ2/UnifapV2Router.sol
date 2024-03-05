// SPDX-License-Identifier: MIT
pragma solidity >=0.8.10;

import "./interfaces/IUnifapV2Factory.sol";
import "./interfaces/IUnifapV2Pair.sol";
import "./interfaces/IERC20.sol";
import "./libraries/UnifapV2Library.sol";

contract UnifapV2Router {
    // ========= Custom Errors =========

    error Expired();
    error SafeTransferFromFailed();
    error InsufficientAmountA();
    error InsufficientAmountB();

    // ========= State Variables =========

    IUnifapV2Factory public immutable factory;

    // ========= Constructor =========

    constructor(address _factory) {
        factory = IUnifapV2Factory(_factory);
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
        liquidity = IUnifapV2Pair(pair).mint(to);
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
        (uint256 amount0, uint256 amount1) = IUnifapV2Pair(pair).burn(to);
        (address token0, ) = UnifapV2Library.sortPairs(tokenA, tokenB);
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

        (uint112 reserveA, uint112 reserveB) = UnifapV2Library.getReserves(
            address(factory),
            tokenA,
            tokenB
        );
        if (reserveA == 0 && reserveB == 0) {
            (amountA, amountB) = (amountADesired, amountBDesired);
        } else {
            amountB = UnifapV2Library.quote(amountADesired, reserveA, reserveB);
            if (amountB <= amountBDesired) {
                if (amountB < amountBMin) revert InsufficientAmountB();
                amountA = amountADesired;
            } else {
                amountA = UnifapV2Library.quote(
                    amountBDesired,
                    reserveB,
                    reserveA
                );
                assert(amountA <= amountADesired);

                if (amountA < amountAMin) revert InsufficientAmountA();
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
        if (!success) revert SafeTransferFromFailed();
    }
}
