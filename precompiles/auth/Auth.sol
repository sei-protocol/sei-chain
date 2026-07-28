// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

address constant AUTH_PRECOMPILE_ADDRESS = 0x000000000000000000000000000000000000100D;

IAuth constant AUTH_CONTRACT = IAuth(AUTH_PRECOMPILE_ADDRESS);

interface IAuth {
    // Queries

    /**
     * @notice Get account information for the given address
     * @param addr The EVM address (must be associated with a Sei address)
     * @return account Account details
     */
    function account(
        address addr
    ) external view returns (Account memory account);

    /**
     * @notice Get all accounts, paginated
     * @param pageKey Pagination key (empty bytes for the first page)
     * @return response Accounts response with pagination
     */
    function accounts(
        bytes memory pageKey
    ) external view returns (AccountsResponse memory response);

    /**
     * @notice Get the auth module parameters
     * @return params Auth module parameters
     */
    function params() external view returns (AuthParams memory params);

    /**
     * @notice Get the next account number
     * @return count The next account number
     */
    function nextAccountNumber() external view returns (uint64 count);

    // Structs

    struct Account {
        string accountAddress;
        uint64 accountNumber;
        uint64 sequence;
    }

    struct AccountsResponse {
        Account[] accounts;
        bytes nextKey;
    }

    struct AuthParams {
        uint64 maxMemoCharacters;
        uint64 txSigLimit;
        uint64 txSizeCostPerByte;
        uint64 sigVerifyCostEd25519;
        uint64 sigVerifyCostSecp256k1;
        bool disableSeqnoCheck;
    }
}
