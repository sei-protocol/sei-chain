// SPDX-License-Identifier: UNLICENSED
pragma solidity ^0.8.20;

interface IBeaconVerifier {
    function verifyBeaconSignature(
        address user,
        bytes32 wifiHash,
        bytes calldata sig
    ) external view returns (bool);
}

contract SeiFastLaneRouter {
    mapping(address => bytes32) public lastWifiHash;
    mapping(address => uint256) public lastSeenBlock;

    address public beaconVerifier;
    uint256 public priorityWindow = 10; // blocks
    address public owner;

    event PresenceProofSubmitted(address indexed user, bytes32 wifiHash);
    event PriorityWindowUpdated(uint256 newWindow);
    event BeaconVerifierUpdated(address indexed newVerifier);
    event OwnershipTransferred(address indexed previousOwner, address indexed newOwner);

    modifier onlyOwner() {
        require(msg.sender == owner, "Not authorized");
        _;
    }

    constructor(address _beaconVerifier) {
        require(_beaconVerifier != address(0), "Invalid beacon verifier");
        beaconVerifier = _beaconVerifier;
        owner = msg.sender;
        emit OwnershipTransferred(address(0), msg.sender);
    }

    function submitPresenceProof(
        bytes32 wifiHash,
        bytes calldata beaconSig
    ) external {
        require(
            IBeaconVerifier(beaconVerifier).verifyBeaconSignature(
                msg.sender,
                wifiHash,
                beaconSig
            ),
            "Invalid beacon signature"
        );

        lastWifiHash[msg.sender] = wifiHash;
        lastSeenBlock[msg.sender] = block.number;

        emit PresenceProofSubmitted(msg.sender, wifiHash);
    }

    function hasFastlaneAccess(address user) external view returns (bool) {
        return (block.number - lastSeenBlock[user]) <= priorityWindow;
    }

    function setPriorityWindow(uint256 newWindow) external onlyOwner {
        priorityWindow = newWindow;
        emit PriorityWindowUpdated(newWindow);
    }

    function setBeaconVerifier(address newVerifier) external onlyOwner {
        require(newVerifier != address(0), "Invalid beacon verifier");
        beaconVerifier = newVerifier;
        emit BeaconVerifierUpdated(newVerifier);
    }

    function transferOwnership(address newOwner) external onlyOwner {
        require(newOwner != address(0), "Invalid owner");
        emit OwnershipTransferred(owner, newOwner);
        owner = newOwner;
    }
}
