// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

address constant PARAMS_PRECOMPILE_ADDRESS = 0x0000000000000000000000000000000000001013;

IParams constant PARAMS_CONTRACT = IParams(PARAMS_PRECOMPILE_ADDRESS);

interface IParams {
    struct ParamKey {
        string subspace; // e.g. "gov"
        string key; // flat key, e.g. "tallyparams.quorum"
        string valueType; // Solidity type of the value, e.g. "uint256"
    }

    // Queries

    /**
     * @notice Get the stored value of a param
     * @param subspace The param's subspace, e.g. "gov"; an unknown subspace reverts
     * @param key The param's flat key, e.g. "tallyparams.quorum"; an unknown key reverts
     * @return value abi.encode of the param's value, of the type paramKeys reports: durations
     *         in nanoseconds, amounts in usei, and ratios scaled by 1e18
     */
    function param(string memory subspace, string memory key) external view returns (bytes memory value);

    /**
     * @notice List every param
     * @return keys Each param's subspace, flat key and value type
     */
    function paramKeys() external view returns (ParamKey[] memory keys);
}
