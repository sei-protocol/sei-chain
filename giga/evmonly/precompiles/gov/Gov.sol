// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

address constant GOV_PRECOMPILE_ADDRESS = 0x0000000000000000000000000000000000001006;

IGov constant GOV_CONTRACT = IGov(GOV_PRECOMPILE_ADDRESS);

/// @notice The EVM-only gov precompile. Times are Unix nanoseconds, periods are
/// nanoseconds, amounts are wei, and ratios and vote weights are 1e18-scaled.
/// Statuses and vote options are numbered as x/gov's.
interface IGov {
    struct WeightedVoteOption {
        int32 option; // 1=Yes, 2=Abstain, 3=No, 4=NoWithVeto
        uint256 weight; // 1e18-scaled
    }

    struct UpgradePlan {
        string name;
        int64 height;
        string info;
    }

    struct ParamChange {
        string subspace; // e.g. "gov"
        string key; // flat key, e.g. "tallyparams.quorum"; see IParams.paramKeys
        bytes value; // abi.encode of the param's value; see IParams.param
    }

    struct TallyResultData {
        uint256 yes;
        uint256 abstain;
        uint256 no;
        uint256 noWithVeto;
    }

    struct ProposalData {
        uint64 id;
        int32 status; // 1=DepositPeriod, 2=VotingPeriod, 3=Passed, 4=Rejected, 5=Failed
        TallyResultData finalTallyResult;
        uint64 submitTime;
        uint64 depositEndTime;
        uint256 totalDeposit; // wei
        uint64 votingStartTime; // 0 before voting starts
        uint64 votingEndTime; // 0 before voting starts
        bool isExpedited;
        uint8 contentType; // 0: TextContent, 1: SoftwareUpgradeContent, 2: CancelSoftwareUpgradeContent, 3: ParamChangeContent
        bytes content; // abi.encode of the contentType struct in GovStorage.sol
    }

    struct VoteData {
        uint64 proposalId;
        address voter;
        WeightedVoteOption[] options;
    }

    struct DepositData {
        uint64 proposalId;
        address depositor;
        uint256 amount; // wei
    }

    struct GovParams {
        uint64 votingPeriod;
        uint64 expeditedVotingPeriod;
        uint256 minDeposit; // wei
        uint64 maxDepositPeriod;
        uint256 minExpeditedDeposit; // wei
        uint256 quorum;
        uint256 threshold;
        uint256 vetoThreshold;
        uint256 expeditedQuorum;
        uint256 expeditedThreshold;
    }

    /// @notice Votes option on a proposal in its voting period, replacing the
    /// caller's earlier vote.
    function vote(uint64 proposalID, int32 option) external returns (bool success);

    /// @notice Splits the caller's vote across up to four distinct options whose
    /// weights sum to 1e18.
    function voteWeighted(uint64 proposalID, WeightedVoteOption[] calldata options) external returns (bool success);

    /// @notice Deposits msg.value, a whole number of usei in wei, on a proposal.
    /// A proposal takes at most 200 depositors, and a depositor's first deposit
    /// must be at least the minimum deposit over 200.
    function deposit(uint64 proposalID) external payable returns (bool success);

    /// @notice Submits a text proposal, depositing msg.value on it. For every
    /// proposal type, the title is at most 140 bytes, the description at most
    /// 10000, and the encoded content at most 16 KiB.
    function submitTextProposal(string calldata title, string calldata description, bool isExpedited)
        external
        payable
        returns (uint64 proposalID);

    /// @notice Submits a proposal to schedule plan, depositing msg.value on it.
    /// The plan needs a name and a positive height.
    function submitSoftwareUpgradeProposal(
        string calldata title,
        string calldata description,
        bool isExpedited,
        UpgradePlan calldata plan
    )
        external
        payable
        returns (uint64 proposalID);

    /// @notice Submits a proposal to cancel the scheduled upgrade, depositing
    /// msg.value on it.
    function submitCancelSoftwareUpgradeProposal(string calldata title, string calldata description, bool isExpedited)
        external
        payable
        returns (uint64 proposalID);

    /// @notice Submits a proposal to change chain params, depositing msg.value on
    /// it. Changes apply in order when it passes, all or none, each setting one
    /// field over the current params. Every change must name a declared param and
    /// leave its subspace valid when submitted.
    function submitParamChangeProposal(
        string calldata title,
        string calldata description,
        bool isExpedited,
        ParamChange[] calldata changes
    ) external payable returns (uint64 proposalID);

        function proposal(uint64 proposalID) external view returns (ProposalData memory proposal);

    /// @notice Pages through proposals, filtered by status (0 for any), voter and
    /// depositor (the zero address for any). A page may hold fewer than 100
    /// proposals, or none, and still return a nextKey; nextKey is empty on the
    /// last page.
    function proposals(int32 proposalStatus, address voter, address depositor, bytes calldata pageKey)
        external
        view
        returns (ProposalData[] memory proposals, bytes memory nextKey);

    function getVote(uint64 proposalID, address voter) external view returns (VoteData memory vote);

    /// @notice Pages through a proposal's votes, 100 at a time.
    function votes(uint64 proposalID, bytes calldata pageKey)
        external
        view
        returns (VoteData[] memory votes, bytes memory nextKey);

    function params() external view returns (GovParams memory params);

    function getDeposit(uint64 proposalID, address depositor) external view returns (DepositData memory deposit);

    /// @notice Pages through a proposal's deposits, 100 at a time.
    function deposits(uint64 proposalID, bytes calldata pageKey)
        external
        view
        returns (DepositData[] memory deposits, bytes memory nextKey);

    /// @notice A proposal's tally: the live count during voting, the final one after.
    function tallyResult(uint64 proposalID) external view returns (TallyResultData memory tallyResult);
}
