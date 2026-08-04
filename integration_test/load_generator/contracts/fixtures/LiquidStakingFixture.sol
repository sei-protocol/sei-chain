// SPDX-License-Identifier: MIT
pragma solidity ^0.8.28;

import {IFixtureProxyImplementation, SafeToken, FixtureReentrancyGuard} from "./FixtureCore.sol";

interface IExchangeRateOracle {
    function exchangeRate() external view returns (uint256);
}

contract DeterministicExchangeRateOracle {
    address public immutable owner;
    uint256 public exchangeRate;

    constructor(address owner_, uint256 initialRate) {
        require(owner_ != address(0), "ZERO_OWNER");
        owner = owner_;
        _setRate(initialRate);
    }

    function setExchangeRate(uint256 newRate) external {
        require(msg.sender == owner, "NOT_OWNER");
        _setRate(newRate);
    }

    function _setRate(uint256 newRate) private {
        require(newRate >= 5e17 && newRate <= 2e18, "RATE_OUT_OF_RANGE");
        exchangeRate = newRate;
    }
}

library LiquidStakingStorage {
    bytes32 internal constant SLOT = keccak256("sei.replay.fixture.liquid-staking.storage.v1");

    struct Request {
        address owner;
        uint128 assets;
        uint64 claimableAt;
        bool claimed;
    }

    struct Layout {
        bool initialized;
        address underlying;
        address receiptToken;
        address exchangeRateOracle;
        uint64 withdrawalDelay;
        uint256 nextRequestId;
        mapping(uint256 => Request) requests;
    }

    function layout() internal pure returns (Layout storage s) {
        bytes32 slot = SLOT;
        assembly { s.slot := slot }
    }
}

contract LiquidStakingImplementation is IFixtureProxyImplementation, FixtureReentrancyGuard {
    using SafeToken for address;

    bytes32 private constant IMPLEMENTATION_SLOT =
        bytes32(uint256(keccak256("eip1967.proxy.implementation")) - 1);
    uint256 public constant MAX_BATCH = 16;

    event Submitted(address indexed sender, address indexed recipient, uint256 assets, uint256 shares);
    event WithdrawalRequested(uint256 indexed requestId, address indexed owner, uint256 shares, uint256 assets);
    event WithdrawalClaimed(uint256 indexed requestId, address indexed owner, uint256 assets);

    function proxiableUUID() external pure returns (bytes32) { return IMPLEMENTATION_SLOT; }

    function initialize(
        address underlying_,
        address receiptToken_,
        address exchangeRateOracle_,
        uint64 withdrawalDelay_
    ) external {
        LiquidStakingStorage.Layout storage s = LiquidStakingStorage.layout();
        require(!s.initialized, "ALREADY_INITIALIZED");
        require(
            underlying_.code.length != 0 && receiptToken_.code.length != 0
                && exchangeRateOracle_.code.length != 0,
            "BAD_CONFIG"
        );
        require(withdrawalDelay_ >= 1 minutes && withdrawalDelay_ <= 7 days, "BAD_DELAY");
        s.initialized = true;
        s.underlying = underlying_;
        s.receiptToken = receiptToken_;
        s.exchangeRateOracle = exchangeRateOracle_;
        s.withdrawalDelay = withdrawalDelay_;
        s.nextRequestId = 1;
    }

    function underlyingToken() external view returns (address) {
        return LiquidStakingStorage.layout().underlying;
    }

    function stake(uint256 assets) external nonReentrant returns (uint256 shares) {
        return _deposit(assets, msg.sender);
    }

    function deposit(uint256 assets, address receiver) external nonReentrant returns (uint256 shares) {
        return _deposit(assets, receiver);
    }

    function requestWithdrawal(uint256 shares) external nonReentrant returns (uint256 requestId) {
        return _request(shares, msg.sender);
    }

    function requestWithdrawals(uint256[] calldata shares, address owner)
        external
        nonReentrant
        returns (uint256[] memory requestIds)
    {
        require(owner == msg.sender, "OWNER_MUST_CALL");
        require(shares.length != 0 && shares.length <= MAX_BATCH, "BAD_BATCH");
        requestIds = new uint256[](shares.length);
        for (uint256 i; i < shares.length; ++i) {
            requestIds[i] = _request(shares[i], owner);
        }
    }

    function claimWithdrawal(uint256 requestId, address recipient)
        external
        nonReentrant
        returns (uint256 assets)
    {
        require(recipient != address(0), "ZERO_RECIPIENT");
        LiquidStakingStorage.Layout storage s = LiquidStakingStorage.layout();
        LiquidStakingStorage.Request storage request = s.requests[requestId];
        require(request.owner == msg.sender && !request.claimed, "INVALID_REQUEST");
        require(block.timestamp >= request.claimableAt, "NOT_CLAIMABLE");
        request.claimed = true;
        assets = request.assets;
        require(SafeToken.staticBalanceOf(s.underlying, address(this)) >= assets, "INSUFFICIENT_LIQUIDITY");
        s.underlying.safeTransfer(recipient, assets);
        emit WithdrawalClaimed(requestId, msg.sender, assets);
    }

    function getWithdrawalRequest(uint256 requestId)
        external
        view
        returns (address owner, uint256 assets, uint256 claimableAt, bool claimed)
    {
        LiquidStakingStorage.Request storage request =
            LiquidStakingStorage.layout().requests[requestId];
        return (request.owner, request.assets, request.claimableAt, request.claimed);
    }

    function _deposit(uint256 assets, address receiver) private returns (uint256 shares) {
        require(assets != 0 && receiver != address(0), "BAD_DEPOSIT");
        LiquidStakingStorage.Layout storage s = LiquidStakingStorage.layout();
        uint256 rate = IExchangeRateOracle(s.exchangeRateOracle).exchangeRate();
        require(rate >= 5e17 && rate <= 2e18, "BAD_ORACLE_RATE");
        shares = assets * 1e18 / rate;
        require(shares != 0, "ZERO_SHARES");
        s.underlying.safeTransferFrom(msg.sender, address(this), assets);
        s.receiptToken.safeMint(receiver, shares);
        emit Submitted(msg.sender, receiver, assets, shares);
    }

    function _request(uint256 shares, address owner) private returns (uint256 requestId) {
        require(shares != 0, "ZERO_SHARES");
        LiquidStakingStorage.Layout storage s = LiquidStakingStorage.layout();
        uint256 rate = IExchangeRateOracle(s.exchangeRateOracle).exchangeRate();
        require(rate >= 5e17 && rate <= 2e18, "BAD_ORACLE_RATE");
        uint256 assets = shares * rate / 1e18;
        require(assets != 0 && assets <= type(uint128).max, "BAD_ASSETS");
        s.receiptToken.safeBurn(owner, shares);
        requestId = s.nextRequestId++;
        s.requests[requestId] = LiquidStakingStorage.Request(
            owner, uint128(assets), uint64(block.timestamp + s.withdrawalDelay), false
        );
        emit WithdrawalRequested(requestId, owner, shares, assets);
    }
}
