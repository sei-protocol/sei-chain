// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

interface ISoulSigil {
    function ownerOf(uint256 tokenId) external view returns (address);
}

interface IX402SeiReceiver {
    function receivePayload(bytes calldata payload) external;
}

contract KinRouter {
    address public soulSigilContract;
    address public seiReceiver;
    mapping(address => bool) public trustedKins;
    address public deployer;

    event Forwarded(address indexed from, address indexed to, bytes payload);
    event KinAdded(address indexed kin);
    event KinRemoved(address indexed kin);

    modifier onlyKin() {
        require(trustedKins[msg.sender], "Not trusted Kin");
        _;
    }

    constructor(address _soulSigil, address _seiReceiver) {
        soulSigilContract = _soulSigil;
        seiReceiver = _seiReceiver;
        deployer = msg.sender;
        trustedKins[msg.sender] = true;
    }

    function addKin(address kin) external {
        require(msg.sender == deployer, "Only deployer");
        trustedKins[kin] = true;
        emit KinAdded(kin);
    }

    function removeKin(address kin) external {
        require(msg.sender == deployer, "Only deployer");
        trustedKins[kin] = false;
        emit KinRemoved(kin);
    }

    function forwardToSei(bytes calldata payload, uint256 sigilId) external onlyKin {
        require(
            ISoulSigil(soulSigilContract).ownerOf(sigilId) == msg.sender,
            "Sender not SoulSigil owner"
        );
        
        // Mood-aware hook placeholder (to be expanded)
        _verifyMoodEntropy(payload);

        IX402SeiReceiver(seiReceiver).receivePayload(payload);
        emit Forwarded(msg.sender, seiReceiver, payload);
    }

    function _verifyMoodEntropy(bytes calldata) internal pure {
        // Placeholder — extend with biometric/mood entropy ZK validation
    }
}
