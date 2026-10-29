// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

address constant PARAMS_PRECOMPILE_ADDRESS = 0x0000000000000000000000000000000000001013;

IParams constant PARAMS_CONTRACT = IParams(PARAMS_PRECOMPILE_ADDRESS);

/// @notice Reads chain params. Every method fails until the chain's first block
/// writes them; after that they change only through a passed gov
/// ParameterChange proposal (IGov.submitParamChangeProposal), and this
/// interface has no setter.
interface IParams {
    /**
     * @notice Get the stored value of a param
     * @param subspace The param's subspace, e.g. "gov"; an unknown subspace fails the call
     * @param key The param's flat key: the V2 key, or the V2 key, ".", and a field
     *        of it, e.g. "tallyparams.quorum", as the comment on its field in
     *        docs/giga-storage/ParamsStorage.sol gives it; an unknown key fails the call
     * @return value The 32-byte abi.encode of the param's value. Each param's type
     *         is its field's in docs/giga-storage/ParamsStorage.sol: Dec is a uint256 with 18
     *         decimal places, Duration a uint64 of nanoseconds, and Coins a
     *         uint256 of wei
     */
    function param(string calldata subspace, string calldata key) external view returns (bytes memory value);

    /// @notice Reverts: use param, which takes a flat key and returns the
    /// ABI-encoded value.
    function params(string calldata subspace, string calldata key) external view returns (string memory value);
}
