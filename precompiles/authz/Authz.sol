// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

address constant AUTHZ_PRECOMPILE_ADDRESS = 0x000000000000000000000000000000000000100e;

IAuthz constant AUTHZ_CONTRACT = IAuthz(AUTHZ_PRECOMPILE_ADDRESS);

interface IAuthz {
    // Queries

    /**
     * @notice Get grants between a granter and a grantee, optionally filtered by message type URL
     * @param granter The granter's EVM address (must be associated with a Sei address)
     * @param grantee The grantee's EVM address (must be associated with a Sei address)
     * @param msgTypeUrl The message type URL to filter by (empty string for all)
     * @param pageKey Pagination key (empty bytes for the first page)
     * @return response Grants response with pagination
     */
    function grants(
        address granter,
        address grantee,
        string memory msgTypeUrl,
        bytes memory pageKey
    ) external view returns (GrantsResponse memory response);

    /**
     * @notice Get all grants granted by a granter
     * @param granter The granter's EVM address (must be associated with a Sei address)
     * @param pageKey Pagination key (empty bytes for the first page)
     * @return response Grant authorizations response with pagination
     */
    function granterGrants(
        address granter,
        bytes memory pageKey
    ) external view returns (GrantAuthorizationsResponse memory response);

    /**
     * @notice Get all grants granted to a grantee
     * @param grantee The grantee's EVM address (must be associated with a Sei address)
     * @param pageKey Pagination key (empty bytes for the first page)
     * @return response Grant authorizations response with pagination
     */
    function granteeGrants(
        address grantee,
        bytes memory pageKey
    ) external view returns (GrantAuthorizationsResponse memory response);

    // Structs

    struct Grant {
        bytes authorization;
        int64 expiration;
    }

    struct GrantsResponse {
        Grant[] grants;
        bytes nextKey;
    }

    struct GrantAuthorization {
        string granter;
        string grantee;
        bytes authorization;
        int64 expiration;
    }

    struct GrantAuthorizationsResponse {
        GrantAuthorization[] grants;
        bytes nextKey;
    }
}
