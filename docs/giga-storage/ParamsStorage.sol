// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

/// @dev Matches `PARAMS_PRECOMPILE_ADDRESS` in `precompiles/params/Params.sol`.
address constant PARAMS_PRECOMPILE_ADDRESS = 0x0000000000000000000000000000000000001013;

/// @notice Storage schema of the evmonly params precompile (0x1013), held as that
/// address's contract storage. Each subspace is one struct at a fixed slot, and
/// each declared param is one field of it.
/// @dev Value encodings, from the V2 amino types:
/// - Dec: uint256, fixed point with 18 decimal places (1e18 is 1).
/// - Duration: uint64 nanoseconds.
/// - Coins: uint256 usei; only the usei denom is accepted.
/// - Int: uint256.
/// - Other integers, bool, string and address: the same Solidity type.
///
/// The layout only grows. A new param takes slots from its struct's `__gap`, a
/// new subspace is appended as a new state variable, and a retired param keeps
/// its slot.
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
