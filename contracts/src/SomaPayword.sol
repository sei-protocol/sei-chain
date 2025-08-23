// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

/**
 * @title SomaProtocol
 * @dev Soma: a simple staking + reflection protocol, where deposited funds "rest" (soma = body)
 * and accrue balance over time. This is conceptual scaffolding to show how the protocol might work.
 */
contract SomaProtocol {
    mapping(address => uint256) private balances;
    mapping(address => uint256) private depositBlock;

    uint256 public totalDeposits;
    uint256 public constant INTEREST_PER_BLOCK = 1e14; // toy interest factor

    event Deposited(address indexed user, uint256 amount);
    event Withdrawn(address indexed user, uint256 reward);

    function deposit() external payable {
        require(msg.value > 0, "No value sent");
        _updateReward(msg.sender);
        balances[msg.sender] += msg.value;
        depositBlock[msg.sender] = block.number;
        totalDeposits += msg.value;
        emit Deposited(msg.sender, msg.value);
    }

    function withdraw(uint256 amount) external {
        _updateReward(msg.sender);
        require(balances[msg.sender] >= amount, "Not enough balance");
        balances[msg.sender] -= amount;
        totalDeposits -= amount;
        payable(msg.sender).transfer(amount);
        emit Withdrawn(msg.sender, amount);
    }

    function _updateReward(address user) internal {
        uint256 blocksPassed = block.number - depositBlock[user];
        uint256 reward = balances[user] * blocksPassed * INTEREST_PER_BLOCK / 1e18;
        balances[user] += reward;
        depositBlock[user] = block.number;
    }

    function balanceOf(address user) external view returns (uint256) {
        uint256 blocksPassed = block.number - depositBlock[user];
        uint256 reward = balances[user] * blocksPassed * INTEREST_PER_BLOCK / 1e18;
        return balances[user] + reward;
    }
}

/**
 * @title PaywordProtocol
 * @dev Payword: a micro-payment protocol where a user precommits a hash chain and reveals words step by step
 * to incrementally pay a merchant. This is a simplified smart contract sketch.
 */
contract PaywordProtocol {
    struct Channel {
        address payer;
        address payee;
        bytes32 rootHash;   // hash commitment to chain
        uint256 deposit;
        uint256 spent;
        bool active;
    }

    mapping(bytes32 => Channel) public channels;

    event ChannelOpened(bytes32 indexed id, address payer, address payee, uint256 deposit);
    event PaymentClaimed(bytes32 indexed id, uint256 amount);
    event ChannelClosed(bytes32 indexed id);

    function openChannel(address payee, bytes32 rootHash) external payable returns (bytes32) {
        require(msg.value > 0, "Deposit required");
        bytes32 id = keccak256(abi.encode(msg.sender, payee, block.timestamp, rootHash));
        channels[id] = Channel({
            payer: msg.sender,
            payee: payee,
            rootHash: rootHash,
            deposit: msg.value,
            spent: 0,
            active: true
        });
        emit ChannelOpened(id, msg.sender, payee, msg.value);
        return id;
    }

    /**
     * @dev Payee submits revealed preimage in the payword chain.
     * `hash(preimage) == lastHash`, walk backwards until root.
     */
    function claimPayment(bytes32 channelId, bytes32 preimage, uint256 amount, bytes32 lastHash) external {
        Channel storage c = channels[channelId];
        require(c.active, "Inactive channel");
        require(msg.sender == c.payee, "Only payee can claim");
        require(c.spent + amount <= c.deposit, "Not enough deposit");

        // Basic check: preimage hashes to lastHash
        require(keccak256(abi.encode(preimage)) == lastHash, "Invalid payword");

        c.spent += amount;
        payable(c.payee).transfer(amount);
        emit PaymentClaimed(channelId, amount);
    }

    function closeChannel(bytes32 channelId) external {
        Channel storage c = channels[channelId];
        require(c.active, "Inactive");
        require(msg.sender == c.payer || msg.sender == c.payee, "Not authorized");
        c.active = false;
        uint256 refund = c.deposit - c.spent;
        if (refund > 0) {
            payable(c.payer).transfer(refund);
        }
        emit ChannelClosed(channelId);
    }
}

