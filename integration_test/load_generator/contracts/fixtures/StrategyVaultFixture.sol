// SPDX-License-Identifier: MIT
pragma solidity ^0.8.28;

import {
    IFixtureProxyImplementation,
    IERC20Fixture,
    SafeToken,
    FixtureReentrancyGuard
} from "./FixtureCore.sol";

interface IStrategyAdapter {
    function deposit(uint256 amount) external;
    function withdraw(address recipient, uint256 amount) external;
    function managedAssets() external view returns (uint256);
}

interface IStrategyModuleFixture {
    function strategyModuleUUID() external pure returns (bytes32);
}

contract DeterministicStrategyAdapter {
    using SafeToken for address;

    address public immutable asset;
    address public immutable owner;
    address public controller;

    constructor(address asset_, address owner_) {
        require(asset_.code.length != 0 && owner_ != address(0), "BAD_CONFIG");
        asset = asset_;
        owner = owner_;
    }

    function setController(address controller_) external {
        require(msg.sender == owner && controller == address(0), "CONTROLLER_LOCKED");
        require(controller_.code.length != 0, "BAD_CONTROLLER");
        controller = controller_;
    }

    function deposit(uint256 amount) external {
        require(msg.sender == controller && amount != 0, "BAD_DEPOSIT");
        asset.safeTransferFrom(msg.sender, address(this), amount);
    }

    function withdraw(address recipient, uint256 amount) external {
        require(msg.sender == controller && recipient != address(0), "BAD_WITHDRAW");
        asset.safeTransfer(recipient, amount);
    }

    function managedAssets() external view returns (uint256) {
        return SafeToken.staticBalanceOf(asset, address(this));
    }

    function donate(uint256 amount) external {
        require(amount != 0, "ZERO_AMOUNT");
        asset.safeTransferFrom(msg.sender, address(this), amount);
    }
}

library StrategyVaultStorage {
    bytes32 internal constant SLOT = keccak256("sei.replay.fixture.erc4626.storage.v1");

    struct Layout {
        bool initialized;
        string name;
        string symbol;
        address asset;
        address keeper;
        address strategyModule;
        address strategyAdapter;
        uint16 maxRebalanceBps;
        bool strategyDispatchActive;
        uint256 totalShares;
        mapping(address => uint256) shareBalance;
        mapping(address => mapping(address => uint256)) allowance;
    }

    function layout() internal pure returns (Layout storage s) {
        bytes32 slot = SLOT;
        assembly { s.slot := slot }
    }
}

contract NamespacedStrategyModule is IStrategyModuleFixture {
    using SafeToken for address;

    bytes32 public constant MODULE_UUID = keccak256("sei.replay.fixture.strategy-module.v1");

    event StrategyHarvested(uint256 managedAssets);
    event StrategyRebalanced(uint256 deposited, uint256 managedAssets);
    event StrategyDivested(uint256 amount);

    function strategyModuleUUID() external pure returns (bytes32) { return MODULE_UUID; }

    function harvest() external returns (uint256 managed) {
        StrategyVaultStorage.Layout storage s = StrategyVaultStorage.layout();
        require(msg.sender == s.keeper, "NOT_KEEPER");
        managed = IStrategyAdapter(s.strategyAdapter).managedAssets();
        emit StrategyHarvested(managed);
    }

    function rebalance(uint256 amount) external returns (uint256 managed) {
        StrategyVaultStorage.Layout storage s = StrategyVaultStorage.layout();
        require(msg.sender == s.keeper, "NOT_KEEPER");
        uint256 idle = SafeToken.staticBalanceOf(s.asset, address(this));
        require(
            amount != 0 && amount <= idle * s.maxRebalanceBps / 10_000,
            "REBALANCE_OUT_OF_RANGE"
        );
        IStrategyAdapter(s.strategyAdapter).deposit(amount);
        managed = IStrategyAdapter(s.strategyAdapter).managedAssets();
        emit StrategyRebalanced(amount, managed);
    }

    function divest(uint256 amount) external {
        StrategyVaultStorage.Layout storage s = StrategyVaultStorage.layout();
        require(s.strategyDispatchActive, "ONLY_VAULT_DISPATCH");
        IStrategyAdapter(s.strategyAdapter).withdraw(address(this), amount);
        emit StrategyDivested(amount);
    }
}

contract StrategyVaultImplementation is IFixtureProxyImplementation, FixtureReentrancyGuard {
    using SafeToken for address;

    bytes32 private constant IMPLEMENTATION_SLOT =
        bytes32(uint256(keccak256("eip1967.proxy.implementation")) - 1);
    bytes32 private constant MODULE_UUID = keccak256("sei.replay.fixture.strategy-module.v1");

    event Transfer(address indexed from, address indexed to, uint256 shares);
    event Approval(address indexed owner, address indexed spender, uint256 shares);
    event Deposit(address indexed sender, address indexed owner, uint256 assets, uint256 shares);
    event Withdraw(
        address indexed sender,
        address indexed receiver,
        address indexed owner,
        uint256 assets,
        uint256 shares
    );

    function proxiableUUID() external pure returns (bytes32) { return IMPLEMENTATION_SLOT; }

    function initialize(
        address asset_,
        address keeper_,
        address strategyModule_,
        address strategyAdapter_,
        uint16 maxRebalanceBps_,
        string calldata name_,
        string calldata symbol_
    ) external {
        StrategyVaultStorage.Layout storage s = StrategyVaultStorage.layout();
        require(!s.initialized, "ALREADY_INITIALIZED");
        require(
            asset_.code.length != 0 && strategyModule_.code.length != 0
                && strategyAdapter_.code.length != 0 && keeper_ != address(0),
            "BAD_CONFIG"
        );
        require(maxRebalanceBps_ != 0 && maxRebalanceBps_ <= 9_000, "BAD_REBALANCE_BPS");
        require(bytes(name_).length != 0 && bytes(name_).length <= 64, "BAD_NAME");
        require(bytes(symbol_).length != 0 && bytes(symbol_).length <= 16, "BAD_SYMBOL");
        require(
            IStrategyModuleFixture(strategyModule_).strategyModuleUUID() == MODULE_UUID,
            "BAD_MODULE"
        );
        require(DeterministicStrategyAdapter(strategyAdapter_).asset() == asset_, "ADAPTER_ASSET_MISMATCH");
        s.initialized = true;
        s.asset = asset_;
        s.keeper = keeper_;
        s.strategyModule = strategyModule_;
        s.strategyAdapter = strategyAdapter_;
        s.maxRebalanceBps = maxRebalanceBps_;
        s.name = name_;
        s.symbol = symbol_;
        asset_.safeApprove(strategyAdapter_, type(uint256).max);
    }

    function name() external view returns (string memory) { return StrategyVaultStorage.layout().name; }
    function symbol() external view returns (string memory) { return StrategyVaultStorage.layout().symbol; }
    function decimals() external pure returns (uint8) { return 18; }
    function asset() external view returns (address) { return StrategyVaultStorage.layout().asset; }
    function totalSupply() external view returns (uint256) { return StrategyVaultStorage.layout().totalShares; }
    function balanceOf(address owner) external view returns (uint256) {
        return StrategyVaultStorage.layout().shareBalance[owner];
    }
    function allowance(address owner, address spender) external view returns (uint256) {
        return StrategyVaultStorage.layout().allowance[owner][spender];
    }

    function totalAssets() public view returns (uint256) {
        StrategyVaultStorage.Layout storage s = StrategyVaultStorage.layout();
        uint256 idle = SafeToken.staticBalanceOf(s.asset, address(this));
        uint256 managed = IStrategyAdapter(s.strategyAdapter).managedAssets();
        return idle + managed;
    }

    function convertToShares(uint256 assets) public view returns (uint256) {
        StrategyVaultStorage.Layout storage s = StrategyVaultStorage.layout();
        uint256 supply = s.totalShares;
        uint256 assetsBefore = totalAssets();
        return supply == 0 || assetsBefore == 0 ? assets : assets * supply / assetsBefore;
    }

    function convertToAssets(uint256 shares) public view returns (uint256) {
        uint256 supply = StrategyVaultStorage.layout().totalShares;
        return supply == 0 ? shares : shares * totalAssets() / supply;
    }

    function maxDeposit(address) external pure returns (uint256) { return type(uint128).max; }

    function deposit(uint256 assets, address receiver)
        external
        nonReentrant
        returns (uint256 shares)
    {
        require(assets != 0 && assets <= type(uint128).max && receiver != address(0), "BAD_DEPOSIT");
        shares = convertToShares(assets);
        require(shares != 0, "ZERO_SHARES");
        StrategyVaultStorage.Layout storage s = StrategyVaultStorage.layout();
        s.asset.safeTransferFrom(msg.sender, address(this), assets);
        _mint(receiver, shares);
        emit Deposit(msg.sender, receiver, assets, shares);
    }

    function withdraw(uint256 assets, address receiver, address owner)
        external
        nonReentrant
        returns (uint256 shares)
    {
        require(assets != 0 && receiver != address(0), "BAD_WITHDRAW");
        StrategyVaultStorage.Layout storage s = StrategyVaultStorage.layout();
        uint256 supply = s.totalShares;
        uint256 allAssets = totalAssets();
        shares = (assets * supply + allAssets - 1) / allAssets;
        _spendAllowance(owner, shares);
        _burn(owner, shares);
        _ensureIdle(assets);
        s.asset.safeTransfer(receiver, assets);
        emit Withdraw(msg.sender, receiver, owner, assets, shares);
    }

    function redeem(uint256 shares, address receiver, address owner)
        external
        nonReentrant
        returns (uint256 assets)
    {
        require(shares != 0 && receiver != address(0), "BAD_REDEEM");
        assets = convertToAssets(shares);
        require(assets != 0, "ZERO_ASSETS");
        _spendAllowance(owner, shares);
        _burn(owner, shares);
        _ensureIdle(assets);
        StrategyVaultStorage.Layout storage s = StrategyVaultStorage.layout();
        s.asset.safeTransfer(receiver, assets);
        emit Withdraw(msg.sender, receiver, owner, assets, shares);
    }

    function approve(address spender, uint256 shares) external returns (bool) {
        require(spender != address(0), "ZERO_SPENDER");
        StrategyVaultStorage.layout().allowance[msg.sender][spender] = shares;
        emit Approval(msg.sender, spender, shares);
        return true;
    }

    function transfer(address to, uint256 shares) external returns (bool) {
        _transfer(msg.sender, to, shares);
        return true;
    }

    function transferFrom(address from, address to, uint256 shares) external returns (bool) {
        _spendAllowance(from, shares);
        _transfer(from, to, shares);
        return true;
    }

    function harvest() external nonReentrant returns (uint256 managed) {
        bytes memory result = _strategyDelegate(abi.encodeCall(NamespacedStrategyModule.harvest, ()));
        managed = abi.decode(result, (uint256));
    }

    function rebalance(uint256 amount) external nonReentrant returns (uint256 managed) {
        bytes memory result =
            _strategyDelegate(abi.encodeCall(NamespacedStrategyModule.rebalance, (amount)));
        managed = abi.decode(result, (uint256));
    }

    function _ensureIdle(uint256 assets) private {
        StrategyVaultStorage.Layout storage s = StrategyVaultStorage.layout();
        uint256 idle = SafeToken.staticBalanceOf(s.asset, address(this));
        if (idle < assets) {
            _strategyDelegate(abi.encodeCall(NamespacedStrategyModule.divest, (assets - idle)));
        }
    }

    function _strategyDelegate(bytes memory data) private returns (bytes memory result) {
        StrategyVaultStorage.Layout storage s = StrategyVaultStorage.layout();
        require(!s.strategyDispatchActive, "NESTED_STRATEGY_DISPATCH");
        s.strategyDispatchActive = true;
        address module = s.strategyModule;
        (bool ok, bytes memory returned) = module.delegatecall(data);
        if (!ok) assembly { revert(add(returned, 32), mload(returned)) }
        s.strategyDispatchActive = false;
        return returned;
    }

    function _mint(address to, uint256 shares) private {
        StrategyVaultStorage.Layout storage s = StrategyVaultStorage.layout();
        s.totalShares += shares;
        s.shareBalance[to] += shares;
        emit Transfer(address(0), to, shares);
    }

    function _burn(address from, uint256 shares) private {
        StrategyVaultStorage.Layout storage s = StrategyVaultStorage.layout();
        require(s.shareBalance[from] >= shares, "INSUFFICIENT_SHARES");
        unchecked { s.shareBalance[from] -= shares; }
        s.totalShares -= shares;
        emit Transfer(from, address(0), shares);
    }

    function _transfer(address from, address to, uint256 shares) private {
        require(to != address(0), "ZERO_RECIPIENT");
        StrategyVaultStorage.Layout storage s = StrategyVaultStorage.layout();
        require(s.shareBalance[from] >= shares, "INSUFFICIENT_SHARES");
        unchecked {
            s.shareBalance[from] -= shares;
            s.shareBalance[to] += shares;
        }
        emit Transfer(from, to, shares);
    }

    function _spendAllowance(address owner, uint256 shares) private {
        if (msg.sender == owner) return;
        StrategyVaultStorage.Layout storage s = StrategyVaultStorage.layout();
        uint256 allowed = s.allowance[owner][msg.sender];
        require(allowed >= shares, "INSUFFICIENT_ALLOWANCE");
        if (allowed != type(uint256).max) {
            s.allowance[owner][msg.sender] = allowed - shares;
            emit Approval(owner, msg.sender, allowed - shares);
        }
    }
}
