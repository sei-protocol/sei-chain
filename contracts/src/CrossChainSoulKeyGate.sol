// SPDX-License-Identifier: UNLICENSED 
pragma solidity ^0.8.20;

import "@openzeppelin/contracts/access/Ownable.sol";

/// -----------------------------------------------------------------------
/// Interfaces
/// -----------------------------------------------------------------------

interface IMessageTransmitter {
    function receiveMessage(bytes calldata message, bytes calldata attestation) external returns (bool);
}

interface ICrossChainMessageProcessor {
    function processMessage(address bridge, bytes calldata message, bytes calldata attestation) external returns (bool);
}

interface ISolanaProofConsumer {
    function receiveSolanaProof(address account, bytes32 proofHash, uint256 amount, bytes calldata metadata)
        external
        returns (bool);
}

interface IZkMultiFactorProofVerifier {
    function verify(bytes calldata proof, bytes calldata publicSignals) external view returns (bool);
}

/// -----------------------------------------------------------------------
/// Basic Message Transmitter
/// -----------------------------------------------------------------------

contract BasicMessageTransmitter is IMessageTransmitter, Ownable {
    address public processor;

    event ProcessorUpdated(address indexed previousProcessor, address indexed newProcessor);
    event MessageRelayed(address indexed bridge, bytes message, bytes attestation);

    constructor(address processor_) Ownable(msg.sender) {
        _updateProcessor(processor_);
    }

    function setProcessor(address newProcessor) external onlyOwner {
        _updateProcessor(newProcessor);
    }

    function receiveMessage(bytes calldata message, bytes calldata attestation)
        external
        override
        returns (bool)
    {
        require(processor != address(0), "BasicMessageTransmitter: processor not set");
        emit MessageRelayed(msg.sender, message, attestation);
        return ICrossChainMessageProcessor(processor).processMessage(msg.sender, message, attestation);
    }

    function _updateProcessor(address newProcessor) private {
        require(newProcessor != address(0), "BasicMessageTransmitter: zero processor");
        address previous = processor;
        processor = newProcessor;
        emit ProcessorUpdated(previous, newProcessor);
    }
}

/// -----------------------------------------------------------------------
/// CrossChain SoulKey Gate (Main Access Controller)
/// -----------------------------------------------------------------------

contract CrossChainSoulKeyGate is ICrossChainMessageProcessor, ISolanaProofConsumer, Ownable {
    enum SourceChain {
        Unknown,
        Sei,
        Polygon,
        Solana
    }

    struct AccessGrant {
        bool valid;
        bytes32 proofHash;
        SourceChain source;
        uint256 amount;
        uint64 timestamp;
        bytes32 attestationHash;
    }

    address public messageTransmitter;
    address public solanaRelayer;
    IZkMultiFactorProofVerifier public verifier; // optional zk-verifier path

    mapping(address => SourceChain) public registeredBridges;
    mapping(address => AccessGrant) private _grants;

    event MessageTransmitterUpdated(address indexed previousTransmitter, address indexed newTransmitter);
    event BridgeRegistered(address indexed bridge, SourceChain indexed source);
    event BridgeUnregistered(address indexed bridge);
    event SolanaRelayerUpdated(address indexed previousRelayer, address indexed newRelayer);
    event AccessGranted(address indexed account, bytes32 indexed proofHash, SourceChain indexed source, uint256 amount);
    event AccessRevoked(address indexed account);

    constructor(address transmitter, address solanaRelayer_, address verifier_) Ownable(msg.sender) {
        messageTransmitter = transmitter;
        solanaRelayer = solanaRelayer_;
        verifier = IZkMultiFactorProofVerifier(verifier_);
    }

    modifier onlyTransmitter() {
        require(msg.sender == messageTransmitter, "CrossChainSoulKeyGate: invalid transmitter");
        _;
    }

    modifier onlySolanaRelayer() {
        require(msg.sender == solanaRelayer, "CrossChainSoulKeyGate: invalid solana relayer");
        _;
    }

    function registerBridge(address bridge, SourceChain source) external onlyOwner {
        require(bridge != address(0) && source != SourceChain.Unknown, "Invalid bridge or source");
        registeredBridges[bridge] = source;
        emit BridgeRegistered(bridge, source);
    }

    function unregisterBridge(address bridge) external onlyOwner {
        delete registeredBridges[bridge];
        emit BridgeUnregistered(bridge);
    }

    function setMessageTransmitter(address transmitter) external onlyOwner {
        require(transmitter != address(0), "zero transmitter");
        address prev = messageTransmitter;
        messageTransmitter = transmitter;
        emit MessageTransmitterUpdated(prev, transmitter);
    }

    function setSolanaRelayer(address newRelayer) external onlyOwner {
        require(newRelayer != address(0), "zero relayer");
        address prev = solanaRelayer;
        solanaRelayer = newRelayer;
        emit SolanaRelayerUpdated(prev, newRelayer);
    }

    function processMessage(address bridge, bytes calldata message, bytes calldata attestation)
        external
        override
        onlyTransmitter
        returns (bool)
    {
        SourceChain src = registeredBridges[bridge];
        require(src != SourceChain.Unknown, "unregistered bridge");
        (address account, bytes32 proofHash, uint256 amount) = abi.decode(message, (address, bytes32, uint256));
        _grantAccess(account, proofHash, src, amount, attestation);
        return true;
    }

    function receiveSolanaProof(
        address account,
        bytes32 proofHash,
        uint256 amount,
        bytes calldata metadata
    ) external override onlySolanaRelayer returns (bool) {
        _grantAccess(account, proofHash, SourceChain.Solana, amount, metadata);
        return true;
    }

    function verifyZkProof(address account, bytes calldata proof, bytes calldata signals) external onlyOwner returns (bool) {
        require(address(verifier) != address(0), "verifier not set");
        require(verifier.verify(proof, signals), "invalid zk proof");
        bytes32 ph = keccak256(abi.encodePacked(proof, signals));
        _grantAccess(account, ph, SourceChain.Unknown, 0, abi.encode(ph));
        return true;
    }

    function hasAccess(address account) external view returns (bool) {
        return _grants[account].valid;
    }

    function getAccessGrant(address account)
        external
        view
        returns (bool valid, bytes32 proofHash, SourceChain source, uint256 amount, uint64 timestamp)
    {
        AccessGrant memory g = _grants[account];
        return (g.valid, g.proofHash, g.source, g.amount, g.timestamp);
    }

    function revokeAccess(address account) external onlyOwner {
        require(_grants[account].valid, "no grant");
        delete _grants[account];
        emit AccessRevoked(account);
    }

    function _grantAccess(address account, bytes32 proofHash, SourceChain source, uint256 amount, bytes memory attestation)
        internal
    {
        require(account != address(0), "zero account");
        _grants[account] = AccessGrant(true, proofHash, source, amount, uint64(block.timestamp), keccak256(attestation));
        emit AccessGranted(account, proofHash, source, amount);
    }
}

/// -----------------------------------------------------------------------
/// Chain Bridges (Sei, Polygon, Solana)
/// -----------------------------------------------------------------------

contract SeiToEvmBridge is Ownable {
    address public messageTransmitter;

    event MessageTransmitterUpdated(address indexed prev, address indexed next);
    event SeiProofForwarded(address indexed sender, address indexed account, uint256 amount, bytes32 proofHash);

    constructor(address transmitter) Ownable(msg.sender) {
        _updateTransmitter(transmitter);
    }

    function setMessageTransmitter(address transmitter) external onlyOwner {
        _updateTransmitter(transmitter);
    }

    function transferToEVM(address account, uint256 amount, bytes32 proofHash) external returns (bool) {
        bytes memory msgData = abi.encode(account, proofHash, amount);
        bytes memory att = abi.encodePacked(keccak256(abi.encodePacked(msgData, block.chainid, address(this))));
        bool ok = IMessageTransmitter(messageTransmitter).receiveMessage(msgData, att);
        require(ok, "rejected");
        emit SeiProofForwarded(msg.sender, account, amount, proofHash);
        return true;
    }

    function _updateTransmitter(address transmitter) private {
        require(transmitter != address(0), "zero transmitter");
        address prev = messageTransmitter;
        messageTransmitter = transmitter;
        emit MessageTransmitterUpdated(prev, transmitter);
    }
}

contract PolygonSoulKeyGate is Ownable {
    address public messageTransmitter;

    event MessageTransmitterUpdated(address indexed prev, address indexed next);
    event PolygonProofForwarded(address indexed sender, address indexed account, uint256 amount, bytes32 proofHash);

    constructor(address transmitter) Ownable(msg.sender) {
        _updateTransmitter(transmitter);
    }

    function grantAccessFromPolygon(address account, uint256 amount, bytes32 proofHash, bytes calldata meta)
        external
        returns (bool)
    {
        bytes memory msgData = abi.encode(account, proofHash, amount);
        bytes memory att = meta.length == 0 ? abi.encodePacked(keccak256(msgData)) : abi.encodePacked(meta, keccak256(msgData));
        bool ok = IMessageTransmitter(messageTransmitter).receiveMessage(msgData, att);
        require(ok, "rejected");
        emit PolygonProofForwarded(msg.sender, account, amount, proofHash);
        return true;
    }

    function _updateTransmitter(address transmitter) private {
        require(transmitter != address(0), "zero transmitter");
        address prev = messageTransmitter;
        messageTransmitter = transmitter;
        emit MessageTransmitterUpdated(prev, transmitter);
    }
}

contract SolanaToEvmBridge is Ownable {
    address public wormholeRelayer;
    ISolanaProofConsumer public soulKeyGate;

    event WormholeRelayerUpdated(address indexed prev, address indexed next);
    event SoulKeyGateUpdated(address indexed prev, address indexed next);
    event SolanaProofForwarded(address indexed relayer, address indexed account, uint256 amount, bytes32 proofHash);

    constructor(address relayer, address gate) Ownable(msg.sender) {
        _updateRelayer(relayer);
        _updateGate(gate);
    }

    modifier onlyRelayer() {
        require(msg.sender == wormholeRelayer, "not relayer");
        _;
    }

    function receiveSolanaProof(bytes calldata proof, bytes calldata meta) external onlyRelayer returns (bool) {
        (address account, bytes32 ph, uint256 amt) = abi.decode(proof, (address, bytes32, uint256));
        bool ok = soulKeyGate.receiveSolanaProof(account, ph, amt, meta);
        require(ok, "rejected");
        emit SolanaProofForwarded(msg.sender, account, amt, ph);
        return ok;
    }

    function _updateRelayer(address n) private {
        require(n != address(0), "zero relayer");
        address p = wormholeRelayer;
        wormholeRelayer = n;
        emit WormholeRelayerUpdated(p, n);
    }

    function _updateGate(address n) private {
        require(n != address(0), "zero gate");
        address p = address(soulKeyGate);
        soulKeyGate = ISolanaProofConsumer(n);
        emit SoulKeyGateUpdated(p, n);
    }
}
