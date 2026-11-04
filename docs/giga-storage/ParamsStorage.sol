// SPDX-License-Identifier: MIT
pragma solidity 0.8.30;

/// @notice Storage schema of the evmonly params precompile (0x1013), held as that
/// address's contract storage. Each subspace is one struct at a fixed slot, and
/// each declared param is one field of it, commented with its flat key.
/// @dev Values by V2 type:
/// - Dec: uint256, fixed point with 18 decimal places (1e18 is 1).
/// - Duration: uint64 nanoseconds.
/// - Coins: uint256 wei, a whole number of usei; only the usei denom exists.
/// Other V2 types have no stored form until a param needs one.
///
/// Once a public network runs this, the layout only grows: a new param takes
/// slots from its struct's `__gap`, a new subspace is appended as a new state
/// variable, and a retired param keeps its slot.
contract ParamsStorage {
    /// @dev x/gov's depositparams, votingparams and tallyparams.
    struct Gov {
        uint64 maxDepositPeriod; // depositparams.max_deposit_period
        uint64 votingPeriod; // votingparams.voting_period
        uint64 expeditedVotingPeriod; // votingparams.expedited_voting_period
        uint256 minDeposit; // depositparams.min_deposit
        uint256 minExpeditedDeposit; // depositparams.min_expedited_deposit
        uint256 quorum; // tallyparams.quorum
        uint256 threshold; // tallyparams.threshold
        uint256 vetoThreshold; // tallyparams.veto_threshold
        uint256 expeditedQuorum; // tallyparams.expedited_quorum
        uint256 expeditedThreshold; // tallyparams.expedited_threshold
        uint256[8] __gap;
    }

    Gov internal gov; // subspace "gov"
}
