// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

address constant PARAMS_PRECOMPILE_ADDRESS = 0x0000000000000000000000000000000000001013;

IParams constant PARAMS_CONTRACT = IParams(PARAMS_PRECOMPILE_ADDRESS);

interface IParams {
    // Queries

    /**
     * @notice Get the stored value of a module parameter
     * @param subspace The parameter subspace, e.g. "distribution"; an unknown subspace reverts
     * @param key The parameter key, e.g. "communitytax"; an unknown key returns an empty string
     * @return value The parameter's amino-JSON encoding, so string and decimal values keep their quotes
     */
    function params(string memory subspace, string memory key) external view returns (string memory value);
}
