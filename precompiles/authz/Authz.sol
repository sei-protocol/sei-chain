// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

address constant AUTHZ_PRECOMPILE_ADDRESS = 0x000000000000000000000000000000000000100e;

IAuthz constant AUTHZ_CONTRACT = IAuthz(AUTHZ_PRECOMPILE_ADDRESS);

interface IAuthz {
    // Transactions

    /**
     * @notice Grant an authorization from the caller (granter) to a grantee
     * @param grantee The grantee's EVM address (must be associated with a Sei address)
     * @param authorization The authorization as protobuf-JSON bytes, e.g.
     *        {"@type":"/cosmos.bank.v1beta1.SendAuthorization","spend_limit":[{"denom":"usei","amount":"100"}]}
     * @param expiration The expiration time of the grant as a Unix timestamp in seconds
     * @return success True if the grant was stored successfully
     */
    function grant(
        address grantee,
        bytes memory authorization,
        int64 expiration
    ) external returns (bool success);

    /**
     * @notice Execute messages with the caller as the authz grantee. Messages
     *         whose signer is not the caller require a matching grant. EVM
     *         messages are not allowed.
     * @param msgs The messages to execute, each as protobuf-JSON bytes, e.g.
     *        {"@type":"/cosmos.bank.v1beta1.MsgSend","from_address":"sei1...","to_address":"sei1...","amount":[...]}
     * @return responses The protobuf-encoded response of each message
     */
    function exec(
        bytes[] memory msgs
    ) external returns (bytes[] memory responses);

    /**
     * @notice Revoke the caller's grant to a grantee for the given message type URL
     * @param grantee The grantee's EVM address (must be associated with a Sei address)
     * @param msgTypeUrl The message type URL of the authorization to revoke, e.g. "/cosmos.bank.v1beta1.MsgSend"
     * @return success True if the grant was revoked successfully
     */
    function revoke(
        address grantee,
        string memory msgTypeUrl
    ) external returns (bool success);

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
