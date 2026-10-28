// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

import "@openzeppelin/contracts/utils/structs/EnumerableSet.sol";

/// @dev Matches `GOV_PRECOMPILE_ADDRESS` in `giga/evmonly/precompiles/gov/Gov.sol`.
address constant GOV_PRECOMPILE_ADDRESS = 0x0000000000000000000000000000000000001006;

/// @notice Storage schema of the evmonly gov precompile (0x1006), held as that
/// address's contract storage.
/// @dev Deposits are held as the balance of a separate escrow address, not in
/// state variables. The precompile's ABI has no authz grant methods, so there is
/// no grant storage. `appliedPlanHeight` records the height each upgrade plan
/// was applied at, for an upgrade precompile to read.
contract GovStorage {
    using EnumerableSet for EnumerableSet.AddressSet;
    using EnumerableSet for EnumerableSet.UintSet;

    struct WeightedVote {
        int32 option;
        uint64 weight; // 1e18-scaled; a simple vote is a one-entry array at weight 1e18
    }

    struct Proposal {
        int32 status;
        bool isExpedited;
        uint8 contentType; // 0: TextContent, 1: SoftwareUpgradeContent, 2: CancelSoftwareUpgradeContent
        uint64 submitTime; // Unix nanoseconds, as are the other times
        uint64 depositEndTime;
        uint64 votingStartTime;
        uint64 votingEndTime;
        uint256 totalDeposit;
        uint256 tallyYes;
        uint256 tallyAbstain;
        uint256 tallyNo;
        uint256 tallyNoWithVeto;
        bytes content; // abi.encode of the contentType struct
        uint256[4] __gap;
    }

    /// @dev The content structs. Each holds the fields of the V2 Content its
    /// proposal type creates.
    struct TextContent {
        string title;
        string description;
        bool isExpedited;
    }

    struct UpgradePlan {
        string name;
        int64 height;
        string info;
    }

    struct SoftwareUpgradeContent {
        string title;
        string description;
        UpgradePlan plan;
    }

    struct CancelSoftwareUpgradeContent {
        string title;
        string description;
    }

    /// @dev A min-heap of `endTime << 64 | proposalId` keys, so its root is the
    /// proposal that ends first, lowest id first. EnumerableSet's layout:
    /// positions are 1-based indexes into keys.
    struct ProposalQueue {
        uint256[] keys;
        mapping(uint256 => uint256) positions;
    }

    /// @dev A tally in progress: 1e18-scaled voting power counted so far from
    /// voters' own delegations. Validators' own votes are added when it finishes.
    struct Tally {
        uint256 yes;
        uint256 abstain;
        uint256 no;
        uint256 noWithVeto;
        uint256 power;
        bool counting;
    }

    struct Plan {
        string name;
        int64 height;
        string info;
        uint64 proposalId;
    }

    struct Params {
        uint64 votingPeriod; // nanoseconds, as are the other periods
        uint64 expeditedVotingPeriod;
        uint256 minDeposit;
        uint64 maxDepositPeriod;
        uint256 minExpeditedDeposit;
        uint64 quorum;
        uint64 threshold;
        uint64 vetoThreshold;
        uint64 expeditedQuorum;
        uint64 expeditedThreshold;
    }

    uint64 internal lastProposalId; // highest id assigned; 0 before the first proposal
    uint64 internal startingProposalId; // first id to assign; 0 means 1
    Params internal params;
    Plan internal plan; // the single live upgrade plan

    mapping(uint64 => Proposal) internal proposals;
    mapping(uint64 => mapping(address => WeightedVote[])) internal votes; // proposalId => voter => options
    mapping(uint64 => mapping(address => uint256)) internal deposits; // proposalId => depositor => cumulative

    mapping(uint64 => EnumerableSet.AddressSet) internal depositors; // proposalId => distinct depositors, at most maxDepositors
    mapping(address => EnumerableSet.UintSet) internal proposalsVotedBy;
    mapping(address => EnumerableSet.UintSet) internal proposalsDepositedBy;

    ProposalQueue internal votingQueue; // proposals pending a voting-period end
    ProposalQueue internal depositQueue; // proposals pending a deposit-period end

    mapping(string => int64) internal appliedPlanHeight; // plan name => applied height

    mapping(uint64 => EnumerableSet.AddressSet) internal votersByProposal; // proposalId => distinct voters

    mapping(uint64 => Tally) internal tallies; // proposalId => tally in progress
    mapping(uint64 => mapping(address => uint256)) internal tallyDeductions; // proposalId => validator => shares whose delegators voted
}
