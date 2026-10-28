// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

import "@openzeppelin/contracts/utils/structs/EnumerableSet.sol";
import "giga-storage/GovStorage.sol";

/// @notice Writes known values into GovStorage with solc's own codegen, so the
/// Go reader can be checked against the compiler's slot derivation.
contract GovStorageHarness is GovStorage {
    using EnumerableSet for EnumerableSet.AddressSet;
    using EnumerableSet for EnumerableSet.UintSet;

    function seed(address voter, address other, bytes calldata content) external {
        lastProposalId = 7;
        startingProposalId = 3;
        params = Params({
            votingPeriod: 11,
            expeditedVotingPeriod: 12,
            minDeposit: 13e18,
            maxDepositPeriod: 14,
            minExpeditedDeposit: 15e18,
            quorum: 0.334e18,
            threshold: 0.5e18,
            vetoThreshold: 0.334e18,
            expeditedQuorum: 0.667e18,
            expeditedThreshold: 0.667e18
        });

        Proposal storage p = proposals[5];
        p.status = 2;
        p.isExpedited = true;
        p.contentType = 2;
        p.submitTime = 1_700_000_001;
        p.depositEndTime = 1_700_000_002;
        p.votingStartTime = 1_700_000_003;
        p.votingEndTime = 1_700_000_004;
        p.totalDeposit = 21e18;
        p.tallyYes = 22;
        p.tallyAbstain = 23;
        p.tallyNo = 24;
        p.tallyNoWithVeto = 25;
        p.content = content;

        votes[5][voter].push(WeightedVote({option: 1, weight: 0.7e18}));
        votes[5][voter].push(WeightedVote({option: 4, weight: 0.3e18}));
        deposits[5][voter] = 31e18;
        deposits[5][other] = 32e18;

        depositors[5].add(voter);
        depositors[5].add(other);
        depositors[5].remove(voter);
        votersByProposal[5].add(voter);
        push(votingQueue, (uint256(1_700_000_004) << 64) | 5);
        push(votingQueue, (uint256(1_700_000_004) << 64) | 6);
        push(depositQueue, (uint256(1_700_000_002) << 64) | 3);

        Tally storage tally = tallies[5];
        tally.yes = 41;
        tally.abstain = 42;
        tally.no = 43;
        tally.noWithVeto = 44;
        tally.power = 45;
        tally.counting = true;
        tallyDeductions[5][other] = 46;
    }

    /// @dev The content struct encodings, with solc's own codegen.
    function textContent(string calldata title, string calldata description, bool isExpedited)
        external pure returns (bytes memory)
    {
        return abi.encode(TextContent(title, description, isExpedited));
    }

    /// @dev Appends key, which must not break the heap order.
    function push(ProposalQueue storage q, uint256 key) internal {
        q.keys.push(key);
        q.positions[key] = q.keys.length;
    }
}
