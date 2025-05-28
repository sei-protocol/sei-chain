// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

address constant GOV_PRECOMPILE_ADDRESS = 0x0000000000000000000000000000000000001006;

IGov constant GOV_CONTRACT = IGov(
    GOV_PRECOMPILE_ADDRESS
);

interface IGov {
    // Transactions
    function vote(
        uint64 proposalID,
        int32 option
    ) external returns (bool success);

    function deposit(
        uint64 proposalID
    ) payable external returns (bool success);
    
    /**
     * @dev Submit a new governance proposal
     * @param proposalJSON JSON string containing proposal details e.g.:
     *        {
     *          "title": "Proposal Title",
     *          "description": "Proposal Description",
     *          "type": "Text", // Optional, defaults to "Text" if empty
     *          "is_expedited": false, // Optional
     *          "deposit": "" // Optional, can also be provided via msg.value
     *        }
     * @return proposalID The ID of the created proposal
     */
    function submitProposal(
        string calldata proposalJSON
    ) external returns (uint64 proposalID);
}
