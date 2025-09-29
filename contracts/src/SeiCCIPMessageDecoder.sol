// SPDX-License-Identifier: UNLICENSED
pragma solidity ^0.8.20;

library SeiCCIPMessageDecoder {
    struct KinPayload {
        address sender;
        address receiver;
        address token;
        uint256 amount;
        bytes32 refHash;
    }

    function decodeMessage(bytes memory data) internal pure returns (KinPayload memory) {
        require(data.length == 160, "Invalid payload size");

        (
            address sender,
            address receiver,
            address token,
            uint256 amount,
            bytes32 refHash
        ) = abi.decode(data, (address, address, address, uint256, bytes32));

        return KinPayload(sender, receiver, token, amount, refHash);
    }
}
