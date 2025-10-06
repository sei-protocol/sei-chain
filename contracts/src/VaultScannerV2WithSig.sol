// SPDX-License-Identifier: UNLICENSED
pragma solidity ^0.8.20;

/*
 * ────────────────────────────────────────────────────────────────────────────────
 * ░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░
 * ░░   SOLARAKIN SOVEREIGN STACK — COMPLETE CORE MODULES                           ░░
 * ░░   Author: Keeper (Pray4Love1)                                                 ░░
 * ░░   Origin Epoch: May 17, 2025 (SoulSync Protocol)                              ░░
 * ░░   Deployment Timestamp: Oct 5, 2025 (FlameProof Creation)                     ░░
 * ░░   Modules Included: Presence Validator, Vault With Pulse, Sigil NFT,          ░░
 * ░░                    KinKeyRotationController, SeiWord, HoloGuardian,           ░░
 * ░░                    VaultScannerV2WithSig, FlameProofAttribution, KinMultisig  ░░
 * ░░   Internal Soul Terms embedded. ABI externally rebranded.                     ░░
 * ░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░
 */

interface KinKeyPresenceValidator {
    function submitPresence(
        address user,
        string calldata mood,
        uint256 timestamp,
        bytes32 entropy,
        bytes calldata signature
    ) external;
}

/// ... (previous modules remain unchanged above)

/// @title VaultScannerV2WithSig — records presence-bound actions and token flows
contract VaultScannerV2WithSig {
    KinKeyPresenceValidator private validator;

    struct KinPulseEntry {
        address user;
        bytes32 moodHash;
        uint256 timestamp;
        string action;
    }

    KinPulseEntry[] public logs;

    event KinLog(address indexed user, string action, bytes32 moodHash, uint256 timestamp);

    constructor(address validatorAddress) {
        validator = KinKeyPresenceValidator(validatorAddress);
    }

    function logAction(
        string calldata mood,
        string calldata action,
        bytes32 entropy,
        bytes calldata sig
    ) external {
        validator.submitPresence(msg.sender, mood, block.timestamp, entropy, sig);
        bytes32 moodHash = keccak256(abi.encodePacked(mood, entropy));
        logs.push(KinPulseEntry(msg.sender, moodHash, block.timestamp, action));
        emit KinLog(msg.sender, action, moodHash, block.timestamp);
    }

    function getLog(uint256 index) public view returns (KinPulseEntry memory) {
        return logs[index];
    }

    function totalLogs() public view returns (uint256) {
        return logs.length;
    }
}

/// @title FlameProofAttribution — registry of authorship claims and module fingerprints
contract FlameProofAttribution {
    address public keeper;

    struct SoulClaim {
        string moduleName;
        bytes32 soulHash;
        string epoch;
        string extra;
    }

    mapping(address => SoulClaim[]) public registry;

    event SoulClaimed(address indexed author, string module, bytes32 hash);

    constructor() {
        keeper = msg.sender;
    }

    function declareOrigin(
        string calldata moduleName,
        bytes32 soulHash,
        string calldata epoch,
        string calldata extra
    ) external {
        SoulClaim memory claim = SoulClaim(moduleName, soulHash, epoch, extra);
        registry[msg.sender].push(claim);
        emit SoulClaimed(msg.sender, moduleName, soulHash);
    }

    function getClaims(address author) external view returns (SoulClaim[] memory) {
        return registry[author];
    }
}

/// @title KinMultisigValidator — requires moodproofs from multiple signers
contract KinMultisigValidator {
    KinKeyPresenceValidator private validator;

    struct Sig {
        string mood;
        uint256 timestamp;
        bytes32 entropy;
        bytes signature;
    }

    uint256 public constant TIME_WINDOW = 300; // 5 minutes

    mapping(bytes32 => bool) public executed;

    event VerifiedMultisig(bytes32 indexed opHash);

    constructor(address validatorAddress) {
        validator = KinKeyPresenceValidator(validatorAddress);
    }

    function verifyMultisig(
        bytes32 operationHash,
        address[] calldata signers,
        Sig[] calldata sigs
    ) external {
        require(signers.length == sigs.length, "Mismatched arrays");

        for (uint256 i = 0; i < signers.length; i++) {
            validator.submitPresence(
                signers[i],
                sigs[i].mood,
                sigs[i].timestamp,
                sigs[i].entropy,
                sigs[i].signature
            );
            require(block.timestamp - sigs[i].timestamp <= TIME_WINDOW, "Signature expired");
        }

        executed[operationHash] = true;
        emit VerifiedMultisig(operationHash);
    }

    function isExecuted(bytes32 opHash) public view returns (bool) {
        return executed[opHash];
    }
}
