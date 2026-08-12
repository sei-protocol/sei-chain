// SPDX-License-Identifier: MIT
pragma solidity ^0.8.28;

import {IFixtureProxyImplementation} from "./FixtureCore.sol";

library ProxyERC20Storage {
    bytes32 internal constant SLOT = keccak256("sei.replay.fixture.erc20.storage.v1");

    struct Layout {
        string name;
        string symbol;
        uint8 decimals;
        uint256 totalSupply;
        address tokenAdmin;
        bool initialized;
        mapping(address => uint256) balances;
        mapping(address => mapping(address => uint256)) allowances;
        mapping(address => bool) minters;
    }

    function layout() internal pure returns (Layout storage s) {
        bytes32 slot = SLOT;
        assembly {
            s.slot := slot
        }
    }
}

contract ProxyERC20Implementation is IFixtureProxyImplementation {
    bytes32 private constant IMPLEMENTATION_SLOT =
        bytes32(uint256(keccak256("eip1967.proxy.implementation")) - 1);

    event Transfer(address indexed from, address indexed to, uint256 value);
    event Approval(address indexed owner, address indexed spender, uint256 value);
    event MinterSet(address indexed minter, bool allowed);
    event TokenAdminChanged(address indexed oldAdmin, address indexed newAdmin);

    modifier onlyTokenAdmin() {
        require(msg.sender == ProxyERC20Storage.layout().tokenAdmin, "NOT_TOKEN_ADMIN");
        _;
    }

    function proxiableUUID() external pure returns (bytes32) {
        return IMPLEMENTATION_SLOT;
    }

    function initialize(string calldata name_, string calldata symbol_, uint8 decimals_, address admin_)
        external
    {
        ProxyERC20Storage.Layout storage s = ProxyERC20Storage.layout();
        require(!s.initialized, "ALREADY_INITIALIZED");
        require(admin_ != address(0) && decimals_ <= 18, "BAD_CONFIG");
        require(bytes(name_).length != 0 && bytes(name_).length <= 64, "BAD_NAME");
        require(bytes(symbol_).length != 0 && bytes(symbol_).length <= 16, "BAD_SYMBOL");
        s.initialized = true;
        s.name = name_;
        s.symbol = symbol_;
        s.decimals = decimals_;
        s.tokenAdmin = admin_;
        s.minters[admin_] = true;
        emit MinterSet(admin_, true);
    }

    function name() external view returns (string memory) { return ProxyERC20Storage.layout().name; }
    function symbol() external view returns (string memory) { return ProxyERC20Storage.layout().symbol; }
    function decimals() external view returns (uint8) { return ProxyERC20Storage.layout().decimals; }
    function totalSupply() external view returns (uint256) { return ProxyERC20Storage.layout().totalSupply; }
    function balanceOf(address account) external view returns (uint256) {
        return ProxyERC20Storage.layout().balances[account];
    }
    function allowance(address owner, address spender) external view returns (uint256) {
        return ProxyERC20Storage.layout().allowances[owner][spender];
    }
    function tokenAdmin() external view returns (address) { return ProxyERC20Storage.layout().tokenAdmin; }
    function isMinter(address account) external view returns (bool) {
        return ProxyERC20Storage.layout().minters[account];
    }

    function transfer(address to, uint256 amount) external returns (bool) {
        _transfer(msg.sender, to, amount);
        return true;
    }

    function approve(address spender, uint256 amount) external returns (bool) {
        require(spender != address(0), "ZERO_SPENDER");
        ProxyERC20Storage.layout().allowances[msg.sender][spender] = amount;
        emit Approval(msg.sender, spender, amount);
        return true;
    }

    function transferFrom(address from, address to, uint256 amount) external returns (bool) {
        ProxyERC20Storage.Layout storage s = ProxyERC20Storage.layout();
        uint256 allowed = s.allowances[from][msg.sender];
        require(allowed >= amount, "INSUFFICIENT_ALLOWANCE");
        if (allowed != type(uint256).max) {
            s.allowances[from][msg.sender] = allowed - amount;
            emit Approval(from, msg.sender, allowed - amount);
        }
        _transfer(from, to, amount);
        return true;
    }

    function mint(address to, uint256 amount) external {
        ProxyERC20Storage.Layout storage s = ProxyERC20Storage.layout();
        require(s.minters[msg.sender], "NOT_MINTER");
        require(to != address(0), "ZERO_RECIPIENT");
        s.totalSupply += amount;
        s.balances[to] += amount;
        emit Transfer(address(0), to, amount);
    }

    function burn(address from, uint256 amount) external {
        ProxyERC20Storage.Layout storage s = ProxyERC20Storage.layout();
        require(msg.sender == from || s.minters[msg.sender], "NOT_BURNER");
        require(s.balances[from] >= amount, "INSUFFICIENT_BALANCE");
        unchecked { s.balances[from] -= amount; }
        s.totalSupply -= amount;
        emit Transfer(from, address(0), amount);
    }

    function setMinter(address minter, bool allowed) external onlyTokenAdmin {
        require(minter != address(0), "ZERO_MINTER");
        ProxyERC20Storage.layout().minters[minter] = allowed;
        emit MinterSet(minter, allowed);
    }

    function changeTokenAdmin(address newAdmin) external onlyTokenAdmin {
        require(newAdmin != address(0), "ZERO_ADMIN");
        ProxyERC20Storage.Layout storage s = ProxyERC20Storage.layout();
        address oldAdmin = s.tokenAdmin;
        s.tokenAdmin = newAdmin;
        emit TokenAdminChanged(oldAdmin, newAdmin);
    }

    function _transfer(address from, address to, uint256 amount) private {
        require(to != address(0), "ZERO_RECIPIENT");
        ProxyERC20Storage.Layout storage s = ProxyERC20Storage.layout();
        require(s.balances[from] >= amount, "INSUFFICIENT_BALANCE");
        unchecked {
            s.balances[from] -= amount;
            s.balances[to] += amount;
        }
        emit Transfer(from, to, amount);
    }
}
