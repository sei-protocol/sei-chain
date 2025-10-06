// SPDX-License-Identifier: UNLICENSED
pragma solidity ^0.8.20;

/*
 * ────────────────────────────────────────────────────────────────────────────────
 * ░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░
 * ░░   SOLARAKIN SOVEREIGN STACK — CORE MODULES                                     ░░
 * ░░   Author: Keeper (Pray4Love1)                                                   ░░
 * ░░   Origin Epoch: May 17, 2025 (SoulSync Protocol)                                ░░
 * ░░   Deployment Timestamp: Oct 5, 2025 (FlameProof Creation)                       ░░
 * ░░   Modules Included: Presence Validator, Vault With Pulse, Sigil NFT             ░░
 * ░░   Internal Soul Terms embedded. ABI externally rebranded.                       ░░
 * ░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░
 */

/// @title KinKeyPresenceValidator
/// @notice Verifies presence proofs signed by participants using an EIP-712 digest.
contract KinKeyPresenceValidator {
    address public immutable sovereign;

    bytes32 public immutable DOMAIN_SEPARATOR;
    bytes32 public constant PRESENCE_TYPEHASH =
        keccak256("Presence(address user,string mood,uint256 timestamp,bytes32 entropy)");
    bytes32 public constant EIP712_DOMAIN_TYPEHASH =
        keccak256("EIP712Domain(string name,uint256 chainId,address verifyingContract)");

    mapping(address => bytes32) public latestMoodHash;
    mapping(address => bytes32) private _latestEntropySeed;
    mapping(address => bool) private _hasPresence;

    event AuthorshipClaim(string indexed sigil, string creator, string timestamp);
    event PresenceSubmitted(address indexed user, bytes32 moodHash, bytes32 entropy);

    constructor() {
        sovereign = msg.sender;
        DOMAIN_SEPARATOR = keccak256(
            abi.encode(
                EIP712_DOMAIN_TYPEHASH,
                keccak256(bytes("KinKeyPresenceValidator")),
                block.chainid,
                address(this)
            )
        );
        emit AuthorshipClaim("SoulSigil#1", "Keeper", "2025-10-05");
    }

    function submitPresence(
        address user,
        string calldata mood,
        uint256 timestamp,
        bytes32 entropy,
        bytes calldata signature
    ) external {
        require(user != address(0), "invalid user");
        require(timestamp <= block.timestamp, "future presence");
        require(block.timestamp - timestamp <= 10, "stale presence");

        bytes32 structHash = keccak256(
            abi.encode(
                PRESENCE_TYPEHASH,
                user,
                keccak256(bytes(mood)),
                timestamp,
                entropy
            )
        );

        bytes32 digest = keccak256(abi.encodePacked("\x19\x01", DOMAIN_SEPARATOR, structHash));
        address recovered = recoverSigner(digest, signature);
        require(recovered == user, "invalid signature");

        bytes32 moodHash = keccak256(abi.encodePacked(mood, entropy));
        latestMoodHash[user] = moodHash;
        _latestEntropySeed[user] = entropy;
        _hasPresence[user] = true;

        emit PresenceSubmitted(user, moodHash, entropy);
    }

    function isPresent(address user, string memory mood) public view returns (bool) {
        if (!_hasPresence[user]) {
            return false;
        }
        bytes32 entropy = _latestEntropySeed[user];
        return latestMoodHash[user] == keccak256(abi.encodePacked(mood, entropy));
    }

    function latestEntropy(address user) public view returns (bytes32) {
        return _latestEntropySeed[user];
    }

    function hasPresence(address user) external view returns (bool) {
        return _hasPresence[user];
    }

    function recoverSigner(bytes32 digest, bytes memory sig) internal pure returns (address) {
        (bytes32 r, bytes32 s, uint8 v) = splitSig(sig);
        if (v < 27) {
            v += 27;
        }
        require(v == 27 || v == 28, "invalid v");
        return ecrecover(digest, v, r, s);
    }

    function splitSig(bytes memory sig)
        internal
        pure
        returns (bytes32 r, bytes32 s, uint8 v)
    {
        require(sig.length == 65, "invalid signature length");
        assembly {
            r := mload(add(sig, 32))
            s := mload(add(sig, 64))
            v := byte(0, mload(add(sig, 96)))
        }
    }
}

/// @title VaultWithPulse — Locks/unlocks based on mood proofs.
contract VaultWithPulse {
    KinKeyPresenceValidator private immutable validator;
    mapping(address => bool) public pulseLockState;
    mapping(address => uint256) public lastKinPresence;
    mapping(address => uint256) public pulseInterval;

    event PulseUnlocked(address indexed by);
    event PulseLocked(address indexed by);

    constructor(address validatorAddress) {
        require(validatorAddress != address(0), "validator required");
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

    function isUnlocked(address user) external view returns (bool) {
        uint256 interval = pulseInterval[user];
        if (pulseLockState[user] || interval == 0) {
            return false;
        }
        return block.timestamp - lastKinPresence[user] < interval;
    }

    function forceLock() external {
        pulseLockState[msg.sender] = true;
        emit PulseLocked(msg.sender);
    }
}

/// @title SoulSigilNFTV2 — A reactive, soulbound mood NFT backed by presence proofs.
contract SoulSigilNFTV2 {
    KinKeyPresenceValidator private immutable validator;
    mapping(uint256 => string) private _sigilDNA;
    mapping(uint256 => bytes32) private _sigilMood;
    mapping(uint256 => address) private _owners;
    mapping(address => uint256) private _balances;
    mapping(address => bool) private _hasSigil;

    address public immutable sovereign;
    uint256 public nextTokenId;

    event SigilMinted(address indexed owner, uint256 indexed tokenId, string mood, bytes32 entropy);
    event SigilMoodUpdated(address indexed owner, uint256 indexed tokenId, string mood, bytes32 entropy);

    constructor(address validatorAddress) {
        require(validatorAddress != address(0), "validator required");
        validator = KinKeyPresenceValidator(validatorAddress);
        sovereign = msg.sender;
    }

    function totalSupply() external view returns (uint256) {
        return nextTokenId;
    }

    function balanceOf(address owner) external view returns (uint256) {
        require(owner != address(0), "zero address");
        return _balances[owner];
    }

    function ownerOf(uint256 tokenId) public view returns (address) {
        address owner = _owners[tokenId];
        require(owner != address(0), "unknown token");
        return owner;
    }

    function hasSigil(address owner) external view returns (bool) {
        return _hasSigil[owner];
    }

    function sigilMoodHash(uint256 tokenId) external view returns (bytes32) {
        return _sigilMood[tokenId];
    }

    function sigilDNA(uint256 tokenId) external view returns (string memory) {
        return _sigilDNA[tokenId];
    }

    function mintSigil(
        string calldata initialMood,
        bytes32 entropy,
        bytes calldata signature
    ) external {
        require(!_hasSigil[msg.sender], "sigil exists");
        validator.submitPresence(msg.sender, initialMood, block.timestamp, entropy, signature);

        uint256 tokenId = nextTokenId++;
        _owners[tokenId] = msg.sender;
        _balances[msg.sender] += 1;
        _hasSigil[msg.sender] = true;

        bytes32 moodHash = keccak256(abi.encodePacked(initialMood, entropy));
        _sigilMood[tokenId] = moodHash;
        _sigilDNA[tokenId] = string.concat("SIGIL:", initialMood);

        emit SigilMinted(msg.sender, tokenId, initialMood, entropy);
    }

    function updateSigilMood(
        uint256 tokenId,
        string calldata newMood,
        bytes32 entropy,
        bytes calldata signature
    ) external {
        address owner = ownerOf(tokenId);
        require(owner == msg.sender, "not owner");

        validator.submitPresence(msg.sender, newMood, block.timestamp, entropy, signature);
        bytes32 moodHash = keccak256(abi.encodePacked(newMood, entropy));
        _sigilMood[tokenId] = moodHash;
        _sigilDNA[tokenId] = string.concat("SIGIL:", newMood);

        emit SigilMoodUpdated(msg.sender, tokenId, newMood, entropy);
    }

    function getSigilState(uint256 tokenId) external view returns (string memory dna, bytes32 moodHash) {
        dna = _sigilDNA[tokenId];
        moodHash = _sigilMood[tokenId];
    }
}
