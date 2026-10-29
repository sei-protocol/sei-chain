// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

address constant PARAMS_PRECOMPILE_ADDRESS = 0x0000000000000000000000000000000000001013;

IParams constant PARAMS_CONTRACT = IParams(PARAMS_PRECOMPILE_ADDRESS);

/// @notice Reads chain params. After the first block writes them, params change
/// only through a passed gov ParameterChange proposal
/// (IGov.submitParamChangeProposal); this interface has no setter.
interface IParams {
    /**
     * @notice Get the stored value of a param
     * @param subspace The param's subspace, e.g. "gov"; an unknown subspace reverts
     * @param key The param's flat key: the V2 key, or the V2 key, ".", and a field
     *        of it, e.g. "tallyparams.quorum"; an unknown key reverts
     * @return value The 32-byte abi.encode of the param's value. Each param's type
     *         is its field's in ParamsStorage.sol: Dec is a uint256 with 18
     *         decimal places, Duration a uint64 of nanoseconds, and Coins a
     *         uint256 of wei
     */
    function param(string calldata subspace, string calldata key) external view returns (bytes memory value);

    /// @notice Reverts: use param, which takes a flat key and returns the
    /// ABI-encoded value.
    function params(string calldata subspace, string calldata key) external view returns (string memory value);
}
