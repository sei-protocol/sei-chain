// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

address constant FEEGRANT_PRECOMPILE_ADDRESS = 0x0000000000000000000000000000000000001010;

IFeegrant constant FEEGRANT_CONTRACT = IFeegrant(FEEGRANT_PRECOMPILE_ADDRESS);

interface IFeegrant {
    // Transactions
    // Grants a fee allowance from the caller (granter) to the grantee. The
    // allowance is protobuf-JSON encoded (the same encoding the queries
    // return), e.g.
    // {"@type":"/cosmos.feegrant.v1beta1.BasicAllowance","spend_limit":[{"denom":"usei","amount":"1000"}],"expiration":null}
    function grantAllowance(address grantee, bytes memory allowance) external returns (bool success);

    // Revokes the fee allowance granted by the caller to the grantee.
    function revokeAllowance(address grantee) external returns (bool success);

    // Queries
    // Returns the fee allowance granted to the grantee by the granter. The
    // allowance field of the returned grant is JSON-encoded.
    function allowance(address granter, address grantee) external view returns (Grant memory grant);

    // Returns all the fee allowances granted to the given grantee, with pagination.
    function allowances(address grantee, bytes memory pageKey) external view returns (AllowancesResponse memory response);

    // Returns all the fee allowances issued by the given granter, with pagination.
    function allowancesByGranter(address granter, bytes memory pageKey) external view returns (AllowancesResponse memory response);

    // Structs
    struct Grant {
        string granter;
        string grantee;
        bytes allowance;
    }

    struct AllowancesResponse {
        Grant[] allowances;
        bytes nextKey;
    }
}
