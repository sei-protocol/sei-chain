// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

address constant PARAMS_PRECOMPILE_ADDRESS = 0x0000000000000000000000000000000000001013;

IParams constant PARAMS_CONTRACT = IParams(PARAMS_PRECOMPILE_ADDRESS);

/// @notice The V2 type of a param's value, and how it is stored and encoded:
/// - Dec: uint256, fixed point with 18 decimal places (1e18 is 1).
/// - Duration: uint64 nanoseconds.
/// - Coins: uint256 wei, a whole number of usei; only the usei denom exists.
/// The other types have no stored form until a param needs one: DecCoins, Int,
/// Uint, Uint64, Uint32, Int64, Int32, Bool, String, Address and StringList.
enum V2Type {
    Dec,
    Duration,
    Coins,
    DecCoins,
    Int,
    Uint,
    Uint64,
    Uint32,
    Int64,
    Int32,
    Bool,
    String,
    Address,
    StringList
}

/// @notice Reads chain params. Params change only through a passed gov
/// ParameterChange proposal (IGov.submitParamChangeProposal); this interface has
/// no setter.
interface IParams {
    struct ParamKey {
        string subspace; // e.g. "gov"
        string key; // flat key, e.g. "tallyparams.quorum"
        string valueType; // Solidity type of the encoded value, e.g. "uint256"
        V2Type v2Type; // the V2 type, which gives the value's unit
    }

    /**
     * @notice Get the stored value of a param
     * @param subspace The param's subspace, e.g. "gov"; an unknown subspace reverts
     * @param key The param's flat key, e.g. "tallyparams.quorum"; an unknown key reverts
     * @return value The 32-byte abi.encode of the param's value, of the type and unit
     *         paramKeys reports
     */
    function param(string calldata subspace, string calldata key) external view returns (bytes memory value);

    /**
     * @notice List every param
     * @return keys Each param's subspace, flat key, value type and V2 type
     */
    function paramKeys() external view returns (ParamKey[] memory keys);

    /// @notice Reverts: use param, which takes a flat key and returns the
    /// ABI-encoded value.
    function params(string calldata subspace, string calldata key) external view returns (string memory value);
}
