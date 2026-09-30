// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

// The EVM-only chain's governance precompile. abi.json is compiled from this
// file with `make giga-gov-abi`. Every function keeps the selector, mutability
// and return types it has in precompiles/gov/Gov.sol.

address constant GOV_PRECOMPILE_ADDRESS = 0x0000000000000000000000000000000000001006;

IGov constant GOV_CONTRACT = IGov(GOV_PRECOMPILE_ADDRESS);

interface IGov {
    // Always empty: the precompile takes no deposits.
    struct Coin {
        uint256 amount;
        string denom;
    }

    // Voting weight behind each option, as decimal integers.
    struct TallyResultData {
        string yes;
        string abstain;
        string no;
        string noWithVeto;
    }

    struct WeightedVoteOptionData {
        int32 option; // 1=Yes, 2=Abstain, 3=No, 4=NoWithVeto
        string weight; // Always "1.000000000000000000"
    }

    struct ProposalData {
        uint64 id;
        int32 status; // 2=VotingPeriod, 3=Passed, 4=Rejected, 5=Failed
        TallyResultData finalTallyResult;
        int64 submitTime; // Unix seconds
        int64 depositEndTime; // Unix seconds, equal to submitTime
        Coin[] totalDeposit;
        int64 votingStartTime; // Unix seconds, equal to submitTime
        int64 votingEndTime; // Unix seconds
        bool isExpedited; // Always false
        bytes content; // The x/upgrade proposal as JSON
    }

    struct VoteData {
        uint64 proposalId;
        string voter; // 0x-prefixed EVM address
        WeightedVoteOptionData[] options;
    }

    struct GovParams {
        uint64 votingPeriod; // seconds
        uint64 expeditedVotingPeriod; // seconds, equal to votingPeriod
        Coin[] minDeposit;
        uint64 maxDepositPeriod; // Always 0
        Coin[] minExpeditedDeposit;
        string quorum;
        string threshold;
        string vetoThreshold;
        string expeditedQuorum; // Equal to quorum
        string expeditedThreshold; // Equal to threshold
    }

    /**
     * @dev Vote on a proposal in its voting period. Only a voter may vote, and
     *      a later vote replaces an earlier one.
     * @param proposalID The ID of the proposal to vote on
     * @param option Vote option: 1=Yes, 2=Abstain, 3=No, 4=NoWithVeto
     * @return success Always true; a refused vote reverts
     */
    function vote(
        uint64 proposalID,
        int32 option
    ) external returns (bool success);

    /**
     * @dev Submit a SoftwareUpgrade or CancelSoftwareUpgrade proposal. Only a
     *      voter may submit. The function is payable for parity with
     *      precompiles/gov, but a call carrying value reverts.
     * @param proposalJSON The proposal, for example:
     *        {
     *          "title": "Upgrade",
     *          "description": "Move to the next build",
     *          "type": "SoftwareUpgrade",
     *          "plan": {"name": "<40-hex commit>", "height": 1000, "info": ""}
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
     * @dev Query a voter's vote on a proposal
     * @param proposalID The ID of the proposal
     * @param voter The voter address
     * @return vote The vote details
     */
    function getVote(
        uint64 proposalID,
        address voter
    ) external view returns (VoteData memory vote);

    /**
     * @dev Query the governance parameters
     * @return params The voting and tally parameters
     */
    function params() external view returns (GovParams memory params);

    /**
     * @dev Query a proposal's tally: the live tally while it is in its voting
     *      period, and the final tally afterwards
     * @param proposalID The ID of the proposal
     * @return tallyResult The tally result
     */
    function tallyResult(
        uint64 proposalID
    ) external view returns (TallyResultData memory tallyResult);
}
