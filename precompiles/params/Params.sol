// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

address constant PARAMS_PRECOMPILE_ADDRESS = 0x0000000000000000000000000000000000001013;

IParams constant PARAMS_CONTRACT = IParams(PARAMS_PRECOMPILE_ADDRESS);

interface IParams {
    // Queries
    function params(string memory subspace, string memory key) external view returns (string memory value);
}
