// SPDX-License-Identifier: MIT
pragma solidity ^0.8.19;

/// @title ISoulMoodOracle
/// @notice Minimal interface for retrieving the current mood associated with a soul address.
interface ISoulMoodOracle {
    /// @notice Returns the textual representation of the soul's current mood.
    /// @param soul The address whose mood should be queried.
    /// @return The mood string tracked by the oracle for the provided address.
    function moodOf(address soul) external view returns (string memory);
}
