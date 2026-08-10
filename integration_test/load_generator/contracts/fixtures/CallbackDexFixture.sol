// SPDX-License-Identifier: MIT
pragma solidity ^0.8.28;

import {IERC20Fixture, SafeToken, FixtureReentrancyGuard} from "./FixtureCore.sol";

interface IFixtureSwapCallback {
    function uniswapV3SwapCallback(int256 amount0Delta, int256 amount1Delta, bytes calldata data)
        external;
}

contract DeterministicV3Pool is FixtureReentrancyGuard {
    using SafeToken for address;

    address public immutable token0;
    address public immutable token1;
    uint16 public immutable feeBps;
    uint256 public immutable maxSwapAmount;

    event Swap(
        address indexed sender,
        address indexed recipient,
        int256 amount0,
        int256 amount1,
        uint256 amountOut
    );

    constructor(address token0_, address token1_, uint16 feeBps_, uint256 maxSwapAmount_) {
        require(token0_ != address(0) && token1_ != address(0) && token0_ != token1_, "BAD_TOKENS");
        require(token0_.code.length != 0 && token1_.code.length != 0, "TOKEN_NO_CODE");
        require(feeBps_ <= 1_000 && maxSwapAmount_ != 0, "BAD_PRICING");
        token0 = token0_;
        token1 = token1_;
        feeBps = feeBps_;
        maxSwapAmount = maxSwapAmount_;
    }

    function quoteExactInput(uint256 amountIn) public view returns (uint256) {
        require(amountIn != 0 && amountIn <= maxSwapAmount, "AMOUNT_OUT_OF_RANGE");
        return amountIn * (10_000 - feeBps) / 10_000;
    }

    function swap(address recipient, bool zeroForOne, uint256 amountIn, bytes calldata data)
        external
        nonReentrant
        returns (int256 amount0, int256 amount1)
    {
        require(recipient != address(0), "ZERO_RECIPIENT");
        uint256 amountOut = quoteExactInput(amountIn);
        address input = zeroForOne ? token0 : token1;
        address output = zeroForOne ? token1 : token0;

        output.safeTransfer(recipient, amountOut);
        uint256 balanceBefore = SafeToken.staticBalanceOf(input, address(this));

        amount0 = zeroForOne ? int256(amountIn) : -int256(amountOut);
        amount1 = zeroForOne ? -int256(amountOut) : int256(amountIn);
        IFixtureSwapCallback(msg.sender).uniswapV3SwapCallback(amount0, amount1, data);

        uint256 balanceAfter = SafeToken.staticBalanceOf(input, address(this));
        require(balanceAfter >= balanceBefore + amountIn, "CALLBACK_UNDERPAID");
        emit Swap(msg.sender, recipient, amount0, amount1, amountOut);
    }
}

contract ProductionShapedSwapRouter is IFixtureSwapCallback, FixtureReentrancyGuard {
    using SafeToken for address;

    struct ExactInputSingleParams {
        address tokenIn;
        address tokenOut;
        uint24 fee;
        address recipient;
        uint256 amountIn;
        uint256 amountOutMinimum;
        uint160 sqrtPriceLimitX96;
    }

    struct PendingSwap {
        address pool;
        address tokenIn;
        uint256 amountIn;
        bool active;
    }

    address public immutable owner;
    mapping(bytes32 => address) public poolForPair;
    PendingSwap private pendingSwap;

    event PoolConfigured(address indexed token0, address indexed token1, uint24 fee, address pool);
    event ExactInputExecuted(address indexed sender, address indexed pool, uint256 amountIn, uint256 amountOut);

    constructor(address owner_) {
        require(owner_ != address(0), "ZERO_OWNER");
        owner = owner_;
    }

    function configurePool(address tokenA, address tokenB, uint24 fee, address pool) external {
        require(msg.sender == owner, "NOT_OWNER");
        require(tokenA != tokenB && pool.code.length != 0 && fee <= 100_000, "BAD_POOL");
        (address a, address b) = tokenA < tokenB ? (tokenA, tokenB) : (tokenB, tokenA);
        require(
            DeterministicV3Pool(pool).token0() == a && DeterministicV3Pool(pool).token1() == b,
            "POOL_TOKEN_MISMATCH"
        );
        poolForPair[keccak256(abi.encode(a, b, fee))] = pool;
        emit PoolConfigured(a, b, fee, pool);
    }

    function exactInputSingle(ExactInputSingleParams calldata params)
        external
        nonReentrant
        returns (uint256 amountOut)
    {
        require(params.recipient != address(0) && params.amountIn != 0, "BAD_PARAMS");
        require(params.sqrtPriceLimitX96 == 0, "PRICE_LIMIT_UNSUPPORTED");
        (address a, address b) =
            params.tokenIn < params.tokenOut ? (params.tokenIn, params.tokenOut) : (params.tokenOut, params.tokenIn);
        address pool = poolForPair[keccak256(abi.encode(a, b, params.fee))];
        require(pool != address(0), "POOL_NOT_ALLOWED");

        params.tokenIn.safeTransferFrom(msg.sender, address(this), params.amountIn);
        bool zeroForOne = params.tokenIn == DeterministicV3Pool(pool).token0();
        pendingSwap = PendingSwap(pool, params.tokenIn, params.amountIn, true);
        (int256 delta0, int256 delta1) =
            DeterministicV3Pool(pool).swap(params.recipient, zeroForOne, params.amountIn, abi.encode(pool));
        delete pendingSwap;

        int256 outputDelta = zeroForOne ? delta1 : delta0;
        require(outputDelta < 0, "INVALID_OUTPUT");
        amountOut = uint256(-outputDelta);
        require(amountOut >= params.amountOutMinimum, "TOO_LITTLE_RECEIVED");
        emit ExactInputExecuted(msg.sender, pool, params.amountIn, amountOut);
    }

    function uniswapV3SwapCallback(int256 amount0Delta, int256 amount1Delta, bytes calldata data)
        external
    {
        PendingSwap memory pending = pendingSwap;
        require(pending.active && msg.sender == pending.pool, "UNAUTHORIZED_CALLBACK");
        require(data.length == 32 && abi.decode(data, (address)) == msg.sender, "BAD_CALLBACK_DATA");
        int256 positive = amount0Delta > 0 ? amount0Delta : amount1Delta;
        require(positive > 0 && uint256(positive) == pending.amountIn, "BAD_CALLBACK_AMOUNT");
        pending.tokenIn.safeTransfer(msg.sender, uint256(positive));
    }
}
