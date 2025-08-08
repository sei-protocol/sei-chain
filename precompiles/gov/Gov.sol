// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

address constant GOV_PRECOMPILE_ADDRESS = 0x0000000000000000000000000000000000001006;

IGov constant GOV_CONTRACT = IGov(
    GOV_PRECOMPILE_ADDRESS
);

struct WeightedVoteOption {
    int32 option;   // Vote option (1=Yes, 2=Abstain, 3=No, 4=NoWithVeto)
    string weight;  // Weight as decimal string (e.g., "0.7")
}

interface IGov {
    /**
     * @dev Cast a simple vote on a governance proposal
     * @param proposalID The ID of the proposal to vote on
     * @param option Vote option: 1=Yes, 2=Abstain, 3=No, 4=NoWithVeto
     * @return success Whether the vote was successfully cast
     */
    function vote(
        uint64 proposalID,
        int32 option
    ) external returns (bool success);

    /**
     * @dev Cast a weighted vote on a governance proposal (vote splitting)
     * @param proposalID The ID of the proposal to vote on
     * @param options Array of weighted vote options, weights must sum to 1.0
     * @return success Whether the vote was successfully cast
     * 
     * Example:
     * WeightedVoteOption[] memory options = new WeightedVoteOption[](2);
     * options[0] = WeightedVoteOption({option: 1, weight: "0.7"}); // 70% Yes
     * options[1] = WeightedVoteOption({option: 2, weight: "0.3"}); // 30% Abstain
     * GOV_CONTRACT.voteWeighted(proposalID, options);
     */
    function voteWeighted(
        uint64 proposalID,
        WeightedVoteOption[] calldata options
    ) external returns (bool success);

    /**
     * @dev Deposit tokens to a governance proposal
     * @param proposalID The ID of the proposal to deposit to
     * @return success Whether the deposit was successful
     * Note: Send usei tokens via msg.value
     */
    function deposit(
        uint64 proposalID
    ) payable external returns (bool success);
    
    /**
     * @dev Submit a new governance proposal. Deposit should be provided via msg.value
     * @param proposalJSON JSON string containing proposal details e.g.:
     *        {
     *          "title": "Proposal Title",
     *          "description": "Proposal Description",
     *          "type": "Text", // Optional, defaults to "Text" if empty
     *          "is_expedited": false // Optional
     *        }
     * @return proposalID The ID of the created proposal
     */
    function submitProposal(
        string calldata proposalJSON
    ) payable external returns (uint64 proposalID);
}
