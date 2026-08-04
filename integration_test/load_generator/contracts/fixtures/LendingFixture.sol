// SPDX-License-Identifier: MIT
pragma solidity ^0.8.28;

import {
    IFixtureProxyImplementation,
    SafeToken,
    FixtureReentrancyGuard
} from "./FixtureCore.sol";

interface IFixturePriceOracle {
    function price(address asset) external view returns (uint256);
}

interface IFixtureRateModel {
    function borrowRatePerSecond(uint256 utilizationWad) external view returns (uint256);
}

interface IFixtureComptroller {
    function checkLiquidity(address asset, uint256 supplied, uint256 borrowed)
        external
        view
        returns (bool, uint256);
}

contract DeterministicPriceOracle {
    address public immutable owner;
    mapping(address => uint256) public price;
    uint256 public configuredAssets;
    uint256 public constant MAX_ASSETS = 32;

    constructor(address owner_) {
        require(owner_ != address(0), "ZERO_OWNER");
        owner = owner_;
    }

    function setPrice(address asset, uint256 priceWad) external {
        require(msg.sender == owner, "NOT_OWNER");
        require(asset.code.length != 0 && priceWad >= 1e12 && priceWad <= 1e30, "BAD_PRICE");
        if (price[asset] == 0) {
            require(configuredAssets < MAX_ASSETS, "TOO_MANY_ASSETS");
            configuredAssets++;
        }
        price[asset] = priceWad;
    }
}

contract DeterministicRateModel {
    uint256 public immutable baseRatePerSecond;
    uint256 public immutable slopePerSecond;

    constructor(uint256 baseRatePerSecond_, uint256 slopePerSecond_) {
        require(baseRatePerSecond_ <= 1e14 && slopePerSecond_ <= 1e14, "RATE_TOO_HIGH");
        baseRatePerSecond = baseRatePerSecond_;
        slopePerSecond = slopePerSecond_;
    }

    function borrowRatePerSecond(uint256 utilizationWad) external view returns (uint256) {
        require(utilizationWad <= 1e18, "BAD_UTILIZATION");
        return baseRatePerSecond + slopePerSecond * utilizationWad / 1e18;
    }
}

contract FixtureComptroller {
    IFixturePriceOracle public immutable oracle;
    uint256 public immutable collateralFactorWad;

    constructor(address oracle_, uint256 collateralFactorWad_) {
        require(oracle_.code.length != 0, "BAD_ORACLE");
        require(collateralFactorWad_ >= 1e17 && collateralFactorWad_ <= 9e17, "BAD_FACTOR");
        oracle = IFixturePriceOracle(oracle_);
        collateralFactorWad = collateralFactorWad_;
    }

    function checkLiquidity(address asset, uint256 supplied, uint256 borrowed)
        external
        view
        returns (bool healthy, uint256 liquidityValue)
    {
        uint256 assetPrice = oracle.price(asset);
        require(assetPrice != 0, "NO_PRICE");
        uint256 collateralValue = supplied * assetPrice / 1e18 * collateralFactorWad / 1e18;
        uint256 borrowValue = borrowed * assetPrice / 1e18;
        healthy = collateralValue >= borrowValue;
        liquidityValue = healthy ? collateralValue - borrowValue : 0;
    }
}

library LendingStorage {
    bytes32 internal constant SLOT = keccak256("sei.replay.fixture.lending.storage.v1");

    struct Account {
        uint256 supplied;
        uint256 principalDebt;
        uint256 debtIndex;
    }

    struct Layout {
        bool initialized;
        address asset;
        address receiptToken;
        address comptroller;
        address oracle;
        address rateModel;
        uint256 totalSupplied;
        uint256 totalBorrows;
        uint256 borrowIndex;
        uint64 lastAccrual;
        mapping(address => Account) accounts;
    }

    function layout() internal pure returns (Layout storage s) {
        bytes32 slot = SLOT;
        assembly { s.slot := slot }
    }
}

contract LendingPoolImplementation is IFixtureProxyImplementation, FixtureReentrancyGuard {
    using SafeToken for address;

    bytes32 private constant IMPLEMENTATION_SLOT =
        bytes32(uint256(keccak256("eip1967.proxy.implementation")) - 1);

    event Supply(address indexed user, address indexed onBehalfOf, uint256 amount);
    event Withdraw(address indexed user, address indexed to, uint256 amount);
    event Borrow(address indexed user, uint256 amount, uint256 accountDebt);
    event Repay(address indexed payer, address indexed borrower, uint256 amount);
    event AccrueInterest(uint256 interest, uint256 borrowIndex);

    function proxiableUUID() external pure returns (bytes32) { return IMPLEMENTATION_SLOT; }

    function initialize(
        address asset_,
        address receiptToken_,
        address comptroller_,
        address oracle_,
        address rateModel_
    ) external {
        LendingStorage.Layout storage s = LendingStorage.layout();
        require(!s.initialized, "ALREADY_INITIALIZED");
        require(
            asset_.code.length != 0 && receiptToken_.code.length != 0
                && comptroller_.code.length != 0 && oracle_.code.length != 0
                && rateModel_.code.length != 0,
            "BAD_CONFIG"
        );
        s.initialized = true;
        s.asset = asset_;
        s.receiptToken = receiptToken_;
        s.comptroller = comptroller_;
        s.oracle = oracle_;
        s.rateModel = rateModel_;
        s.borrowIndex = 1e18;
        s.lastAccrual = uint64(block.timestamp);
    }

    function asset() external view returns (address) { return LendingStorage.layout().asset; }
    function totalSupply() external view returns (uint256) { return LendingStorage.layout().totalSupplied; }
    function totalBorrows() external view returns (uint256) { return LendingStorage.layout().totalBorrows; }

    function accountSnapshot(address account)
        external
        view
        returns (uint256 supplied, uint256 debt)
    {
        LendingStorage.Layout storage s = LendingStorage.layout();
        LendingStorage.Account storage a = s.accounts[account];
        supplied = a.supplied;
        debt = _currentDebt(a, s.borrowIndex);
    }

    function supply(address asset_, uint256 amount, address onBehalfOf, uint16 referralCode)
        external
        nonReentrant
    {
        referralCode;
        _supply(asset_, amount, onBehalfOf);
    }

    function mint(uint256 amount) external nonReentrant returns (uint256) {
        _supply(LendingStorage.layout().asset, amount, msg.sender);
        return 0;
    }

    function withdraw(address asset_, uint256 amount, address to)
        external
        nonReentrant
        returns (uint256)
    {
        return _withdraw(asset_, amount, to);
    }

    function redeem(uint256 redeemTokens) external nonReentrant returns (uint256) {
        _withdraw(LendingStorage.layout().asset, redeemTokens, msg.sender);
        return 0;
    }

    function borrow(uint256 amount) external nonReentrant {
        require(amount != 0, "ZERO_AMOUNT");
        LendingStorage.Layout storage s = LendingStorage.layout();
        _accrue(s);
        LendingStorage.Account storage a = s.accounts[msg.sender];
        uint256 debt = _syncDebt(a, s.borrowIndex) + amount;
        require(_isHealthy(s, a.supplied, debt), "INSUFFICIENT_COLLATERAL");
        require(SafeToken.staticBalanceOf(s.asset, address(this)) >= amount, "INSUFFICIENT_CASH");
        a.principalDebt = debt;
        a.debtIndex = s.borrowIndex;
        s.totalBorrows += amount;
        s.asset.safeTransfer(msg.sender, amount);
        emit Borrow(msg.sender, amount, debt);
    }

    function repayBorrow(uint256 amount) external nonReentrant returns (uint256) {
        return _repay(msg.sender, msg.sender, amount);
    }

    function repay(address asset_, uint256 amount, uint256, address onBehalfOf)
        external
        nonReentrant
        returns (uint256)
    {
        require(asset_ == LendingStorage.layout().asset, "WRONG_ASSET");
        return _repay(msg.sender, onBehalfOf, amount);
    }

    function accrueInterest() external returns (uint256) {
        return _accrue(LendingStorage.layout());
    }

    function _supply(address asset_, uint256 amount, address onBehalfOf) private {
        LendingStorage.Layout storage s = LendingStorage.layout();
        require(asset_ == s.asset && amount != 0 && onBehalfOf != address(0), "BAD_SUPPLY");
        _accrue(s);
        s.asset.safeTransferFrom(msg.sender, address(this), amount);
        s.accounts[onBehalfOf].supplied += amount;
        s.totalSupplied += amount;
        s.receiptToken.safeMint(onBehalfOf, amount);
        emit Supply(msg.sender, onBehalfOf, amount);
    }

    function _withdraw(address asset_, uint256 amount, address to) private returns (uint256) {
        LendingStorage.Layout storage s = LendingStorage.layout();
        require(asset_ == s.asset && amount != 0 && to != address(0), "BAD_WITHDRAW");
        _accrue(s);
        LendingStorage.Account storage a = s.accounts[msg.sender];
        uint256 debt = _syncDebt(a, s.borrowIndex);
        require(a.supplied >= amount, "INSUFFICIENT_SUPPLY");
        require(_isHealthy(s, a.supplied - amount, debt), "COLLATERAL_REQUIRED");
        require(SafeToken.staticBalanceOf(s.asset, address(this)) >= amount, "INSUFFICIENT_CASH");
        a.supplied -= amount;
        s.totalSupplied -= amount;
        s.receiptToken.safeBurn(msg.sender, amount);
        s.asset.safeTransfer(to, amount);
        emit Withdraw(msg.sender, to, amount);
        return amount;
    }

    function _repay(address payer, address borrower, uint256 amount) private returns (uint256 paid) {
        require(borrower != address(0) && amount != 0, "BAD_REPAY");
        LendingStorage.Layout storage s = LendingStorage.layout();
        _accrue(s);
        LendingStorage.Account storage a = s.accounts[borrower];
        uint256 debt = _syncDebt(a, s.borrowIndex);
        paid = amount > debt ? debt : amount;
        require(paid != 0, "NO_DEBT");
        s.asset.safeTransferFrom(payer, address(this), paid);
        a.principalDebt = debt - paid;
        a.debtIndex = s.borrowIndex;
        s.totalBorrows -= paid;
        emit Repay(payer, borrower, paid);
    }

    function _accrue(LendingStorage.Layout storage s) private returns (uint256 interest) {
        uint256 elapsed = block.timestamp - s.lastAccrual;
        if (elapsed == 0) return 0;
        uint256 cash = SafeToken.staticBalanceOf(s.asset, address(this));
        uint256 denominator = cash + s.totalBorrows;
        uint256 utilization = denominator == 0 ? 0 : s.totalBorrows * 1e18 / denominator;
        uint256 rate = IFixtureRateModel(s.rateModel).borrowRatePerSecond(utilization);
        interest = s.totalBorrows * rate * elapsed / 1e18;
        if (interest != 0) {
            s.totalBorrows += interest;
            s.borrowIndex += s.borrowIndex * rate * elapsed / 1e18;
        }
        s.lastAccrual = uint64(block.timestamp);
        emit AccrueInterest(interest, s.borrowIndex);
    }

    function _isHealthy(LendingStorage.Layout storage s, uint256 supplied, uint256 debt)
        private
        view
        returns (bool)
    {
        uint256 directPrice = IFixturePriceOracle(s.oracle).price(s.asset);
        require(directPrice != 0, "NO_PRICE");
        (bool healthy,) = IFixtureComptroller(s.comptroller).checkLiquidity(s.asset, supplied, debt);
        return healthy;
    }

    function _syncDebt(LendingStorage.Account storage a, uint256 currentIndex)
        private
        returns (uint256 debt)
    {
        debt = _currentDebt(a, currentIndex);
        a.principalDebt = debt;
        a.debtIndex = currentIndex;
    }

    function _currentDebt(LendingStorage.Account storage a, uint256 currentIndex)
        private
        view
        returns (uint256)
    {
        if (a.principalDebt == 0) return 0;
        return a.principalDebt * currentIndex / a.debtIndex;
    }
}
