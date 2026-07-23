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
    struct Coin {
        uint256 amount;
        string denom;
    }

    struct TallyResultData {
        string yes;
        string abstain;
        string no;
        string noWithVeto;
    }

    struct WeightedVoteOptionData {
        int32 option;
        string weight; // Weight as decimal string (e.g., "0.7")
    }

    struct ProposalData {
        uint64 id;
        int32 status; // ProposalStatus enum value
        TallyResultData finalTallyResult;
        int64 submitTime; // Unix seconds
        int64 depositEndTime; // Unix seconds
        Coin[] totalDeposit;
        int64 votingStartTime; // Unix seconds
        int64 votingEndTime; // Unix seconds
        bool isExpedited;
        bytes content; // proposal content as JSON
    }

    struct VoteData {
        uint64 proposalId;
        string voter; // bech32 address
        WeightedVoteOptionData[] options;
    }

    struct DepositData {
        uint64 proposalId;
        string depositor; // bech32 address
        Coin[] amount;
    }

    struct GovParams {
        uint64 votingPeriod; // seconds
        uint64 expeditedVotingPeriod; // seconds
        Coin[] minDeposit;
        uint64 maxDepositPeriod; // seconds
        Coin[] minExpeditedDeposit;
        string quorum;
        string threshold;
        string vetoThreshold;
        string expeditedQuorum;
        string expeditedThreshold;
    }

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

    /**
     * @dev Query a proposal by ID
     * @param proposalID The ID of the proposal
     * @return proposal The proposal details
     */
    function proposal(
        uint64 proposalID
    ) external view returns (ProposalData memory proposal);

    /**
     * @dev Query proposals with optional filters
     * @param proposalStatus Filter by proposal status (0 = all)
     * @param voter Filter by voter address (zero address = no filter)
     * @param depositor Filter by depositor address (zero address = no filter)
     * @param pageKey Pagination key (empty for first page)
     * @return proposals The matching proposals
     * @return nextKey Pagination key for the next page
     */
    function proposals(
        int32 proposalStatus,
        address voter,
        address depositor,
        bytes memory pageKey
    ) external view returns (ProposalData[] memory proposals, bytes memory nextKey);

    /**
     * @dev Query a vote cast on a proposal
     * @param proposalID The ID of the proposal
     * @param voter The voter address
     * @return vote The vote details
     */
    function getVote(
        uint64 proposalID,
        address voter
    ) external view returns (VoteData memory vote);

    /**
     * @dev Query all votes cast on a proposal
     * @param proposalID The ID of the proposal
     * @param pageKey Pagination key (empty for first page)
     * @return votes The votes cast on the proposal
     * @return nextKey Pagination key for the next page
     */
    function votes(
        uint64 proposalID,
        bytes memory pageKey
    ) external view returns (VoteData[] memory votes, bytes memory nextKey);

    /**
     * @dev Query the governance module parameters
     * @return params The voting, deposit and tally parameters
     */
    function params() external view returns (GovParams memory params);

    /**
     * @dev Query a deposit made to a proposal
     * @param proposalID The ID of the proposal
     * @param depositor The depositor address
     * @return deposit The deposit details
     */
    function getDeposit(
        uint64 proposalID,
        address depositor
    ) external view returns (DepositData memory deposit);

    /**
     * @dev Query all deposits made to a proposal
     * @param proposalID The ID of the proposal
     * @param pageKey Pagination key (empty for first page)
     * @return deposits The deposits made to the proposal
     * @return nextKey Pagination key for the next page
     */
    function deposits(
        uint64 proposalID,
        bytes memory pageKey
    ) external view returns (DepositData[] memory deposits, bytes memory nextKey);

    /**
     * @dev Query the current tally of votes on a proposal
     * @param proposalID The ID of the proposal
     * @return tallyResult The tally result
     */
    function tallyResult(
        uint64 proposalID
    ) external view returns (TallyResultData memory tallyResult);
}
