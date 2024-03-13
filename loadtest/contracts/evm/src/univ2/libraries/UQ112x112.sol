// SPDX-License-Identifier: MIT
pragma solidity >=0.8.10;

/// @title UQ112x112
/// @author [Uniswap Labs](https://github.com/Uniswap/v2-core/blob/master/contracts/libraries/UQ112x112.sol)
/// @notice Library for handling binary fixed point numbers
library UQ112x112 {
    uint224 public constant Q112 = 2**112;

    // encode a uint112 as a UQ112x112
    function encode(uint112 y) internal pure returns (uint224 z) {
        z = uint224(y) * Q112; // never overflows
    }

    // divide a UQ112x112 by a uint112, returning a UQ112x112
    function uqdiv(uint224 x, uint112 y) internal pure returns (uint224 z) {
        z = x / uint224(y);
    }
}
