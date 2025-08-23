// SPDX-License-Identifier: MIT
pragma solidity ^0.8.24;

/// @title SeiWord – Sovereign Payword Settlement for Sei Protocol
/// @notice Embedded KinKey logic, NFT-gated channels, and Soma yield inside Sei-native payment streams
contract SeiWord {
    struct WordChannel {
        address initiator;
        address settler;
        bytes32 rootWordHash;   // Preimage commitment (hash chain root)
        uint256 deposit;
        uint256 spent;
        uint256 lastBlockSeen;
        bool active;
        address soulSigil;      // Optional NFT required to unlock stream
    }

    uint256 public constant YIELD_RATE = 1e13;
    mapping(bytes32 => WordChannel) public channels;
    mapping(address => uint256) public somaBalances;
    mapping(address => uint256) public lastUpdatedBlock;

    event ChannelInitiated(bytes32 indexed channelId, address initiator, address settler, uint256 deposit);
    event WordRevealed(bytes32 indexed channelId, uint256 amount, address kinKey);
    event ChannelTerminated(bytes32 indexed channelId);
    event YieldWithdrawn(address indexed user, uint256 amount);

    modifier soulCheck(address nft) {
        require(nft == address(0) || IERC721(nft).balanceOf(msg.sender) > 0, "SoulSigil missing");
        _;
    }

    /// Deposit passive Soma funds
    function depositSoma() external payable {
        _applyYield(msg.sender);
        somaBalances[msg.sender] += msg.value;
    }

    function _applyYield(address user) internal {
        uint256 delta = block.number - lastUpdatedBlock[user];
        uint256 earned = somaBalances[user] * delta * YIELD_RATE / 1e18;
        somaBalances[user] += earned;
        lastUpdatedBlock[user] = block.number;
    }

    function withdrawYield() external {
        _applyYield(msg.sender);
        uint256 amount = somaBalances[msg.sender];
        require(amount > 0, "Zero balance");
        somaBalances[msg.sender] = 0;
        payable(msg.sender).transfer(amount);
        emit YieldWithdrawn(msg.sender, amount);
    }

    /// Begin a sovereign word stream
    function initiateChannel(address settler, bytes32 rootWordHash, address soulSigil) external payable returns (bytes32) {
        require(msg.value > 0, "Deposit required");
        bytes32 id = keccak256(abi.encode(msg.sender, settler, block.timestamp, rootWordHash));
        channels[id] = WordChannel({
            initiator: msg.sender,
            settler: settler,
            rootWordHash: rootWordHash,
            deposit: msg.value,
            spent: 0,
            lastBlockSeen: block.number,
            active: true,
            soulSigil: soulSigil
        });
        emit ChannelInitiated(id, msg.sender, settler, msg.value);
        return id;
    }

    /// Reveal the next word in the chain + settle
    function revealWord(
        bytes32 channelId,
        bytes32 preimage,
        uint256 amount,
        bytes32 expectedHash,
        address kinKey
    ) external soulCheck(channels[channelId].soulSigil) {
        WordChannel storage ch = channels[channelId];
        require(ch.active, "Closed");
        require(msg.sender == ch.settler, "Only settler can claim");
        require(ch.spent + amount <= ch.deposit, "Exceeds balance");
        require(keccak256(abi.encode(preimage)) == expectedHash, "Bad preimage");

        ch.spent += amount;
        ch.lastBlockSeen = block.number;
        payable(ch.settler).transfer(amount);
        emit WordRevealed(channelId, amount, kinKey);
    }

    function terminateChannel(bytes32 channelId) external {
        WordChannel storage ch = channels[channelId];
        require(ch.active, "Already closed");
        require(msg.sender == ch.initiator || msg.sender == ch.settler, "Unauthorized");
        ch.active = false;
        uint256 refund = ch.deposit - ch.spent;
        if (refund > 0) {
            payable(ch.initiator).transfer(refund);
        }
        emit ChannelTerminated(channelId);
    }

    receive() external payable {}
}

interface IERC721 {
    function balanceOf(address owner) external view returns (uint256);
}

