// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

address constant GOV_PRECOMPILE_ADDRESS = 0x0000000000000000000000000000000000001006;

IGov constant GOV_CONTRACT = IGov(GOV_PRECOMPILE_ADDRESS);

/// @notice Content of a text proposal; `ProposalData.content` is its abi.encode
/// when `contentType` is 0.
struct TextContent {
    string title;
    string description;
    bool isExpedited;
}

/// @notice A change to one param. key is its flat key, e.g.
/// "tallyparams.quorum" (see IParams.paramKeys), and value is the 32-byte
/// abi.encode of its value, as IParams.param returns it.
struct ParamChange {
    string subspace;
    string key;
    bytes value;
}

/// @notice Content of a param change proposal; `ProposalData.content` is its
/// abi.encode when `contentType` is 3.
struct ParamChangeContent {
    string title;
    string description;
    ParamChange[] changes;
    bool isExpedited;
}

/// @notice The EVM-only gov precompile. Times are Unix nanoseconds, periods are
/// nanoseconds, amounts are wei, and ratios and vote weights are 1e18-scaled.
/// Statuses and vote options are numbered as x/gov's.
/// @dev Every proposal needs a non-empty title of at most 140 bytes and a
/// non-empty description of at most 10000, and its encoded content may not
/// exceed 16 KiB. Each submit method deposits msg.value under the same rules as
/// `deposit`.
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

    struct TallyResultData {
        uint256 yes;
        uint256 abstain;
        uint256 no;
        uint256 noWithVeto;
    }

    struct ProposalData {
        uint64 id;
        int32 status; // 1=DepositPeriod, 2=VotingPeriod, 3=Passed, 4=Rejected, 5=Failed
        TallyResultData finalTallyResult; // zero until voting ends; see tallyResult
        uint64 submitTime;
        uint64 depositEndTime;
        uint256 totalDeposit; // wei
        uint64 votingStartTime; // 0 before voting starts
        uint64 votingEndTime; // 0 before voting starts
        bool isExpedited;
        uint8 contentType; // 0: TextContent, 3: ParamChangeContent; 1 and 2 are reserved for software upgrades
        bytes content; // abi.encode of the contentType struct
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

    /// @dev V2's weighted vote option, with the weight as a decimal string.
    struct DecimalWeightedVoteOption {
        int32 option;
        string weight;
    }

    /// @notice A proposal was submitted. votingStarted is true when its deposit
    /// reached the minimum.
    event SubmitProposal(uint64 indexed proposalId, address indexed proposer, uint8 contentType, bool votingStarted);

    /// @notice amount wei was deposited on a proposal, including a submitter's
    /// initial deposit. votingStarted is true when this deposit opened voting.
    event ProposalDeposit(uint64 indexed proposalId, address indexed depositor, uint256 amount, bool votingStarted);

    /// @notice voter cast options on a proposal, replacing any earlier vote.
    event ProposalVote(uint64 indexed proposalId, address indexed voter, WeightedVoteOption[] options);

    /// @notice Votes option on a proposal in its voting period, replacing the
    /// caller's earlier vote.
    function vote(uint64 proposalID, int32 option) external returns (bool success);

    /// @notice Splits the caller's vote across up to four distinct options whose
    /// weights sum to 1e18, replacing the caller's earlier vote.
    function voteWeighted(uint64 proposalID, WeightedVoteOption[] calldata options) external returns (bool success);

    /// @notice Deposits msg.value on a proposal. The value must be a positive
    /// whole number of usei, in wei. A proposal takes at most 200 depositors, and
    /// a depositor's first deposit must be at least the proposal's minimum
    /// deposit (expedited or regular) divided by 200, rounded up to whole usei.
    function deposit(uint64 proposalID) external payable returns (bool success);

    /// @notice Submits a text proposal, depositing msg.value on it.
    function submitTextProposal(string calldata title, string calldata description, bool isExpedited)
        external
        payable
        returns (uint64 proposalID);

    /// @notice Submits a proposal to change chain params, depositing msg.value on
    /// it. Changes apply in order when it passes, all or none, each setting one
    /// param over the current params. Every change must name a declared param and
    /// leave its subspace valid when submitted.
    function submitParamChangeProposal(
        string calldata title,
        string calldata description,
        bool isExpedited,
        ParamChange[] calldata changes
    ) external payable returns (uint64 proposalID);

    /// @notice Reverts: software upgrade proposals are not supported yet.
    function submitSoftwareUpgradeProposal(
        string calldata title,
        string calldata description,
        bool isExpedited,
        UpgradePlan calldata plan
    ) external payable returns (uint64 proposalID);

    /// @notice Reverts: software upgrade proposals are not supported yet.
    function submitCancelSoftwareUpgradeProposal(string calldata title, string calldata description, bool isExpedited)
        external
        payable
        returns (uint64 proposalID);

    /// @notice Returns a proposal. Reverts when the proposal is unknown or was
    /// dropped.
    function proposal(uint64 proposalID) external view returns (ProposalData memory proposal);

    /// @notice Pages through proposals by id, filtered by status (0 for any),
    /// voter and depositor (the zero address for any). A page covers 100 ids, so
    /// it may hold fewer than 100 proposals, or none, and still return a nextKey;
    /// nextKey is empty on the last page.
    function proposals(int32 proposalStatus, address voter, address depositor, bytes calldata pageKey)
        external
        view
        returns (ProposalData[] memory proposals, bytes memory nextKey);

    /// @notice Returns a voter's vote. Reverts when the voter has no vote on the
    /// proposal, including once the tally has counted it.
    function getVote(uint64 proposalID, address voter) external view returns (VoteData memory vote);

    /// @notice Pages through a proposal's votes, 100 at a time.
    function votes(uint64 proposalID, bytes calldata pageKey)
        external
        view
        returns (VoteData[] memory votes, bytes memory nextKey);

    function params() external view returns (GovParams memory params);

    /// @notice Returns a depositor's deposit on a proposal. Reverts when they have
    /// none, including once the proposal settles and its deposits are cleared.
    function getDeposit(uint64 proposalID, address depositor) external view returns (DepositData memory deposit);

    /// @notice Pages through a proposal's deposits, 100 at a time.
    function deposits(uint64 proposalID, bytes calldata pageKey)
        external
        view
        returns (DepositData[] memory deposits, bytes memory nextKey);

    /// @notice A proposal's tally: the live count during voting, the final one after.
    function tallyResult(uint64 proposalID) external view returns (TallyResultData memory tallyResult);

    // V2 methods this precompile no longer serves. Each reverts, naming the call
    // to use instead where there is one.

    /// @notice Reverts: use submitTextProposal or submitParamChangeProposal.
    function submitProposal(string calldata proposalJSON) external payable returns (uint64 proposalID);

    /// @notice Reverts: use voteWeighted with 1e18-scaled weights.
    function voteWeighted(uint64 proposalID, DecimalWeightedVoteOption[] calldata options)
        external
        returns (bool success);

    /// @notice Reverts: gov authorizations are not supported.
    function grantVoteAuthorization(address grantee, int64 expiration) external returns (bool success);

    /// @notice Reverts: gov authorizations are not supported.
    function grantProposalAuthorization(address grantee, int64 expiration) external returns (bool success);

    /// @notice Reverts: gov authorizations are not supported.
    function revokeVoteAuthorization(address grantee) external returns (bool success);

    /// @notice Reverts: gov authorizations are not supported.
    function revokeProposalAuthorization(address grantee) external returns (bool success);

    /// @notice Reverts: gov authorizations are not supported.
    function voteWithAuthorization(address voter, uint64 proposalID, int32 option) external returns (bool success);

    /// @notice Reverts: gov authorizations are not supported.
    function submitProposalWithAuthorization(address proposer, string calldata proposalJSON)
        external
        payable
        returns (uint64 proposalID);
}
