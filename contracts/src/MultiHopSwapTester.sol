// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

import "@openzeppelin/contracts/token/ERC20/ERC20.sol";

/**
 * @title SimpleToken
 * @dev Minimal ERC20 token for testing multi-hop swaps.
 *      Deployer gets an initial mint and can mint more for testing.
 */
contract SimpleToken is ERC20 {
    constructor(string memory name_, string memory symbol_, uint256 initialSupply) ERC20(name_, symbol_) {
        _mint(msg.sender, initialSupply);
    }

    function mint(address to, uint256 amount) external {
        _mint(to, amount);
    }
}

/**
 * @title SimplePair
 * @dev Minimal Uniswap V2-style constant-product AMM pair.
 *      Holds reserves of two tokens and allows swaps between them.
 *      No LP tokens, fees, or flash-loan protection — just the swap math
 *      needed to exercise multi-hop cross-contract token transfers.
 */
contract SimplePair {
    address public token0;
    address public token1;
    uint256 public reserve0;
    uint256 public reserve1;

    event Swap(address indexed sender, uint256 amount0In, uint256 amount1In, uint256 amount0Out, uint256 amount1Out, address indexed to);
    event Sync(uint256 reserve0, uint256 reserve1);

    constructor(address _token0, address _token1) {
        token0 = _token0;
        token1 = _token1;
    }

    /**
     * @dev Add initial liquidity. Tokens must already be transferred to this contract.
     */
    function addLiquidity() external {
        reserve0 = ERC20(token0).balanceOf(address(this));
        reserve1 = ERC20(token1).balanceOf(address(this));
        emit Sync(reserve0, reserve1);
    }

    /**
     * @dev Swap: caller sends tokenIn to this contract first, then calls swap.
     *      Calculates output using x*y=k (no fee for simplicity).
     * @param amountIn Amount of input token already transferred to this contract
     * @param tokenIn Address of the input token
     * @param to Recipient of the output tokens
     * @return amountOut Amount of output tokens sent
     */
    function swap(uint256 amountIn, address tokenIn, address to) external returns (uint256 amountOut) {
        require(tokenIn == token0 || tokenIn == token1, "invalid token");
        require(amountIn > 0, "zero input");

        bool isToken0 = (tokenIn == token0);
        (uint256 resIn, uint256 resOut) = isToken0 ? (reserve0, reserve1) : (reserve1, reserve0);
        address tokenOut = isToken0 ? token1 : token0;

        // x * y = k, so amountOut = resOut - k / (resIn + amountIn)
        // With 0.3% fee: amountInWithFee = amountIn * 997
        uint256 amountInWithFee = amountIn * 997;
        amountOut = (amountInWithFee * resOut) / (resIn * 1000 + amountInWithFee);
        require(amountOut > 0, "insufficient output");

        // Transfer output to recipient
        ERC20(tokenOut).transfer(to, amountOut);

        // Update reserves
        reserve0 = ERC20(token0).balanceOf(address(this));
        reserve1 = ERC20(token1).balanceOf(address(this));

        if (isToken0) {
            emit Swap(msg.sender, amountIn, 0, 0, amountOut, to);
        } else {
            emit Swap(msg.sender, 0, amountIn, amountOut, 0, to);
        }
        emit Sync(reserve0, reserve1);
    }
}

/**
 * @title SimpleRouter
 * @dev Minimal multi-hop swap router. Chains swaps through multiple pairs,
 *      transferring tokens between contracts at each hop — exactly like
 *      the Dragonswap router call that triggered the AppHash divergence.
 *
 *      The key property this exercises: many cross-contract ERC20 transferFrom/
 *      transfer calls within a single top-level tx, each going through
 *      separate EVM CALL frames with separate EVM snapshots.
 */
contract SimpleRouter {
    /**
     * @dev Execute a multi-hop swap through a series of pairs.
     * @param amountIn Amount of the first token to swap
     * @param path Array of token addresses representing the swap path
     *             e.g. [tokenA, tokenB, tokenC] swaps A→B then B→C
     * @param pairs Array of pair addresses for each hop
     *             e.g. [pairAB, pairBC]
     * @param to Final recipient of the output tokens
     * @return amountOut Final output amount
     */
    function swapExactTokensForTokens(
        uint256 amountIn,
        address[] calldata path,
        address[] calldata pairs,
        address to
    ) external returns (uint256 amountOut) {
        require(path.length >= 2, "path too short");
        require(pairs.length == path.length - 1, "pairs/path mismatch");

        // Pull input tokens from sender to the first pair
        ERC20(path[0]).transferFrom(msg.sender, pairs[0], amountIn);

        // Chain swaps through each pair
        amountOut = amountIn;
        for (uint256 i = 0; i < pairs.length; i++) {
            address recipient = (i < pairs.length - 1) ? pairs[i + 1] : to;
            amountOut = SimplePair(pairs[i]).swap(amountOut, path[i], recipient);
        }
    }
}
