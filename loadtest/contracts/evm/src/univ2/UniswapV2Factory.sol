// SPDX-License-Identifier: MIT
pragma solidity >=0.8.10;

import "./UniswapV2Pair.sol";
import "./interfaces/IUniswapV2Pair.sol";

/// @title UniswapV2Factory
/// @author Uniswap Labs
/// @notice Creates pool pairs of tokens
contract UniswapV2Factory {
    // ========= Custom Errors =========

    error IdenticalTokens();
    error InvalidToken();
    error DuplicatePair();

    // ========= State Variables =========

    mapping(address => mapping(address => address)) public pairs;
    address[] public allPairs;

    // ========= Events =========

    event PairCreated(
        address indexed token0,
        address indexed token1,
        address pair,
        uint256
    );

    // ========= Public Helper Functions =========

    function getAllPairLength() external view returns (uint256) {
        return allPairs.length;
    }

    function getAllPairsIndex(uint256 index) external view returns (address) {
        return allPairs[index];
    }

    // ========= Public Functions =========

    /// @notice Creates a new pool of token pair
    /// @param tokenA First token in the pair
    /// @param tokenB Second token in the pair
    /// @return pair Address of the pair created
    function createPair(address tokenA, address tokenB)
        public
        returns (address pair)
    {
        if (tokenA == tokenB) revert IdenticalTokens();

        (address token0, address token1) = tokenA < tokenB
            ? (tokenA, tokenB)
            : (tokenB, tokenA);

        if (token0 == address(0)) revert InvalidToken();
        if (pairs[token0][token1] != address(0)) revert DuplicatePair();

        bytes memory bytecode = type(UniswapV2Pair).creationCode;
        bytes32 salt = keccak256(abi.encodePacked(token0, token1));

        // solhint-disable-next-line no-inline-assembly
        assembly {
            pair := create2(0, add(bytecode, 32), mload(bytecode), salt)
        }

        IUniswapV2Pair(pair).initialize(token0, token1);

        pairs[token0][token1] = pair;
        pairs[token1][token0] = pair;
        allPairs.push(pair);

        emit PairCreated(token0, token1, pair, allPairs.length);
    }
}
