// SPDX-License-Identifier: UNLICENSED
pragma solidity ^0.8.20;

/*
 * ────────────────────────────────────────────────────────────────────────────────
 * ░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░
 * ░░   SOLARAKIN SOVEREIGN STACK — CORE MODULES (EXTENDED)                         ░░
 * ░░   Author: Keeper (Pray4Love1)                                                   ░░
 * ░░   Origin Epoch: May 17, 2025 (SoulSync Protocol)                                ░░
 * ░░   Deployment Timestamp: Oct 5, 2025 (FlameProof Creation)                       ░░
 * ░░   Modules Included: Presence Validator, Vault With Pulse, Sigil NFT,            ░░
 * ░░                    KinKeyRotationController, SeiWord, HoloGuardian             ░░
 * ░░   Internal Soul Terms embedded. ABI externally rebranded.                       ░░
 * ░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░
 */

/// @title KinKeyPresenceValidator
contract KinKeyPresenceValidator {
    address public sovereign;
    mapping(address => bytes32) public latestMoodHash;
    mapping(address => bytes32) private latestEntropyByUser;

    event AuthorshipClaim(string indexed sigil, string creator, string timestamp);

    constructor() {
        sovereign = msg.sender;
        emit AuthorshipClaim("SoulSigil#1", "Keeper", "2025-10-05");
    }

    function submitPresence(
        address user,
        string calldata mood,
        uint256 timestamp,
        bytes32 entropy,
        bytes calldata signature
    ) external {
        require(block.timestamp - timestamp < 10, "Stale presence");
        bytes32 structHash = keccak256(
            abi.encode(
                keccak256("Presence(address user,string mood,uint256 timestamp,bytes32 entropy)"),
                user,
                keccak256(bytes(mood)),
                timestamp,
                entropy
            )
        );
        bytes32 digest = keccak256(abi.encodePacked("\x19\x01", structHash));
        address recovered = recoverSigner(digest, signature);
        require(recovered == user, "Invalid presence signature");
        latestEntropyByUser[user] = entropy;
        latestMoodHash[user] = keccak256(abi.encodePacked(mood, entropy));
    }

    function isPresent(address user, string memory mood) public view returns (bool) {
        bytes32 entropy = latestEntropy(user);
        if (entropy == bytes32(0) && latestMoodHash[user] == bytes32(0)) {
            return false;
        }
        return latestMoodHash[user] == keccak256(abi.encodePacked(mood, entropy));
    }

    function latestEntropy(address user) internal view returns (bytes32) {
        return latestEntropyByUser[user];
    }

    function recoverSigner(bytes32 digest, bytes memory sig) internal pure returns (address) {
        (bytes32 r, bytes32 s, uint8 v) = splitSig(sig);
        return ecrecover(digest, v, r, s);
    }

    function splitSig(bytes memory sig) internal pure returns (bytes32 r, bytes32 s, uint8 v) {
        require(sig.length == 65, "Invalid signature length");
        assembly {
            r := mload(add(sig, 32))
            s := mload(add(sig, 64))
            v := byte(0, mload(add(sig, 96)))
        }
    }
}

/// @title VaultWithPulse — Locks/unlocks based on moodproofs
contract VaultWithPulse {
    KinKeyPresenceValidator private validator;
    mapping(address => bool) public pulseLockState;
    mapping(address => uint256) public lastKinPresence;
    mapping(address => uint256) public pulseInterval;

    event PulseUnlocked(address indexed by);
    event PulseLocked(address indexed by);

    constructor(address validatorAddress) {
        validator = KinKeyPresenceValidator(validatorAddress);
    }

    function setPulseInterval(uint256 secondsInterval) external {
        pulseInterval[msg.sender] = secondsInterval;
    }

    function unlockWithPresence(
        string calldata mood,
        uint256 timestamp,
        bytes32 entropy,
        bytes calldata signature
    ) external {
        validator.submitPresence(msg.sender, mood, timestamp, entropy, signature);
        lastKinPresence[msg.sender] = block.timestamp;
        pulseLockState[msg.sender] = false;
        emit PulseUnlocked(msg.sender);
    }

    function isUnlocked(address user) public view returns (bool) {
        return (!pulseLockState[user] && (block.timestamp - lastKinPresence[user] < pulseInterval[user]));
    }

    function forceLock() external {
        pulseLockState[msg.sender] = true;
        emit PulseLocked(msg.sender);
    }
}

/// @title SoulSigilNFT — A reactive, soulbound mood NFT
contract SoulSigilNFT {
    KinKeyPresenceValidator private validator;
    mapping(uint256 => string) public SigilDNA;
    mapping(uint256 => bytes32) public SigilMood;
    address public sovereign;
    uint256 public nextTokenId;

    constructor(address validatorAddress) {
        validator = KinKeyPresenceValidator(validatorAddress);
        sovereign = msg.sender;
    }

    function mintSigil(string calldata initialMood, bytes32 entropy, bytes calldata sig) external {
        validator.submitPresence(msg.sender, initialMood, block.timestamp, entropy, sig);
        uint256 tokenId = nextTokenId++;
        SigilMood[tokenId] = keccak256(abi.encodePacked(initialMood, entropy));
        SigilDNA[tokenId] = string.concat("SIGIL:", initialMood);
    }

    function updateSigilMood(uint256 tokenId, string calldata newMood, bytes32 entropy, bytes calldata sig) external {
        validator.submitPresence(msg.sender, newMood, block.timestamp, entropy, sig);
        SigilMood[tokenId] = keccak256(abi.encodePacked(newMood, entropy));
        SigilDNA[tokenId] = string.concat("SIGIL:", newMood);
    }

    function getSigilState(uint256 tokenId) public view returns (string memory dna, bytes32 moodHash) {
        return (SigilDNA[tokenId], SigilMood[tokenId]);
    }
}

/// @title KinKeyRotationController — rotating ephemeral keys validated by mood entropy
contract KinKeyRotationController {
    KinKeyPresenceValidator private validator;
    mapping(address => bytes32) public kinkeyEpoch;
    mapping(address => uint256) public rotationTTL;
    mapping(address => bytes32) private rotationEntropy;

    constructor(address validatorAddress) {
        validator = KinKeyPresenceValidator(validatorAddress);
    }

    function rotateKey(string calldata mood, bytes32 entropy, bytes calldata sig, uint256 ttl) external {
        validator.submitPresence(msg.sender, mood, block.timestamp, entropy, sig);
        kinkeyEpoch[msg.sender] = keccak256(abi.encodePacked(mood, entropy));
        rotationEntropy[msg.sender] = entropy;
        rotationTTL[msg.sender] = block.timestamp + ttl;
    }

    function verifyRotation(address user, string calldata mood) public view returns (bool) {
        if (block.timestamp >= rotationTTL[user]) {
            return false;
        }
        return kinkeyEpoch[user] == keccak256(abi.encodePacked(mood, rotationEntropy[user]));
    }
}

/// @title SeiWord — mood validated payments using upgraded 402 Payment Required standard
contract SeiWord {
    KinKeyPresenceValidator private validator;
    event MoodTransfer(address indexed from, address indexed to, uint256 amount, bytes32 moodHash);
    mapping(address => uint256) public balances;

    constructor(address validatorAddress) {
        validator = KinKeyPresenceValidator(validatorAddress);
    }

    function deposit() external payable {
        balances[msg.sender] += msg.value;
    }

    function transferWithPresence(
        address to,
        uint256 amount,
        string calldata mood,
        bytes32 entropy,
        bytes calldata sig
    ) external {
        validator.submitPresence(msg.sender, mood, block.timestamp, entropy, sig);
        require(balances[msg.sender] >= amount, "Insufficient");
        balances[msg.sender] -= amount;
        balances[to] += amount;
        emit MoodTransfer(msg.sender, to, amount, keccak256(abi.encodePacked(mood, entropy)));
    }
}

/// @title HoloGuardian — watches for presence decay, locks contracts, alerts modules
contract HoloGuardian {
    KinKeyPresenceValidator private validator;
    mapping(address => uint256) public lastPresence;
    mapping(address => bool) public anomalyDetected;

    event Anomaly(address indexed user, uint256 epoch);

    constructor(address validatorAddress) {
        validator = KinKeyPresenceValidator(validatorAddress);
    }

    function reportPresence(string calldata mood, bytes32 entropy, bytes calldata sig) external {
        validator.submitPresence(msg.sender, mood, block.timestamp, entropy, sig);
        lastPresence[msg.sender] = block.timestamp;
        anomalyDetected[msg.sender] = false;
    }

    function checkDecay(address user, uint256 maxSilence) public {
        if (block.timestamp - lastPresence[user] > maxSilence) {
            anomalyDetected[user] = true;
            emit Anomaly(user, block.timestamp);
        }
    }

    function isHealthy(address user) public view returns (bool) {
        return !anomalyDetected[user];
    }
}
