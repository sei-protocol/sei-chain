// SPDX-License-Identifier: MIT
pragma solidity >=0.8.10;

import "solmate/tokens/ERC20.sol";
import "solmate/utils/FixedPointMathLib.sol";
import "solmate/utils/ReentrancyGuard.sol";
import "@openzeppelin/proxy/utils/Initializable.sol";
import "./interfaces/IERC20.sol";
import "./libraries/Math.sol";
import "./libraries/UQ112x112.sol";

/// @title UniswapV2Pair
/// @author Uniswap Labs
/// @notice maintains a liquidity pool of a pair of tokens
contract UniswapV2Pair is ERC20, ReentrancyGuard, Initializable {
    // ========= Custom Errors =========

    error InsufficientLiquidityMinted();
    error InsufficientLiquidityBurned();
    error SafeTransferFailed();
    error SwapToSelf();
    error InsufficientLiquidity();
    error InvalidAmount();
    error InvalidConstantProductFormula();
    error BalanceOverflow();

    // ========= Libraries =========

    using Math for uint256;
    using FixedPointMathLib for uint256;

    // ========= Constants =========

    uint256 public constant MINIMUM_LIQUIDITY = 1e3;
    bytes4 public constant SELECTOR =
        bytes4(keccak256("transfer(address,uint256"));

    // ========= State Variables =========

    address public token0;
    address public token1;

    // reserves are tracked rather than balances to prevent price manipulation
    // bit-packing is done to save gas
    uint112 public reserve0;
    uint112 public reserve1;
    uint32 public blockTimestampLast;

    uint256 public price0CumulativeLast;
    uint256 public price1CumulativeLast;

    // ======== Events ========

    event Mint(address indexed _operator, uint256 _value);
    event Burn(address indexed _operator, uint256 _value);
    event Swap(
        address indexed sender,
        uint256 amount0Out,
        uint256 amount1Out,
        address indexed to
    );
    event Sync(uint112 reserve0, uint112 reserve1);

    // ========= Constructor =========

    constructor() ERC20("UniswapV2", "UNIV2", 18) {}

    // ========= Initializer =========

    function initialize(address _token0, address _token1) external initializer {
        token0 = _token0;
        token1 = _token1;
    }

    // ========= Public Functions =========

    /// @notice Returns reserves and last synced block timestamp
    /// @return reserve0 Reserve of token 0
    /// @return reserve1 Reserve of token 1
    /// @return blockTimestampLast Block timestamp of last sync
    function getReserves()
        public
        view
        returns (
            uint112,
            uint112,
            uint32
        )
    {
        return (reserve0, reserve1, blockTimestampLast);
    }

    /// @notice Calculate the pool tokens for the given new liquidity amount
    /// @dev If new pool is created, then minimum liquidity is 1e3 transfered to 0x0
    /// @param to Address to which pool tokens are minted
    /// @return liquidity Total liquidity minted
    function mint(address to) public nonReentrant returns (uint256 liquidity) {
        uint256 balance0 = IERC20(token0).balanceOf(address(this));
        uint256 balance1 = IERC20(token1).balanceOf(address(this));

        uint256 amount0 = balance0 - uint256(reserve0);
        uint256 amount1 = balance1 - uint256(reserve1);

        if (totalSupply == 0) {
            // Initial liquidity = sqrt(a0 * a1)
            liquidity =
                FixedPointMathLib.sqrt(amount0 * amount1) -
                MINIMUM_LIQUIDITY;

            // Prevents value of 1 LP token being too high
            _mint(address(0), MINIMUM_LIQUIDITY);
        } else {
            // Minimum because max is prone to price manipulation
            liquidity = Math.min(
                (amount0 * totalSupply) / reserve0,
                (amount1 * totalSupply) / reserve1
            );
        }

        if (liquidity == 0) revert InsufficientLiquidityMinted();

        _mint(to, liquidity);

        _update(balance0, balance1, reserve0, reserve1);

        emit Mint(to, liquidity);
    }

    /// @notice Burns pool tokens of a particular address
    /// @dev Needs to transfer pool tokens to the pool first to be burnt
    /// @param to Address whose tokens are burned
    /// @return amount0 Amount of token0 burned
    /// @return amount1 Amount of token1 burned
    function burn(address to)
        public
        nonReentrant
        returns (uint256 amount0, uint256 amount1)
    {
        uint256 balance0 = IERC20(token0).balanceOf(address(this));
        uint256 balance1 = IERC20(token1).balanceOf(address(this));

        uint256 liquidity = balanceOf[address(this)];

        amount0 = (liquidity * balance0) / uint256(reserve0);
        amount1 = (liquidity * balance1) / uint256(reserve1);

        if (amount0 == 0 || amount1 == 0) revert InsufficientLiquidityBurned();

        _burn(address(this), liquidity);

        _safeTransfer(token0, to, amount0);
        _safeTransfer(token1, to, amount1);

        _update(balance0 - amount0, balance1 - amount1, reserve0, reserve1);

        emit Burn(to, liquidity);
    }

    /// @notice Dwaps two tokens
    /// @dev New balances should maintain the constant product formula
    /// @param amount0Out Amount of token0 to be transfered
    /// @param amount1Out Amount of token1 to be transfered
    /// @param to Address o which tokens are transfered
    function swap(
        uint256 amount0Out,
        uint256 amount1Out,
        address to
    ) public nonReentrant {
        if (amount0Out == 0 && amount1Out == 0) revert InvalidAmount();

        if (amount0Out > reserve0 || amount1Out > reserve1)
            revert InsufficientLiquidity();

        if (to == token0 || to == token1) revert SwapToSelf();

        if (amount0Out != 0) _safeTransfer(token0, to, amount0Out);
        if (amount1Out != 0) _safeTransfer(token1, to, amount1Out);

        uint256 balance0 = IERC20(token0).balanceOf(address(this));
        uint256 balance1 = IERC20(token1).balanceOf(address(this));

        if (balance0 * balance1 < uint256(reserve0) * uint256(reserve1))
            revert InvalidConstantProductFormula();

        _update(balance0, balance1, reserve0, reserve1);

        emit Swap(msg.sender, amount0Out, amount1Out, to);
    }

    /// @notice Syncs reserves
    function sync() public {
        _update(
            IERC20(token0).balanceOf(address(this)),
            IERC20(token1).balanceOf(address(this)),
            reserve0,
            reserve1
        );
    }

    // ========= Internal functions =========

    /// @notice Updates pool reserves and price accumulators
    /// @param balance0 New balance of token0 in pool
    /// @param balance1 New balance of token1 in pool
    /// @param _reserve0 Reserve of token0 in pool
    /// @param _reserve1 Reserve of token1 in pool
    function _update(
        uint256 balance0,
        uint256 balance1,
        uint112 _reserve0,
        uint112 _reserve1
    ) internal {
        if (balance0 > type(uint112).max || balance1 > type(uint112).max)
            revert BalanceOverflow();

        unchecked {
            uint32 timeElapsed = uint32(block.timestamp) - blockTimestampLast;
            if (timeElapsed > 0 && _reserve0 > 0 && _reserve1 > 0) {
                price0CumulativeLast +=
                    uint256(
                        UQ112x112.uqdiv(UQ112x112.encode(_reserve1), reserve0)
                    ) *
                    timeElapsed;
                price1CumulativeLast +=
                    uint256(
                        UQ112x112.uqdiv(UQ112x112.encode(_reserve0), _reserve1)
                    ) *
                    timeElapsed;
            }
        }

        reserve0 = uint112(balance0);
        reserve1 = uint112(balance1);
        blockTimestampLast = uint32(block.timestamp);

        emit Sync(reserve0, reserve1);
    }

    function _safeTransfer(
        address token,
        address to,
        uint256 amount
    ) internal {
        bool success = IERC20(token).transfer(to, amount);
        if (!success) revert SafeTransferFailed();
    }
}
