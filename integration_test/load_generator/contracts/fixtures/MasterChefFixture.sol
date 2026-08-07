// SPDX-License-Identifier: MIT
pragma solidity ^0.8.28;

import {SafeToken, FixtureReentrancyGuard} from "./FixtureCore.sol";

contract DeterministicMasterChef is FixtureReentrancyGuard {
    using SafeToken for address;

    uint256 public constant MAX_POOLS = 32;
    uint256 public constant MAX_REWARD_PER_SECOND = 1e24;
    uint256 private constant ACC_PRECISION = 1e18;

    struct PoolInfo {
        address stakingToken;
        uint96 rewardPerSecond;
        uint64 lastRewardTime;
        uint256 accRewardPerShare;
        uint256 totalStaked;
        bool enabled;
    }

    struct UserInfo {
        uint256 amount;
        uint256 rewardDebt;
        uint256 unpaidRewards;
    }

    address public immutable owner;
    address public immutable rewardToken;
    PoolInfo[] public poolInfo;
    mapping(uint256 => mapping(address => UserInfo)) public userInfo;

    event PoolAdded(uint256 indexed pid, address indexed stakingToken, uint256 rewardPerSecond);
    event PoolUpdated(uint256 indexed pid, uint256 rewardPerSecond, bool enabled);
    event Deposit(address indexed user, uint256 indexed pid, uint256 amount);
    event Withdraw(address indexed user, uint256 indexed pid, uint256 amount);
    event Harvest(address indexed user, uint256 indexed pid, uint256 amount);
    event EmergencyWithdraw(address indexed user, uint256 indexed pid, uint256 amount);

    constructor(address owner_, address rewardToken_) {
        require(owner_ != address(0) && rewardToken_.code.length != 0, "BAD_CONFIG");
        owner = owner_;
        rewardToken = rewardToken_;
    }

    function poolLength() external view returns (uint256) { return poolInfo.length; }

    function addPool(address stakingToken, uint96 rewardPerSecond) external {
        require(msg.sender == owner, "NOT_OWNER");
        require(poolInfo.length < MAX_POOLS, "TOO_MANY_POOLS");
        require(stakingToken.code.length != 0 && stakingToken != rewardToken, "BAD_STAKING_TOKEN");
        require(rewardPerSecond <= MAX_REWARD_PER_SECOND, "RATE_TOO_HIGH");
        poolInfo.push(PoolInfo(stakingToken, rewardPerSecond, uint64(block.timestamp), 0, 0, true));
        emit PoolAdded(poolInfo.length - 1, stakingToken, rewardPerSecond);
    }

    function setPool(uint256 pid, uint96 rewardPerSecond, bool enabled) external {
        require(msg.sender == owner, "NOT_OWNER");
        require(rewardPerSecond <= MAX_REWARD_PER_SECOND, "RATE_TOO_HIGH");
        _updatePool(pid);
        PoolInfo storage pool = poolInfo[pid];
        pool.rewardPerSecond = rewardPerSecond;
        pool.enabled = enabled;
        emit PoolUpdated(pid, rewardPerSecond, enabled);
    }

    function pendingReward(uint256 pid, address account) external view returns (uint256) {
        PoolInfo memory pool = poolInfo[pid];
        uint256 accumulated = pool.accRewardPerShare;
        if (pool.enabled && block.timestamp > pool.lastRewardTime && pool.totalStaked != 0) {
            accumulated +=
                (block.timestamp - pool.lastRewardTime) * uint256(pool.rewardPerSecond) * ACC_PRECISION
                    / pool.totalStaked;
        }
        UserInfo memory user = userInfo[pid][account];
        return user.amount * accumulated / ACC_PRECISION - user.rewardDebt + user.unpaidRewards;
    }

    function deposit(uint256 pid, uint256 amount) external nonReentrant {
        require(amount != 0, "ZERO_AMOUNT");
        _settle(pid, msg.sender);
        PoolInfo storage pool = poolInfo[pid];
        require(pool.enabled, "POOL_DISABLED");
        pool.stakingToken.safeTransferFrom(msg.sender, address(this), amount);
        pool.totalStaked += amount;
        UserInfo storage user = userInfo[pid][msg.sender];
        user.amount += amount;
        user.rewardDebt = user.amount * pool.accRewardPerShare / ACC_PRECISION;
        emit Deposit(msg.sender, pid, amount);
    }

    function withdraw(uint256 pid, uint256 amount) external nonReentrant {
        _settle(pid, msg.sender);
        PoolInfo storage pool = poolInfo[pid];
        UserInfo storage user = userInfo[pid][msg.sender];
        require(amount != 0 && user.amount >= amount, "BAD_AMOUNT");
        user.amount -= amount;
        pool.totalStaked -= amount;
        user.rewardDebt = user.amount * pool.accRewardPerShare / ACC_PRECISION;
        pool.stakingToken.safeTransfer(msg.sender, amount);
        emit Withdraw(msg.sender, pid, amount);
    }

    function harvest(uint256 pid) external nonReentrant {
        _settle(pid, msg.sender);
        PoolInfo storage pool = poolInfo[pid];
        UserInfo storage user = userInfo[pid][msg.sender];
        uint256 reward = user.unpaidRewards;
        user.unpaidRewards = 0;
        user.rewardDebt = user.amount * pool.accRewardPerShare / ACC_PRECISION;
        if (reward != 0) rewardToken.safeMint(msg.sender, reward);
        emit Harvest(msg.sender, pid, reward);
    }

    function emergencyWithdraw(uint256 pid) external nonReentrant {
        PoolInfo storage pool = poolInfo[pid];
        UserInfo storage user = userInfo[pid][msg.sender];
        uint256 amount = user.amount;
        require(amount != 0, "NOT_STAKED");
        pool.totalStaked -= amount;
        delete userInfo[pid][msg.sender];
        pool.stakingToken.safeTransfer(msg.sender, amount);
        emit EmergencyWithdraw(msg.sender, pid, amount);
    }

    function _settle(uint256 pid, address account) private {
        _updatePool(pid);
        PoolInfo storage pool = poolInfo[pid];
        UserInfo storage user = userInfo[pid][account];
        uint256 accrued = user.amount * pool.accRewardPerShare / ACC_PRECISION;
        if (accrued > user.rewardDebt) user.unpaidRewards += accrued - user.rewardDebt;
        user.rewardDebt = accrued;
    }

    function _updatePool(uint256 pid) private {
        PoolInfo storage pool = poolInfo[pid];
        uint256 elapsed = block.timestamp - pool.lastRewardTime;
        if (elapsed != 0) {
            if (pool.enabled && pool.totalStaked != 0) {
                pool.accRewardPerShare +=
                    elapsed * uint256(pool.rewardPerSecond) * ACC_PRECISION / pool.totalStaked;
            }
            pool.lastRewardTime = uint64(block.timestamp);
        }
    }
}
