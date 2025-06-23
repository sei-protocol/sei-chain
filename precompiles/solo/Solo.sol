// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

address constant SOLO_PRECOMPILE_ADDRESS = 0x000000000000000000000000000000000000100C;

ISolo constant SOLO_CONTRACT = ISolo(
    SOLO_PRECOMPILE_ADDRESS
);

/**
 * @dev Interface for interacting with the solo precompile contract.
 */
interface ISolo {
    /**
     * @dev Claim assets using approver's signed Cosmos tx payload.
     * @param payload Signed Cosmos tx payload as bytes.
     * @return response true indicates a successful claim.
     */
    function claim(bytes memory payload) external returns (bool response);
    /**
     * @dev Claim assets from a specific contract using approver's signed Cosmos tx payload.
     * @param payload Signed Cosmos tx payload as bytes.
     * @return response true indicates a successful claim.
     */
    function claimSpecific(bytes memory payload) external returns (bool response);
}
