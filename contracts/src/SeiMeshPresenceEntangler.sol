// SPDX-License-Identifier: MIT
pragma solidity ^0.8.19;

import {ISoulMoodOracle} from "./interfaces/ISoulMoodOracle.sol";

/// @title SeiMeshPresenceEntangler
/// @notice 1-of-1 presence attestation contract bound to a specific mesh SSID and mood oracle.
contract SeiMeshPresenceEntangler {
    /// @notice Thrown when a zero address is supplied where it is not allowed.
    error ZeroAddress();

    /// @notice Thrown when a caller without validator privileges attempts a restricted action.
    error Unauthorized();

    /// @notice Thrown when entanglement is requested while one is already active.
    error AlreadyEntangled();

    /// @notice Thrown when no entanglement is active but an action requires one.
    error NoActiveEntanglement();

    /// @notice Thrown when a proof was already generated and stored.
    error ProofAlreadyCommitted();

    /// @notice Address with permission to entangle and release souls.
    address public immutable validator;

    /// @notice Hash of the mesh SSID this contract is bound to.
    bytes32 public immutable meshSSIDHash;

    /// @notice External oracle providing the current mood for a given soul.
    ISoulMoodOracle public immutable moodOracle;

    /// @notice Details about the current entanglement session.
    struct Entanglement {
        address soul;
        string mood;
        uint256 nonce;
        uint256 timestamp;
        bytes32 proofId;
    }

    /// @notice Emitted when the validator entangles a soul presence.
    event Entangled(
        address indexed validator,
        address indexed soul,
        string mood,
        uint256 indexed nonce,
        bytes32 proofId
    );

    /// @notice Emitted when the validator releases the active entanglement.
    event Released(address indexed validator, address indexed soul, bytes32 proofId);

    /// @dev Tracks whether an entanglement is currently active.
    bool private _active;

    /// @dev Storage for the current entanglement details.
    Entanglement private _currentEntanglement;

    /// @dev Registry of previously observed proof identifiers for immutability.
    mapping(bytes32 => bool) private _proofRegistry;

    /// @param validator_ Address permitted to entangle and release.
    /// @param meshSSIDHash_ The SSID hash this contract is bound to.
    /// @param moodOracle_ External oracle exposing the soul mood state.
    constructor(address validator_, bytes32 meshSSIDHash_, ISoulMoodOracle moodOracle_) {
        if (validator_ == address(0) || address(moodOracle_) == address(0)) {
            revert ZeroAddress();
        }
        validator = validator_;
        meshSSIDHash = meshSSIDHash_;
        moodOracle = moodOracle_;
    }

    /// @notice Returns the current entanglement information and whether it is active.
    function currentEntanglement()
        external
        view
        returns (Entanglement memory entanglement, bool isActive)
    {
        return (_currentEntanglement, _active);
    }

    /// @notice Returns whether a given proof identifier has been previously committed.
    function isProofCommitted(bytes32 proofId) external view returns (bool) {
        return _proofRegistry[proofId];
    }

    /// @notice Indicates whether the contract currently maintains an entanglement.
    function isEntangled() external view returns (bool) {
        return _active;
    }

    /// @notice Validator-only operation to entangle a soul based on the oracle-provided mood.
    /// @param soul Address whose presence is being attested.
    /// @param nonce Validator supplied nonce ensuring uniqueness of the proof.
    /// @return proofId The immutable identifier representing this entanglement.
    function entangle(address soul, uint256 nonce) external returns (bytes32 proofId) {
        if (msg.sender != validator) revert Unauthorized();
        if (_active) revert AlreadyEntangled();
        if (soul == address(0)) revert ZeroAddress();

        string memory mood = moodOracle.moodOf(soul);
        proofId = keccak256(abi.encodePacked(soul, meshSSIDHash, mood, nonce));
        if (_proofRegistry[proofId]) revert ProofAlreadyCommitted();

        _proofRegistry[proofId] = true;
        _currentEntanglement = Entanglement({
            soul: soul,
            mood: mood,
            nonce: nonce,
            timestamp: block.timestamp,
            proofId: proofId
        });
        _active = true;

        emit Entangled(validator, soul, mood, nonce, proofId);
    }

    /// @notice Validator-only operation to release the current entanglement.
    function release() external {
        if (msg.sender != validator) revert Unauthorized();
        if (!_active) revert NoActiveEntanglement();

        address soul = _currentEntanglement.soul;
        bytes32 proofId = _currentEntanglement.proofId;

        delete _currentEntanglement;
        _active = false;

        emit Released(validator, soul, proofId);
    }

    /// @notice Offline-verifiable helper that recomputes the proof identifier and checks commitment.
    /// @param soul Address originally entangled.
    /// @param mood Mood string observed from the oracle at entanglement time.
    /// @param nonce Validator supplied nonce used for entanglement.
    /// @return True when the proof exists in the registry.
    function verifyProof(address soul, string calldata mood, uint256 nonce)
        external
        view
        returns (bool)
    {
        bytes32 proofId = keccak256(abi.encodePacked(soul, meshSSIDHash, mood, nonce));
        return _proofRegistry[proofId];
    }
}
