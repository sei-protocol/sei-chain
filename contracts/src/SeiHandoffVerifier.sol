// SPDX-License-Identifier: UNLICENSED
pragma solidity ^0.8.20;

interface ICircleBridge {
    function isValidMessage(bytes calldata message) external view returns (bool);
}

interface IChainlinkRouter {
    function verifyPayload(bytes calldata payload) external view returns (bool);
}

contract SeiHandoffVerifier {
    address public owner;
    address public immutable circleBridge;
    address public immutable chainlinkRouter;

    event ValidatedCircleUSDC(bytes32 indexed msgHash);
    event ValidatedChainlinkCCIP(bytes32 indexed msgHash);

    constructor(address _circleBridge, address _chainlinkRouter) {
        owner = msg.sender;
        circleBridge = _circleBridge;
        chainlinkRouter = _chainlinkRouter;
    }

    function verifyCircle(bytes calldata message) external {
        require(ICircleBridge(circleBridge).isValidMessage(message), "Invalid Circle USDC");
        emit ValidatedCircleUSDC(keccak256(message));
    }

    function verifyCCIP(bytes calldata payload) external {
        require(IChainlinkRouter(chainlinkRouter).verifyPayload(payload), "Invalid Chainlink Payload");
        emit ValidatedChainlinkCCIP(keccak256(payload));
    }

    function updateOwner(address newOwner) external {
        require(msg.sender == owner, "Not owner");
        owner = newOwner;
    }
}
