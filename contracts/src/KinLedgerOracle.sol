// SPDX-License-Identifier: UNLICENSED
pragma solidity ^0.8.20;

contract KinLedgerOracle {
    struct LedgerEntry {
        address submitter;
        bytes32 moodHash;
        string emotion;
        uint256 royaltyPercent;
        uint256 timestamp;
    }

    mapping(bytes32 => LedgerEntry) public ledger;
    address public admin;

    event LedgerUpdated(bytes32 indexed proofId, address submitter, string emotion, uint256 royaltyPercent);

    modifier onlyAdmin() {
        require(msg.sender == admin, "Not admin");
        _;
    }

    constructor() {
        admin = msg.sender;
    }

    function writeEntry(
        bytes32 proofId,
        string calldata emotion,
        uint256 royaltyPercent
    ) external {
        require(ledger[proofId].timestamp == 0, "Already written");
        ledger[proofId] = LedgerEntry({
            submitter: msg.sender,
            moodHash: keccak256(abi.encodePacked(emotion, block.timestamp, msg.sender)),
            emotion: emotion,
            royaltyPercent: royaltyPercent,
            timestamp: block.timestamp
        });

        emit LedgerUpdated(proofId, msg.sender, emotion, royaltyPercent);
    }

    function getMood(bytes32 proofId) external view returns (string memory, uint256) {
        return (ledger[proofId].emotion, ledger[proofId].royaltyPercent);
    }
}
