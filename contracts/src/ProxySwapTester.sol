// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

/**
 * Reproduces the exact pattern from the failing mainnet tx 0xf0ca0ec2...
 *
 * Key patterns being tested:
 * 1. Proxy token with delegatecall (like Sei's USDC proxy 0xe15fC38F → 0xcaFdC392)
 * 2. V3-style callback swaps (pool calls back into router mid-swap)
 * 3. Balance verification via staticcall after callback mutates state
 * 4. Multiple cross-contract transfers touching the same token storage
 */

// ─── Proxy Token ───────────────────────────────────────────────────────────────

/**
 * @title TokenImplementation
 * @dev ERC20 implementation that lives behind a proxy. All storage is on the proxy.
 *      Uses raw storage slots matching OpenZeppelin layout so delegatecall works.
 */
contract TokenImplementation {
    // Storage layout: slot 0 = mapping(address => uint256) balances
    //                 slot 1 = mapping(address => mapping(address => uint256)) allowances
    //                 slot 2 = uint256 totalSupply

    event Transfer(address indexed from, address indexed to, uint256 value);
    event Approval(address indexed owner, address indexed spender, uint256 value);

    function _balanceSlot(address a) internal pure returns (bytes32) {
        return keccak256(abi.encode(a, uint256(0)));
    }

    function _allowanceSlot(address owner_, address spender_) internal pure returns (bytes32) {
        return keccak256(abi.encode(spender_, keccak256(abi.encode(owner_, uint256(1)))));
    }

    function balanceOf(address a) external view returns (uint256 bal) {
        bytes32 slot = _balanceSlot(a);
        assembly { bal := sload(slot) }
    }

    function totalSupply() external view returns (uint256 ts) {
        assembly { ts := sload(2) }
    }

    function transfer(address to, uint256 amount) external returns (bool) {
        return _transfer(msg.sender, to, amount);
    }

    function transferFrom(address from, address to, uint256 amount) external returns (bool) {
        bytes32 slot = _allowanceSlot(from, msg.sender);
        uint256 currentAllowance;
        assembly { currentAllowance := sload(slot) }
        if (currentAllowance != type(uint256).max) {
            require(currentAllowance >= amount, "insufficient allowance");
            assembly { sstore(slot, sub(currentAllowance, amount)) }
        }
        return _transfer(from, to, amount);
    }

    function approve(address spender, uint256 amount) external returns (bool) {
        bytes32 slot = _allowanceSlot(msg.sender, spender);
        assembly { sstore(slot, amount) }
        emit Approval(msg.sender, spender, amount);
        return true;
    }

    function mint(address to, uint256 amount) external {
        bytes32 balSlot = _balanceSlot(to);
        uint256 bal;
        assembly { bal := sload(balSlot) }
        assembly { sstore(balSlot, add(bal, amount)) }
        uint256 ts;
        assembly { ts := sload(2) }
        assembly { sstore(2, add(ts, amount)) }
        emit Transfer(address(0), to, amount);
    }

    function _transfer(address from, address to, uint256 amount) internal returns (bool) {
        bytes32 fromSlot = _balanceSlot(from);
        bytes32 toSlot = _balanceSlot(to);
        uint256 fromBal;
        uint256 toBal;
        assembly { fromBal := sload(fromSlot) }
        require(fromBal >= amount, "insufficient balance");
        assembly {
            sstore(fromSlot, sub(fromBal, amount))
            toBal := sload(toSlot)
            sstore(toSlot, add(toBal, amount))
        }
        emit Transfer(from, to, amount);
        return true;
    }
}

/**
 * @title ProxyToken
 * @dev Minimal proxy that delegates all calls to TokenImplementation.
 *      Storage lives here, code lives in the implementation.
 *      This reproduces the Sei USDC proxy pattern from the failing tx.
 */
contract ProxyToken {
    address public immutable implementation;

    constructor(address impl) {
        implementation = impl;
    }

    fallback() external payable {
        address impl = implementation;
        assembly {
            calldatacopy(0, 0, calldatasize())
            let result := delegatecall(gas(), impl, 0, calldatasize(), 0, 0)
            returndatacopy(0, 0, returndatasize())
            switch result
            case 0 { revert(0, returndatasize()) }
            default { return(0, returndatasize()) }
        }
    }
}

// ─── V3-Style Pool with Callback ───────────────────────────────────────────────

interface ISwapCallback {
    function swapCallback(int256 amount0Delta, int256 amount1Delta, bytes calldata data) external;
}

/**
 * @title CallbackPool
 * @dev V3-style AMM pool that calls back into the caller to receive input tokens.
 *      Reproduces the pattern: pool sends output first, then callbacks to get input,
 *      then verifies balance. This creates nested CALL frames that mutate state.
 */
contract CallbackPool {
    address public token0;
    address public token1;

    event Swap(address indexed sender, int256 amount0, int256 amount1);

    constructor(address _token0, address _token1) {
        token0 = _token0;
        token1 = _token1;
    }

    /**
     * @dev V3-style swap with callback.
     *      Matches the EXACT operation order from the real mainnet V3 pool trace:
     *        1. Transfer output token to recipient (state mutation FIRST)
     *        2. STATICCALL balanceOf(inputToken) — pre-callback balance check
     *        3. Callback to msg.sender to deliver input tokens
     *        4. STATICCALL balanceOf(inputToken) — post-callback verification
     *
     *      This order is critical: there's a dirty write (output transfer) sitting
     *      in the state BEFORE the first STATICCALL read. The giga KV store's
     *      snapshot/cache behavior must handle this correctly.
     */
    function swap(
        address recipient,
        bool zeroForOne,
        uint256 amountIn,
        bytes calldata data
    ) external returns (int256 amount0, int256 amount1) {
        // Calculate output (simplified: 99% of input for a ~1:1 pool)
        uint256 amountOut = amountIn * 99 / 100;

        address tokenIn = zeroForOne ? token0 : token1;
        address tokenOut = zeroForOne ? token1 : token0;

        // Step 1: Send output tokens to recipient FIRST — state mutation before any reads
        // (Real V3 pools transfer output before checking input balance)
        (bool ok1,) = tokenOut.call(
            abi.encodeWithSignature("transfer(address,uint256)", recipient, amountOut)
        );
        require(ok1, "output transfer failed");

        // Step 2: Check balance of input token AFTER the output transfer
        // This read happens with dirty state from step 1 already in the snapshot
        (bool ok0, bytes memory bal0) = tokenIn.staticcall(
            abi.encodeWithSignature("balanceOf(address)", address(this))
        );
        require(ok0, "balanceOf failed");
        uint256 balanceBefore = abi.decode(bal0, (uint256));

        // Step 3: Callback to the caller to deliver input tokens to this pool
        if (zeroForOne) {
            amount0 = int256(amountIn);
            amount1 = -int256(amountOut);
        } else {
            amount0 = -int256(amountOut);
            amount1 = int256(amountIn);
        }
        ISwapCallback(msg.sender).swapCallback(amount0, amount1, data);

        // Step 4: Verify we received the input tokens (balance check via staticcall)
        // This read must see the callback's state mutations
        (bool ok2, bytes memory bal1) = tokenIn.staticcall(
            abi.encodeWithSignature("balanceOf(address)", address(this))
        );
        require(ok2, "balanceOf after failed");
        uint256 balanceAfter = abi.decode(bal1, (uint256));
        require(balanceAfter >= balanceBefore + amountIn, "insufficient input received");

        emit Swap(msg.sender, amount0, amount1);
    }
}

// ─── V2-Style Pool (no callback) ──────────────────────────────────────────────

/**
 * @title SimpleV2Pool
 * @dev V2-style pool: tokens sent first, then swap() called.
 *      Reproduces the Dragonswap V2 pair from the failing tx.
 */
contract SimpleV2Pool {
    address public token0;
    address public token1;
    uint112 public reserve0;
    uint112 public reserve1;

    event Swap(address indexed sender, uint256 amount0In, uint256 amount1In,
               uint256 amount0Out, uint256 amount1Out, address indexed to);
    event Sync(uint112 reserve0, uint112 reserve1);

    constructor(address _token0, address _token1) {
        token0 = _token0;
        token1 = _token1;
    }

    function addLiquidity() external {
        (bool ok0, bytes memory b0) = token0.staticcall(
            abi.encodeWithSignature("balanceOf(address)", address(this))
        );
        (bool ok1, bytes memory b1) = token1.staticcall(
            abi.encodeWithSignature("balanceOf(address)", address(this))
        );
        require(ok0 && ok1, "balanceOf failed");
        reserve0 = uint112(abi.decode(b0, (uint256)));
        reserve1 = uint112(abi.decode(b1, (uint256)));
        emit Sync(reserve0, reserve1);
    }

    function getReserves() external view returns (uint112, uint112, uint32) {
        return (reserve0, reserve1, uint32(block.timestamp));
    }

    function swap(uint256 amount0Out, uint256 amount1Out, address to, bytes calldata) external {
        require(amount0Out > 0 || amount1Out > 0, "insufficient output");

        if (amount0Out > 0) {
            (bool ok,) = token0.call(
                abi.encodeWithSignature("transfer(address,uint256)", to, amount0Out)
            );
            require(ok, "transfer0 failed");
        }
        if (amount1Out > 0) {
            (bool ok,) = token1.call(
                abi.encodeWithSignature("transfer(address,uint256)", to, amount1Out)
            );
            require(ok, "transfer1 failed");
        }

        _updateReserves();
    }

    function _updateReserves() internal {
        (bool ok0, bytes memory b0) = token0.staticcall(
            abi.encodeWithSignature("balanceOf(address)", address(this))
        );
        (bool ok1, bytes memory b1) = token1.staticcall(
            abi.encodeWithSignature("balanceOf(address)", address(this))
        );
        require(ok0 && ok1, "balanceOf failed");
        uint256 balance0 = abi.decode(b0, (uint256));
        uint256 balance1 = abi.decode(b1, (uint256));

        reserve0 = uint112(balance0);
        reserve1 = uint112(balance1);

        emit Sync(reserve0, reserve1);
    }
}

// ─── Multi-Hop Router with Callback Support ────────────────────────────────────

/**
 * @title CallbackRouter
 * @dev Router that chains swaps through V3 callback pools and V2 pools.
 *      Reproduces the exact swap pattern from the failing tx:
 *        1. transferFrom(user → router) for input token
 *        2. V3 pool swap with callback (pool sends output, callbacks for input)
 *        3. Another V3 pool swap with callback
 *        4. V2 pool swap (router sends tokens first, then calls swap)
 *        5. Transfer final output to user
 */
contract CallbackRouter is ISwapCallback {
    // Transient state for callback
    struct SwapState {
        address tokenIn;
        address pool;
        uint256 amountIn;
    }

    SwapState private _pendingSwap;

    /**
     * @dev Called by the V3 pool during swap to deliver input tokens.
     */
    function swapCallback(int256 amount0Delta, int256 amount1Delta, bytes calldata) external override {
        // Determine which amount is positive (that's what we owe the pool)
        uint256 amountOwed;
        if (amount0Delta > 0) {
            amountOwed = uint256(amount0Delta);
        } else {
            amountOwed = uint256(amount1Delta);
        }

        // Transfer the owed tokens to the pool
        (bool ok,) = _pendingSwap.tokenIn.call(
            abi.encodeWithSignature("transfer(address,uint256)", msg.sender, amountOwed)
        );
        require(ok, "callback transfer failed");
    }

    /**
     * @dev Execute the full multi-hop swap mimicking the failing mainnet tx.
     *
     * Path: tokenA --(V3 callback pool 1)--> proxyToken --(V3 callback pool 2)--> tokenB
     *       tokenB --(V2 pool)--> tokenA (back to original token for arb profit)
     */
    function executeMultiHopSwap(
        uint256 amountIn,
        address tokenA,
        address proxyToken,
        address tokenB,
        address v3Pool1,
        address v3Pool2,
        address v2Pool,
        address recipient
    ) external returns (uint256 finalAmount) {
        // Pull tokenA from sender
        _safeCall(tokenA, abi.encodeWithSignature(
            "transferFrom(address,address,uint256)", msg.sender, address(this), amountIn
        ));

        // Hop 1: V3 callback swap (tokenA → proxyToken)
        uint256 hop1Out = _v3Swap(v3Pool1, tokenA, amountIn);

        // Hop 2: V3 callback swap (proxyToken → tokenB)
        uint256 hop2Out = _v3Swap(v3Pool2, proxyToken, hop1Out);

        // Hop 3: V2 swap (tokenB → tokenA)
        uint256 hop3Out = _v2Swap(v2Pool, tokenB, tokenA, hop2Out);

        // Transfer final output to recipient
        _safeCall(tokenA, abi.encodeWithSignature(
            "transfer(address,uint256)", recipient, hop3Out
        ));

        finalAmount = hop3Out;
    }

    function _safeCall(address target, bytes memory data) internal {
        (bool ok,) = target.call(data);
        require(ok, "call failed");
    }

    function _v3Swap(address pool, address tokenIn, uint256 amountIn) internal returns (uint256 amountOut) {
        _pendingSwap = SwapState({ tokenIn: tokenIn, pool: pool, amountIn: amountIn });
        (bool ok, bytes memory ret) = pool.call(
            abi.encodeWithSignature(
                "swap(address,bool,uint256,bytes)",
                address(this), true, amountIn, bytes("")
            )
        );
        require(ok, "v3 swap failed");
        (, int256 amount1) = abi.decode(ret, (int256, int256));
        amountOut = amount1 < 0 ? uint256(-amount1) : uint256(amount1);
    }

    function _v2Swap(address pool, address tokenIn, address tokenOut, uint256 amountIn) internal returns (uint256 amountOut) {
        // Send input tokens to pool
        _safeCall(tokenIn, abi.encodeWithSignature("transfer(address,uint256)", pool, amountIn));

        // Get reserves and token0
        (bool ok1, bytes memory resData) = pool.staticcall(abi.encodeWithSignature("getReserves()"));
        require(ok1, "getReserves failed");
        (uint112 r0, uint112 r1,) = abi.decode(resData, (uint112, uint112, uint32));

        (bool ok2, bytes memory t0Data) = pool.staticcall(abi.encodeWithSignature("token0()"));
        require(ok2, "token0 failed");
        address poolToken0 = abi.decode(t0Data, (address));

        // Calculate output amount
        uint256 amount0Out;
        uint256 amount1Out;
        if (poolToken0 == tokenIn) {
            uint256 fee = amountIn * 997;
            amountOut = (fee * uint256(r1)) / (uint256(r0) * 1000 + fee);
            amount1Out = amountOut;
        } else {
            uint256 fee = amountIn * 997;
            amountOut = (fee * uint256(r0)) / (uint256(r1) * 1000 + fee);
            amount0Out = amountOut;
        }

        _safeCall(pool, abi.encodeWithSignature(
            "swap(uint256,uint256,address,bytes)", amount0Out, amount1Out, address(this), bytes("")
        ));
    }
}
